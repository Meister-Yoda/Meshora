package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/db"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type setupTest struct {
	*testEnv

	cfg      *types.Config
	database *db.HSDatabase
	setup    *setupHandler
}

func newSetupTest(t *testing.T) *setupTest {
	t.Helper()
	directory := t.TempDir()
	cfg := &types.Config{
		ConfigFilePath: filepath.Join(directory, "config.yaml"),
		ServerURL:      "http://127.0.0.1:8080",
		BaseDomain:     "meshora.internal",
		Addr:           "127.0.0.1:0",
		Database:       types.DatabaseConfig{Type: types.DatabaseSqlite, Sqlite: types.SqliteConfig{Path: filepath.Join(directory, "db.sqlite")}},
		WebAdmin:       types.WebAdminConfig{Enabled: true, SetupEnabled: true, SessionTTL: time.Hour},
	}
	require.NoError(t, os.WriteFile(cfg.ConfigFilePath, []byte("server_url: http://127.0.0.1:8080\nweb_admin:\n  enabled: true\n  setup_enabled: true\ndns:\n  base_domain: meshora.internal\n"), 0o600))
	database, err := db.NewHeadscaleDatabase(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	handler := newSetupHandler(cfg, database)
	server := httptest.NewServer(handler.routes())
	t.Cleanup(server.Close)

	return &setupTest{testEnv: &testEnv{t: t, server: server, client: newClient(t)}, cfg: cfg, database: database, setup: handler}
}

func (e *setupTest) form() url.Values {
	e.t.Helper()
	response, body := e.get(e.client, "/setup")
	require.Equal(e.t, http.StatusOK, response.StatusCode)

	match := csrfRe.FindStringSubmatch(body)
	require.Len(e.t, match, 2)

	return url.Values{"csrf_token": {match[1]}, "server_url": {"https://mesh.example.com"}, "username": {"owner"}, "password": {testPassword}, "password_confirm": {testPassword}}
}

func TestInitialSetupPersistsFirstAdminAndConfig(t *testing.T) {
	e := newSetupTest(t)
	response, body := e.do(e.client, http.MethodPost, "/setup", e.form())
	require.Equal(t, http.StatusOK, response.StatusCode, body)
	assert.Contains(t, body, "Setup complete")
	assert.NotContains(t, body, testPassword)
	admin, err := e.database.AuthenticateWebAdmin("owner", testPassword)
	require.NoError(t, err)
	require.NotNil(t, admin)

	data, err := os.ReadFile(e.cfg.ConfigFilePath)
	require.NoError(t, err)
	assert.NotContains(t, string(data), testPassword)

	var config struct {
		ServerURL string `yaml:"server_url"`
		Admin     struct {
			Enabled bool `yaml:"enabled"`
			Setup   bool `yaml:"setup_enabled"`
		} `yaml:"web_admin"`
	}
	require.NoError(t, yaml.Unmarshal(data, &config))
	assert.Equal(t, "https://mesh.example.com", config.ServerURL)
	assert.True(t, config.Admin.Enabled)
	assert.False(t, config.Admin.Setup)

	info, err := os.Stat(e.cfg.ConfigFilePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	response, _ = e.do(e.client, http.MethodPost, "/setup", nil)
	assert.Equal(t, http.StatusGone, response.StatusCode)

	available, err := e.database.InitialSetupAvailable()
	require.NoError(t, err)
	assert.False(t, available)
	shutdown, err := RunInitialSetup(t.Context(), e.cfg)
	require.NoError(t, err)
	assert.Nil(t, shutdown, "an existing account closes setup even if the caller still has the old setup-enabled config")
}

func TestInitialSetupRejectsInvalidOrCrossSiteRequests(t *testing.T) {
	for _, name := range []string{"csrf", "password", "url", "origin", "config", "username", "partial_oidc"} {
		t.Run(name, func(t *testing.T) {
			e := newSetupTest(t)
			form := e.form()
			want := http.StatusBadRequest

			switch name {
			case "csrf":
				form.Set("csrf_token", "forged")

				want = http.StatusForbidden
			case "password":
				form.Set("password_confirm", "different password")
			case "url":
				form.Set("server_url", "javascript:alert(1)")
			case "config":
				require.NoError(t, os.WriteFile(e.cfg.ConfigFilePath, []byte("web_admin: []\n"), 0o600))
			case "username":
				form.Set("username", strings.Repeat("!", 12))
			case "partial_oidc":
				form.Set("client_secret", "never-reflect-this-secret")
			case "origin":
				request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, e.server.URL+"/setup", strings.NewReader(form.Encode()))
				require.NoError(t, err)
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				request.Header.Set("Origin", "https://attacker.example")
				response, err := e.client.Do(request)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				assert.Equal(t, http.StatusForbidden, response.StatusCode)

				admins, err := e.database.ListWebAdmins()
				require.NoError(t, err)
				assert.Empty(t, admins)

				return
			}

			response, body := e.do(e.client, http.MethodPost, "/setup", form)
			assert.Equal(t, want, response.StatusCode, body)
			assert.NotContains(t, body, testPassword)
			assert.NotContains(t, body, "never-reflect-this-secret")

			admins, err := e.database.ListWebAdmins()
			require.NoError(t, err)
			assert.Empty(t, admins)
		})
	}
}

func TestInitialSetupConcurrentFirstAccountClaim(t *testing.T) {
	e := newSetupTest(t)
	form := e.form()

	var group sync.WaitGroup

	statuses := make(chan int, 4)

	for range 4 {
		group.Go(func() {
			response, _ := e.do(e.client, http.MethodPost, "/setup", form)
			statuses <- response.StatusCode
		})
	}

	group.Wait()
	close(statuses)

	successes := 0

	for status := range statuses {
		if status == http.StatusOK {
			successes++
		} else {
			assert.Equal(t, http.StatusGone, status)
		}
	}

	assert.Equal(t, 1, successes)

	admins, err := e.database.ListWebAdmins()
	require.NoError(t, err)
	assert.Len(t, admins, 1)
}

func TestInitialSetupOIDCValidationAndSecretHandling(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "valid", false: "wrong_issuer"}[valid], func(t *testing.T) {
			e := newSetupTest(t)

			var issuer string

			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/.well-known/openid-configuration", r.URL.Path)

				declared := issuer
				if !valid {
					declared = "https://different.example"
				}

				_ = json.NewEncoder(w).Encode(map[string]string{"issuer": declared, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys"})
			}))
			defer provider.Close()

			issuer = provider.URL
			form := e.form()
			form.Set("issuer", issuer)
			form.Set("client_id", "setup-client")
			form.Set("client_secret", "unique-oidc-secret")
			response, body := e.do(e.client, http.MethodPost, "/setup", form)
			assert.NotContains(t, body, "unique-oidc-secret")

			if !valid {
				assert.Equal(t, http.StatusBadRequest, response.StatusCode)

				admins, err := e.database.ListWebAdmins()
				require.NoError(t, err)
				assert.Empty(t, admins)

				return
			}

			require.Equal(t, http.StatusOK, response.StatusCode, body)

			_, _, saved, err := readOIDCSettings(e.cfg.ConfigFilePath)
			require.NoError(t, err)
			assert.Equal(t, issuer, saved.Issuer)
			assert.Equal(t, "setup-client", saved.ClientID)
			assert.Equal(t, "unique-oidc-secret", saved.Secret)
		})
	}
}

