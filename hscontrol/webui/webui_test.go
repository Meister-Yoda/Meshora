package webui

import (
	"bytes"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/juanfont/headscale/hscontrol/state"
	"github.com/juanfont/headscale/hscontrol/templates"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

const (
	testAdmin    = "alice"
	testPassword = "correct horse battery"
)

// elem-go renders attributes sorted by name.
var csrfRe = regexp.MustCompile(`name="csrf_token"[^>]*value="([^"]+)"`)

type testEnv struct {
	t       *testing.T
	state   *state.State
	server  *httptest.Server
	client  *http.Client
	handler *Handler

	changesMu sync.Mutex
	changes   []change.Change
}

// changeCount returns how many changes the dashboard handed to Change.
func (e *testEnv) changeCount() int {
	e.changesMu.Lock()
	defer e.changesMu.Unlock()

	return len(e.changes)
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	tmpDir := t.TempDir()
	cfg := &types.Config{
		ServerURL: "http://localhost:8080",
		Database: types.DatabaseConfig{
			Type:   types.DatabaseSqlite,
			Sqlite: types.SqliteConfig{Path: tmpDir + "/headscale_test.db"},
		},
		Policy:   types.PolicyConfig{Mode: types.PolicyModeDB},
		WebAdmin: types.WebAdminConfig{Enabled: true, SessionTTL: time.Hour},
		Tuning: types.Tuning{
			RegisterCacheExpiration: time.Minute,
			RegisterCacheMaxEntries: 100,
		},
	}

	st, err := state.NewState(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	_, err = st.CreateWebAdmin(testAdmin, testPassword)
	require.NoError(t, err)

	env := &testEnv{t: t, state: st, client: newClient(t)}

	r := chi.NewRouter()
	env.handler = New(st, cfg, func(cs ...change.Change) {
		env.changesMu.Lock()
		defer env.changesMu.Unlock()

		env.changes = append(env.changes, cs...)
	})
	t.Cleanup(env.handler.Close)
	env.handler.Routes(r)

	env.server = httptest.NewServer(r)
	t.Cleanup(env.server.Close)

	return env
}

func newClient(t *testing.T) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)

	return &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// result is a fully read response.
type result struct {
	StatusCode int
	Header     http.Header
	cookies    []*http.Cookie
}

func (r result) Cookies() []*http.Cookie { return r.cookies }

func (e *testEnv) do(client *http.Client, method, path string, form url.Values) (result, string) {
	e.t.Helper()

	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}

	req, err := http.NewRequestWithContext(e.t.Context(), method, e.server.URL+path, body)
	require.NoError(e.t, err)

	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := client.Do(req)
	require.NoError(e.t, err)

	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	require.NoError(e.t, err)

	return result{StatusCode: resp.StatusCode, Header: resp.Header, cookies: resp.Cookies()}, string(b)
}

func (e *testEnv) get(client *http.Client, path string) (result, string) {
	e.t.Helper()

	return e.do(client, http.MethodGet, path, nil)
}

func (e *testEnv) post(client *http.Client, path string, form url.Values) (result, string) {
	e.t.Helper()

	return e.do(client, http.MethodPost, path, form)
}

func csrfFrom(t *testing.T, body string) string {
	t.Helper()

	m := csrfRe.FindStringSubmatch(body)
	require.Len(t, m, 2, "no csrf token in page")

	return m[1]
}

// login runs the login form with client and returns the dashboard body.
func (e *testEnv) login(client *http.Client, username, password string) (result, string) {
	e.t.Helper()

	_, body := e.get(client, "/")

	return e.post(client, "/", url.Values{
		"csrf_token": {csrfFrom(e.t, body)},
		"username":   {username},
		"password":   {password},
	})
}

func TestDashboardRequiresLogin(t *testing.T) {
	env := newTestEnv(t)

	resp, _ := env.get(env.client, "/admin/")
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Location"), "/")
}

func TestStylesheetIsPublic(t *testing.T) {
	env := newTestEnv(t)

	resp, body := env.get(env.client, "/admin/assets/admin.css")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/css; charset=utf-8", resp.Header.Get("Content-Type"))
	assert.Contains(t, body, ".auth-card")

	_, login := env.get(env.client, "/")
	assert.Contains(t, login, `href="/admin/assets/admin.css?v=`)
	assert.NotContains(t, login, "style=", "the CSP forbids inline styles")
}

