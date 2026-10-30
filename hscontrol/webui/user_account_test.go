package webui

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/require"
)

func TestUnifiedUserFirstLoginAndPrivilegeIsolation(t *testing.T) {
	env := newTestEnv(t)
	adminCSRF := env.signedIn()
	user, _, err := env.state.CreateUser(types.User{Name: "personal", Email: "person@example.test"})
	require.NoError(t, err)

	other := env.state.CreateUserForTest("other")
	own := env.state.CreateRegisteredNodeForTest(user, "my-device")
	env.state.PutNodeInStoreForTest(*own)
	foreign := env.state.CreateRegisteredNodeForTest(other, "foreign-device")
	env.state.PutNodeInStoreForTest(*foreign)

	path := fmt.Sprintf("/admin/users/%d", user.ID)
	response, body := env.action(adminCSRF, path+"/password", nil)
	require.Equal(t, path, response)

	match := regexp.MustCompile(`<pre class="codeblock secret"><code>([^<]+)</code></pre>`).FindStringSubmatch(body)
	require.Len(t, match, 2)
	password := html.UnescapeString(match[1])
	_, again := env.get(env.client, path)
	require.NotContains(t, again, password)
	client := newClient(t)
	login, _ := env.login(client, user.Email, password)
	require.Equal(t, http.StatusSeeOther, login.StatusCode)
	require.Equal(t, "/account/password", login.Header.Get("Location"))

	blocked, _ := env.get(client, "/admin/users")
	require.Equal(t, "/account/password", blocked.Header.Get("Location"))

	_, page := env.get(client, "/account/password")
	token := csrfFrom(t, page)
	bad, _ := env.post(client, "/account/password", url.Values{"password": {"new personal password"}, "password_confirm": {"new personal password"}})
	require.Equal(t, http.StatusForbidden, bad.StatusCode)

	changed, _ := env.post(client, "/account/password", url.Values{"csrf_token": {token}, "current_password": {password}, "password": {"new personal password"}, "password_confirm": {"new personal password"}})
	require.Equal(t, http.StatusSeeOther, changed.StatusCode)

	rejected, _ := env.login(client, user.Email, password)
	require.Equal(t, http.StatusUnauthorized, rejected.StatusCode)

	login, _ = env.login(client, user.Email, "new personal password")
	require.Equal(t, "/account", login.Header.Get("Location"))

	_, page = env.get(client, "/account/devices")
	require.Contains(t, page, "my-device")
	require.NotContains(t, page, "foreign-device")
	require.NotContains(t, page, `href="/admin/users"`)

	for _, adminPath := range []string{"/admin/users", "/admin/nodes", "/admin/zones", "/admin/keys", "/admin/settings"} {
		res, _ := env.get(client, adminPath)
		require.Equal(t, http.StatusForbidden, res.StatusCode, adminPath)
	}

	hidden, _ := env.get(client, fmt.Sprintf("/account/devices/%d", foreign.ID))
	require.Equal(t, http.StatusNotFound, hidden.StatusCode)

	_, _ = env.action(adminCSRF, path+"/admin", url.Values{"is_admin": {"on"}})
	expired, _ := env.get(client, "/account")
	require.Equal(t, "/", expired.Header.Get("Location"))

	login, _ = env.login(client, user.Email, "new personal password")
	require.Equal(t, "/admin/", login.Header.Get("Location"))

	allowed, _ := env.get(client, "/admin/users")
	require.Equal(t, http.StatusOK, allowed.StatusCode)
}

func TestUnifiedMergePreservesDeviceOwnership(t *testing.T) {
	env := newTestEnv(t)
	source := env.state.CreateUserForTest("legacy-user")
	target, _, err := env.state.CreateUser(types.User{Name: "canonical-user", Email: "canonical@example.test"})
	require.NoError(t, err)

	node := env.state.CreateRegisteredNodeForTest(source, "preserved-device")
	env.state.PutNodeInStoreForTest(*node)
	_, err = env.state.MergeUserAccounts(types.UserID(source.ID), types.UserID(target.ID), false)
	require.NoError(t, err)

	transferred, ok := env.state.GetNodeByID(node.ID)
	require.True(t, ok)
	require.Equal(t, types.UserID(target.ID), transferred.TypedUserID())
	require.Equal(t, node.ID, transferred.ID())
	_, err = env.state.GetUserByID(types.UserID(source.ID))
	require.Error(t, err)
}