func TestInitialSetupPageSecurityAndEscaping(t *testing.T) {
	e := newSetupTest(t)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, e.server.URL+"/setup", nil)
	require.NoError(t, err)
	request.Header.Set("X-Forwarded-Proto", "https")
	response, err := e.client.Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.Contains(t, response.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'")
	require.Len(t, response.Cookies(), 1)
	cookie := response.Cookies()[0]
	assert.True(t, cookie.Secure)
	assert.True(t, cookie.HttpOnly)
	assert.Equal(t, http.SameSiteStrictMode, cookie.SameSite)

	form := e.form()
	form.Set("server_url", `https://example.com/?"><script>alert(1)</script>`)
	// e.form uses e.client; keep that cookie for the invalid submitted values.
	responseValue, body := e.do(e.client, http.MethodPost, "/setup", form)
	assert.Equal(t, http.StatusBadRequest, responseValue.StatusCode)
	assert.NotContains(t, body, "<script>alert(1)</script>")
}

func TestInitialSetupURLValidation(t *testing.T) {
	for _, value := range []string{"javascript:alert(1)", "https://user:pass@example.com", "https://mesh.example/path", "https://mesh.example/?a=b", "https://mesh.example/#fragment", "https://mesh.example:99999", "http://0.0.0.0:8080", "https://meshora.internal", "https://server.meshora.internal"} {
		_, err := validateSetupServerURL(value, "meshora.internal")
		require.Error(t, err, value)
	}

	for _, value := range []string{"http://192.168.1.10:8080", "https://mesh.example.com/", "http://[::1]:8080"} {
		normalized, err := validateSetupServerURL(value, "meshora.internal")
		require.NoError(t, err, value)
		assert.NotEmpty(t, normalized)
	}
}

func TestInitialSetupRunDisabledAndCancellation(t *testing.T) {
	shutdown, err := RunInitialSetup(context.Background(), &types.Config{})
	require.NoError(t, err)
	assert.Nil(t, shutdown)
	e := newSetupTest(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	shutdown, err = RunInitialSetup(ctx, e.cfg)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, shutdown)
}