func TestLoginAndLogout(t *testing.T) {
	env := newTestEnv(t)

	resp, _ := env.login(env.client, testAdmin, testPassword)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, "/admin/", resp.Header.Get("Location"))

	var sessionSet bool

	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie && c.Value != "" {
			sessionSet = true

			assert.True(t, c.HttpOnly)
			assert.Equal(t, http.SameSiteStrictMode, c.SameSite)
			assert.Equal(t, "/", c.Path)
		}
	}

	require.True(t, sessionSet)

	resp, body := env.get(env.client, "/admin/")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, body, `class="account-name">`+testAdmin+`<`)
	assert.Contains(t, resp.Header.Get("Content-Security-Policy"), "style-src 'self';")
	assert.NotContains(t, body, "style=", "the CSP forbids inline styles")
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))

	// Logout without the session's CSRF token is refused.
	resp, _ = env.post(env.client, "/admin/logout", url.Values{})
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	resp, _ = env.post(env.client, "/admin/logout", url.Values{"csrf_token": {csrfFrom(t, body)}})
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)

	resp, _ = env.get(env.client, "/admin/")
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	env := newTestEnv(t)

	for _, tc := range []struct{ name, username, password string }{
		{"wrong password", testAdmin, "wrong password!!"},
		{"unknown user", "mallory", testPassword},
	} {
		resp, body := env.login(newClient(t), tc.username, tc.password)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, tc.name)
		assert.Contains(t, body, msgInvalidLogin, tc.name)
	}
}

func TestLoginRequiresLoginCSRF(t *testing.T) {
	env := newTestEnv(t)

	resp, _ := env.post(env.client, "/", url.Values{
		"csrf_token": {"forged"},
		"username":   {testAdmin},
		"password":   {testPassword},
	})
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	resp, _ = env.get(env.client, "/admin/")
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode, "no session after a forged login")
}

func TestLoginRateLimit(t *testing.T) {
	env := newTestEnv(t)

	for range maxLoginFailures {
		resp, _ := env.login(env.client, testAdmin, "wrong password!!")
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	}

	resp, body := env.login(env.client, testAdmin, testPassword)
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Contains(t, body, msgTooManyAttempts)
}

func TestPasswordChangeEndsSessions(t *testing.T) {
	env := newTestEnv(t)

	resp, _ := env.login(env.client, testAdmin, testPassword)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)

	resp, _ = env.get(env.client, "/admin/")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	require.NoError(t, env.state.SetWebAdminPassword(testAdmin, "a brand new password"))

	resp, _ = env.get(env.client, "/admin/")
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
}

func TestDeletedAdminLosesSession(t *testing.T) {
	env := newTestEnv(t)

	resp, _ := env.login(env.client, testAdmin, testPassword)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)

	require.NoError(t, env.state.DeleteWebAdmin(testAdmin))

	resp, _ = env.get(env.client, "/admin/")
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
}

func TestLoginEscapesEchoedUsername(t *testing.T) {
	env := newTestEnv(t)

	_, body := env.login(env.client, `"><script>alert(1)</script>`, "wrong password!!")
	assert.NotContains(t, body, "<script>alert(1)</script>")
	assert.True(t, strings.Contains(body, "&#34;&gt;&lt;script&gt;") || strings.Contains(body, "&quot;&gt;&lt;script&gt;"))
}

const xssPayload = `"><script>alert(1)</script>`

// assertEscaped checks that the XSS payload never reaches the page raw, in a
// text node or in an attribute value.
func assertEscaped(t *testing.T, body string) {
	t.Helper()

	assert.NotContains(t, body, "<script>alert(1)</script>")
	assert.NotContains(t, body, `"><script`)
	assert.NotContains(t, body, "style=", "the CSP forbids inline styles")
}

func TestListPagesRequireLogin(t *testing.T) {
	env := newTestEnv(t)

	for _, path := range []string{"/admin/users", "/admin/nodes", "/admin/pending"} {
		resp, _ := env.get(env.client, path)
		assert.Equal(t, http.StatusSeeOther, resp.StatusCode, path)
		assert.Equal(t, "/", resp.Header.Get("Location"), path)
	}
}