func TestPersonalMenuSeparatesPagesAndKeepsAdminNavigation(t *testing.T) {
	env := newTestEnv(t)
	enableTestEmail(env)
	client, id, _ := emailUser(t, env)
	user, err := env.state.GetUserByID(id)
	require.NoError(t, err)

	own := env.state.CreateRegisteredNodeForTest(user, "personal-menu-device")
	env.state.PutNodeInStoreForTest(*own)
	_, profile := env.get(client, "/account")
	require.Contains(t, profile, `action="/account/profile"`)
	require.NotContains(t, profile, `id="account-email"`)
	require.NotContains(t, profile, "personal-menu-device")
	require.NotContains(t, profile, `href="/admin/users"`)

	for _, path := range []string{"/account/devices", "/account/email", "/account/security", "/account/password"} {
		require.Contains(t, profile, `href="`+path+`"`)
	}

	_, devices := env.get(client, "/account/devices")
	require.Contains(t, devices, "personal-menu-device")
	require.NotContains(t, devices, `action="/account/profile"`)
	require.Regexp(t, `<a[^>]*aria-current="page"[^>]*href="/account/devices"`, devices)

	_, email := env.get(client, "/account/email")
	require.Contains(t, email, `id="account-email"`)
	require.NotContains(t, email, "personal-menu-device")
	require.Regexp(t, `<a[^>]*aria-current="page"[^>]*href="/account/email"`, email)
	env.signedIn()

	for _, path := range []string{"/account", "/account/email", "/admin/"} {
		response, page := env.get(env.client, path)
		require.Equal(t, http.StatusOK, response.StatusCode)
		require.Contains(t, page, `href="/admin/zones"`)
		require.Contains(t, page, `href="/admin/users"`)
		require.Contains(t, page, `href="/account/devices"`)
		require.Contains(t, page, `href="/account/email"`)
	}
}

func TestAccountSessionManagementIsolation(t *testing.T) {
	env := newTestEnv(t)
	env.signedIn()

	other := newClient(t)
	response, _ := env.login(other, testAdmin, testPassword)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	normal, _, _ := emailUser(t, env)
	lookup := func(client *http.Client) *session {
		target, err := url.Parse(env.server.URL)
		require.NoError(t, err)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, env.server.URL+"/account", nil)
		for _, cookie := range client.Jar.Cookies(target) {
			req.AddCookie(cookie)
		}

		sess, ok := env.handler.currentSession(req)
		require.True(t, ok)

		return sess
	}
	current, second, foreign := lookup(env.client), lookup(other), lookup(normal)
	_, page := env.get(env.client, "/account/sessions")
	require.Contains(t, page, current.managementID)
	require.Contains(t, page, second.managementID)
	require.NotContains(t, page, foreign.managementID)

	target, _ := url.Parse(env.server.URL)
	for _, cookie := range env.client.Jar.Cookies(target) {
		if cookie.Name == sessionCookie {
			require.NotContains(t, page, cookie.Value)
			require.NotContains(t, page, hashToken(cookie.Value))
		}
	}

	response, _ = env.post(env.client, "/account/sessions/revoke", url.Values{"scope": {"one"}, "session_id": {second.managementID}})
	require.Equal(t, http.StatusForbidden, response.StatusCode)
	csrf := csrfFrom(t, page)
	response, _ = env.post(env.client, "/account/sessions/revoke", url.Values{"csrf_token": {csrf}, "scope": {"one"}, "session_id": {foreign.managementID}})
	require.Equal(t, http.StatusNotFound, response.StatusCode)
	response, _ = env.post(env.client, "/account/sessions/revoke", url.Values{"csrf_token": {csrf}, "scope": {"one"}, "session_id": {second.managementID}})
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response, _ = env.get(other, "/account")
	require.Equal(t, http.StatusSeeOther, response.StatusCode)

	_, _ = env.login(other, testAdmin, testPassword)
	response, _ = env.post(env.client, "/account/sessions/revoke", url.Values{"csrf_token": {csrf}, "scope": {"others"}})
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response, _ = env.get(other, "/account")
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response, _ = env.get(env.client, "/account/sessions")
	require.Equal(t, http.StatusOK, response.StatusCode)
	response, _ = env.post(env.client, "/account/sessions/revoke", url.Values{"csrf_token": {csrf}, "scope": {"all"}})
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response, _ = env.get(env.client, "/account")
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response, _ = env.get(normal, "/account")
	require.Equal(t, http.StatusOK, response.StatusCode)
}