func TestListPagesEmpty(t *testing.T) {
	env := newTestEnv(t)

	resp, _ := env.login(env.client, testAdmin, testPassword)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)

	for path, want := range map[string]string{
		"/admin/nodes":   "No nodes yet",
		"/admin/pending": "Nothing pending",
	} {
		resp, body := env.get(env.client, path)
		require.Equal(t, http.StatusOK, resp.StatusCode, path)
		assert.Contains(t, body, want, path)
		assert.Contains(t, body, `aria-current="page"`, path)
		assert.NotContains(t, body, "<table", path)
	}
}

func TestUsersPage(t *testing.T) {
	env := newTestEnv(t)

	// Usernames are validated, but OIDC display names and emails come from
	// the identity provider unchecked.
	user := env.state.CreateUserForTest("dave")
	require.NoError(t, env.state.DB().DB.Model(user).Updates(map[string]any{
		"display_name": xssPayload,
		"email":        xssPayload + "@example.com",
		"provider":     "oidc",
	}).Error)
	env.state.PutNodeInStoreForTest(*env.state.CreateRegisteredNodeForTest(user, "laptop"))

	resp, _ := env.login(env.client, testAdmin, testPassword)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)

	resp, body := env.get(env.client, "/admin/users")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Regexp(t, `<a aria-current="page" href="/admin/users">.*?Users</a>`, body)
	assert.Contains(t, body, "&lt;script&gt;alert(1)&lt;/script&gt;")
	assert.Contains(t, body, ">OIDC<")
	assert.Contains(t, body, `title="&#34;&gt;&lt;script&gt;`, "display name escaped in attribute")
	assert.Contains(t, body, `<td class="num">1</td>`, "one node owned by the user")
	assertEscaped(t, body)
}

func TestNodesPage(t *testing.T) {
	env := newTestEnv(t)

	user := env.state.CreateUserForTest("bob")

	// The NodeStore holds nodes with User preloaded, as loaded from the DB.
	// No given name yet, so the raw hostname is the primary label and also
	// lands in the title attribute.
	owned := env.state.CreateRegisteredNodeForTest(user, "owned")
	owned.User = user
	owned.Hostname = xssPayload
	owned.GivenName = ""
	online := true
	owned.IsOnline = &online
	env.state.PutNodeInStoreForTest(*owned)

	// Tagged nodes keep UserID for "created by" tracking; the tags own them.
	tagged := env.state.CreateRegisteredNodeForTest(user, "server")
	tagged.User = user
	tagged.Tags = []string{"tag:server"}
	env.state.PutNodeInStoreForTest(*tagged)

	resp, _ := env.login(env.client, testAdmin, testPassword)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)

	resp, body := env.get(env.client, "/admin/nodes")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, body, "2 / 2 devices shown")
	assert.Contains(t, body, ">tag:server<")
	assert.Contains(t, body, ">Online<")
	assert.Contains(t, body, owned.IPv4.String())
	assert.Contains(t, body, "&lt;script&gt;alert(1)&lt;/script&gt;")
	assert.Contains(t, body, `title="&#34;&gt;&lt;script&gt;`, "hostname escaped in attribute")
	assertEscaped(t, body)

	// The tagged node's row must not list bob as owner: only the owned
	// node's row does.
	assert.Equal(t, 1, strings.Count(body, "<td>bob</td>"))

	_, users := env.get(env.client, "/admin/users")
	assert.Contains(t, users, `<td class="num">1</td>`, "tagged nodes do not count for their creator")
}

func TestPendingPage(t *testing.T) {
	env := newTestEnv(t)

	user := env.state.CreateUserForTest("carol")
	src := env.state.CreateRegisteredNodeForTest(user, "src")
	env.state.PutNodeInStoreForTest(*src)

	env.state.SetAuthCacheEntry(types.MustAuthID(), types.NewRegisterAuthRequest(&types.RegistrationData{
		MachineKey: key.NewMachine().Public(),
		Hostname:   xssPayload,
		Hostinfo:   &tailcfg.Hostinfo{OS: "linux"},
	}))
	env.state.SetAuthCacheEntry(types.MustAuthID(), types.NewSSHCheckAuthRequest(src.ID, 999))

	resp, _ := env.login(env.client, testAdmin, testPassword)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)

	resp, body := env.get(env.client, "/admin/pending")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, body, ">Registration<")
	assert.Contains(t, body, ">SSH check<")
	assert.Contains(t, body, ">linux<")
	assert.Contains(t, body, "mkey:")
	assert.Contains(t, body, "src → node 999")
	assert.Contains(t, body, "just now")
	assertEscaped(t, body)

	_, overview := env.get(env.client, "/admin/")
	assert.Contains(t, overview, `href="/admin/pending"`)
	assert.Regexp(t, `Pending</div><div class="stat-value">2<`, overview)
}

func TestLanguageSwitchPersistsAndPreservesUserText(t *testing.T) {
	env := newTestEnv(t)
	resp, page := env.get(env.client, "/?lang=de")
	assert.Equal(t, "de", resp.Header.Get("Content-Language"))
	assert.Contains(t, page, `<html lang="de">`)
	assert.Contains(t, page, ">Anmelden<")
	assert.Contains(t, page, ">E-Mail / bisheriger Anmeldename<")
	assert.Contains(t, page, "Mit deinem Meshora-Konto anmelden.")
	assert.NotContains(t, page, "<!--meshora-i18n-->")
	resp, page = env.get(env.client, "/")
	assert.Equal(t, "de", resp.Header.Get("Content-Language"))
	assert.Contains(t, page, ">Anmelden<")
	resp, page = env.get(env.client, "/?lang=en")
	assert.Equal(t, "en", resp.Header.Get("Content-Language"))
	assert.Contains(t, page, ">Sign in<")
	assert.Contains(t, page, ">Email / previous login name<")
	assert.Contains(t, page, "Sign in with your Meshora account.")
	assert.NotContains(t, page, "Mit deinem Meshora-Konto anmelden.")
	// Browser language is used only in the absence of a saved selection.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, env.server.URL+"/", nil)
	require.NoError(t, err)
	req.Header.Set("Accept-Language", "de-DE,de;q=0.9,en;q=0.5")
	result, err := newClient(t).Do(req)
	require.NoError(t, err)

	defer result.Body.Close()

	assert.Equal(t, "de", result.Header.Get("Content-Language"))

	env.handler.activeOIDC = types.OIDCConfig{Issuer: "https://auth.example.com", ClientID: "meshora"}
	env.handler.adminOIDCGroups = []string{"admins"}
	env.handler.oidcButtonText = "Settings"
	_, page = env.get(env.client, "/?lang=de")
	assert.Contains(t, page, ">Settings</button>", "Custom button text is not a translation key")
}

func TestCloseStopsDashboardCacheSweepers(t *testing.T) {
	stack := make([]byte, 64)
	n := runtime.Stack(stack, false)
	gid := strings.Fields(string(stack[:n]))[1]
	count := func() int {
		var out bytes.Buffer

		require.NoError(t, pprof.Lookup("goroutine").WriteTo(&out, 2))

		total := 0

		for g := range strings.SplitSeq(out.String(), "\n\n") {
			if strings.Contains(g, "golang-lru/v2/expirable") &&
				strings.Contains(g, " in goroutine "+gid+"\n") {
				total++
			}
		}

		return total
	}
	baseline := count()
	h := New(nil, &types.Config{WebAdmin: types.WebAdminConfig{SessionTTL: time.Hour}}, nil)
	t.Cleanup(h.Close)
	require.Equal(t, baseline+4, count())

	h.Close()
	h.Close()
	assert.Eventually(t, func() bool { return count() == baseline },
		5*time.Second, 10*time.Millisecond)
}

func TestDeviceFilters(t *testing.T) {
	online := true

	node := templates.AdminNodeRow{GivenName: "Laptop", Hostname: "workstation", Owner: "alice", IPv4: "100.64.0.1", Online: &online}
	for _, filters := range []templates.AdminNodeFilters{
		{Query: "LAPTOP"},
		{Query: "station"},
		{Query: "100.64"},
		{Query: "ALICE"},
		{Status: "online", User: "alice", Expiry: "never", Query: "lap"},
	} {
		assert.True(t, matchesNodeFilters(node, filters), "%+v", filters)
	}

	for _, filters := range []templates.AdminNodeFilters{
		{Status: "offline"},
		{Status: "unknown"},
		{User: "bob"},
		{Tag: "tag:server"},
		{Expiry: "expired"},
		{Query: "missing"},
		{Query: "lap", User: "bob"},
	} {
		assert.False(t, matchesNodeFilters(node, filters), "%+v", filters)
	}

	node.Tags = []string{"tag:server"}
	assert.False(t, matchesNodeFilters(node, templates.AdminNodeFilters{User: "alice"}))
	assert.True(t, matchesNodeFilters(node, templates.AdminNodeFilters{Tag: "tag:server"}))
	node.Online = nil
	node.Expired = true
	assert.True(t, matchesNodeFilters(node, templates.AdminNodeFilters{Status: "unknown", Expiry: "expired"}))
	assert.False(t, matchesNodeFilters(node, templates.AdminNodeFilters{Expiry: "valid"}))
}

func TestDeviceFiltersHTTP(t *testing.T) {
	env := newTestEnv(t)

	user := env.state.CreateUserForTest("filter-owner")
	for _, name := range []string{"filter-laptop", "filter-server"} {
		node := env.state.CreateRegisteredNodeForTest(user, name)
		node.User = user
		env.state.PutNodeInStoreForTest(*node)
	}

	resp, _ := env.login(env.client, testAdmin, testPassword)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	resp, body := env.get(env.client, "/admin/nodes?q=LAPTOP&user=filter-owner&expiry=never&lang=de")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, body, "1 / 2 Geräte angezeigt")
	assert.Contains(t, body, `title="filter-laptop"`)
	assert.NotContains(t, body, `title="filter-server"`)
	resp, body = env.get(env.client, "/admin/nodes?q=missing&lang=en")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, body, "No matching devices")
	assert.Contains(t, body, "0 / 2 devices shown")
}

func TestPublicEnrollment(t *testing.T) {
	env := newTestEnv(t)
	for _, path := range []string{"/connect", "/connect?platform=linux", "/connect?platform=android"} {
		resp, body := env.get(env.client, path)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Security-Policy"), "script-src 'self'")
		assert.Contains(t, resp.Header.Get("Cache-Control"), "no-store")
		assert.NotContains(t, body, "Sign out")

		if path != "/connect" {
			assert.Contains(t, body, "http://localhost:8080")
		}
	}

	resp, _ := env.get(env.client, "/connect?platform=invalid")
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	resp, script := env.get(env.client, "/admin/assets/enrollment.js")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, script, "navigator.clipboard.writeText")
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/javascript")

	env.handler.activeOIDC.Issuer = "https://idp.example"
	_, body := env.get(env.client, "/connect?platform=windows")
	assert.Contains(t, body, "confirm registration")
	assert.NotContains(t, body, "https://idp.example")
	assert.Zero(t, env.changeCount())
}

func TestRootLoginAndLegacyRedirect(t *testing.T) {
	env := newTestEnv(t)
	response, page := env.get(env.client, "/")
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Contains(t, page, `action="/"`)
	require.NotContains(t, page, `class="brand-tag"`)
	require.Equal(t, "no-store", response.Header.Get("Cache-Control"))

	for _, path := range []string{"/admin/login?local=1&lang=de", "/setup"} {
		response, _ = env.get(env.client, path)
		require.Equal(t, http.StatusSeeOther, response.StatusCode)

		if path == "/setup" {
			require.Equal(t, "/", response.Header.Get("Location"))
		} else {
			require.Equal(t, "/?local=1&lang=de", response.Header.Get("Location"))
		}
	}

	loginResponse, _ := env.login(env.client, testAdmin, testPassword)
	require.Equal(t, http.StatusSeeOther, loginResponse.StatusCode)

	response, _ = env.get(env.client, "/")
	require.Equal(t, "/admin/", response.Header.Get("Location"))
	// A prefixed reverse proxy must keep links and cookie scope inside its prefix.
	env.handler.publicPath = "/mesh/admin"
	response, page = env.get(newClient(t), "/")
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Contains(t, page, `action="/mesh/"`)

	for _, cookie := range response.Cookies() {
		if cookie.Name == loginCSRFCookie && cookie.MaxAge >= 0 {
			require.Equal(t, "/mesh/", cookie.Path)
		}
	}
}
