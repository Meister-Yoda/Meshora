package webui

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const settingsIssuer = "https://auth.example.com/application/o/meshora/"

func settingsTestEnv(t *testing.T, config string) (*testEnv, string) {
	t.Helper()
	env := newTestEnv(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	//nolint:gosec // G306: verify that saving tightens an initially public config to 0600.
	require.NoError(t, os.WriteFile(path, []byte(config), 0o644))
	env.handler.configPath = path

	return env, path
}

func TestOIDCSettingsSave(t *testing.T) {
	env, path := settingsTestEnv(t, `# Keep this comment.
server_url: http://localhost:8080
web_admin:
  enabled: true
oidc:
  allowed_groups: [meshora-users]
  scope: [openid, profile, email, groups]
  pkce:
    enabled: true
    method: S256
`)
	csrf := env.signedIn()
	secret := "test-value-" + t.Name()
	location, page := env.action(csrf, "/admin/settings/oidc", url.Values{
		"issuer": {settingsIssuer}, "client_id": {"meshora"}, "client_secret": {secret},
	})
	assert.Equal(t, "/admin/settings", location)
	assert.Contains(t, page, "Settings saved.")
	assert.Contains(t, page, "Restart required")
	assert.Contains(t, page, "http://localhost:8080/oidc/callback")
	assert.Contains(t, page, `type="password"`)
	assert.NotContains(t, page, secret)
	assert.NotContains(t, location, secret)
	assert.NotContains(t, page, "<style")
	assert.NotContains(t, page, "style=")

	_, document, saved, err := readOIDCSettings(path)
	require.NoError(t, err)
	assert.Equal(t, settingsIssuer, saved.Issuer)
	assert.Equal(t, "meshora", saved.ClientID)
	assert.Equal(t, secret, saved.Secret)

	var config map[string]any
	require.NoError(t, document.Decode(&config))
	assert.Equal(t, "http://localhost:8080", config["server_url"])
	oidc, ok := config["oidc"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"meshora-users"}, oidc["allowed_groups"])
	assert.Equal(t, []any{"openid", "profile", "email", "groups"}, oidc["scope"])
	assert.Equal(t, map[string]any{"enabled": true, "method": "S256"}, oidc["pkce"])

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "# Keep this comment.")

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	_, page = env.get(env.client, "/admin/settings")
	assert.NotContains(t, page, secret)
	assert.Contains(t, page, "Leave blank")
}

func TestOIDCSettingsRetainAndRotateFileSecret(t *testing.T) {
	env, path := settingsTestEnv(t, `server_url: http://localhost:8080
oidc:
  issuer: https://old.example.com/
  client_id: old-client
  client_secret_path: oidc-secret
`)
	secret := "test-value-" + t.Name()
	secretPath := filepath.Join(filepath.Dir(path), "oidc-secret")
	require.NoError(t, os.WriteFile(secretPath, []byte(secret+"\n"), 0o600))

	csrf := env.signedIn()
	form := url.Values{"issuer": {settingsIssuer}, "client_id": {"new-client"}, "client_secret": {""}}
	_, page := env.action(csrf, "/admin/settings/oidc", form)
	assert.NotContains(t, page, secret)

	_, document, saved, err := readOIDCSettings(path)
	require.NoError(t, err)
	assert.Equal(t, secret, saved.Secret)
	assert.Equal(t, "oidc-secret", saved.SecretPath)
	assert.Nil(t, settingsMappingValue(settingsMappingValue(document.Content[0], "oidc"), "client_secret"))

	// A newly supplied secret replaces the reference without modifying its file.
	form.Set("client_secret", secret+"-rotated")
	_, page = env.action(csrf, "/admin/settings/oidc", form)
	assert.NotContains(t, page, secret)

	_, _, saved, err = readOIDCSettings(path)
	require.NoError(t, err)
	assert.Equal(t, secret+"-rotated", saved.Secret)
	assert.Empty(t, saved.SecretPath)

	unchanged, err := os.ReadFile(secretPath)
	require.NoError(t, err)
	assert.Equal(t, secret+"\n", string(unchanged))

	// Blank input retains the inline secret as well.
	form.Set("client_secret", "")
	env.action(csrf, "/admin/settings/oidc", form)

	_, _, saved, err = readOIDCSettings(path)
	require.NoError(t, err)
	assert.Equal(t, secret+"-rotated", saved.Secret)
}

func TestOIDCSettingsRequireSessionAndCSRF(t *testing.T) {
	initial := "server_url: http://localhost:8080\n"
	env, path := settingsTestEnv(t, initial)
	resp, _ := env.get(env.client, "/admin/settings")
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, "/admin/login", resp.Header.Get("Location"))

	form := url.Values{"issuer": {settingsIssuer}, "client_id": {"meshora"}, "client_secret": {"test-value"}}
	resp, _ = env.post(env.client, "/admin/settings/oidc", form)
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	env.signedIn()

	for _, csrf := range []string{"", "forged"} {
		form.Set("csrf_token", csrf)
		resp, _ = env.post(env.client, "/admin/settings/oidc", form)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	}

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, initial, string(data))
}

func TestOIDCSettingsInvalidInputLeavesConfigUntouched(t *testing.T) {
	initial := "server_url: http://localhost:8080\n"
	env, path := settingsTestEnv(t, initial)

	csrf := env.signedIn()
	for _, form := range []url.Values{
		{"issuer": {"http://auth.example.com/"}, "client_id": {"meshora"}, "client_secret": {"test-value"}},
		{"issuer": {settingsIssuer}, "client_id": {""}, "client_secret": {"test-value"}},
		{"issuer": {settingsIssuer}, "client_id": {"meshora"}, "client_secret": {""}},
	} {
		_, page := env.action(csrf, "/admin/settings/oidc", form)
		assert.NotContains(t, page, "Settings saved.")

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, initial, string(data))
	}
}

func TestOIDCSettingsEscapesClientID(t *testing.T) {
	env, path := settingsTestEnv(t, "server_url: http://localhost:8080\n")
	csrf := env.signedIn()
	_, page := env.action(csrf, "/admin/settings/oidc", url.Values{
		"issuer": {settingsIssuer}, "client_id": {xssPayload}, "client_secret": {"test-value"},
	})
	assertEscaped(t, page)

	_, document, _, err := readOIDCSettings(path)
	require.NoError(t, err)

	var scopes []string
	require.NoError(t, settingsMappingValue(settingsMappingValue(document.Content[0], "oidc"), "scope").Decode(&scopes))
	assert.Equal(t, []string{"openid", "profile", "email"}, scopes)
}

func TestOIDCSettingsMalformedConfig(t *testing.T) {
	initial := "oidc: [not-a-mapping]\n"
	env, path := settingsTestEnv(t, initial)
	csrf := env.signedIn()
	_, page := env.action(csrf, "/admin/settings/oidc", url.Values{
		"issuer": {settingsIssuer}, "client_id": {"meshora"}, "client_secret": {"test-value"},
	})
	assert.Contains(t, page, "could not be read")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, initial, string(data))
}

func TestValidateSettingsIssuer(t *testing.T) {
	for _, issuer := range []string{settingsIssuer, "http://localhost:8081/", "http://127.0.0.1:8081/", "http://[::1]:8081/"} {
		require.NoError(t, validateSettingsIssuer(issuer), issuer)
	}

	for _, issuer := range []string{"", "javascript:alert(1)", "http://auth.example.com/", "https://u:p@auth.example.com/", "https://auth.example.com/?token=x", "https://auth.example.com/#secret"} {
		assert.ErrorIs(t, validateSettingsIssuer(issuer), errSettingsIssuer, issuer)
	}
}

// A missing secret file can be repaired through the form with a new secret.
func TestOIDCSettingsReplaceMissingSecretFile(t *testing.T) {
	env, _ := settingsTestEnv(t, "oidc:\n  client_secret_path: missing-secret\n")
	csrf := env.signedIn()
	_, page := env.action(csrf, "/admin/settings/oidc", url.Values{
		"issuer": {settingsIssuer}, "client_id": {"meshora"}, "client_secret": {"test-value"},
	})
	assert.Contains(t, page, "Settings saved.")
}

func TestOIDCSettingsAdminGroups(t *testing.T) {
	env, path := settingsTestEnv(t, `server_url: http://localhost:8080
web_admin:
  enabled: true
  session_ttl: 8h
oidc:
  issuer: https://auth.example.com/application/o/meshora/
  client_id: meshora
  client_secret: test-secret
  allowed_groups: [device-users]
`)
	csrf := env.signedIn()
	form := url.Values{
		"issuer": {settingsIssuer}, "client_id": {"meshora"},
		"admin_groups": {" meshora-admins, operators, "},
	}
	_, page := env.action(csrf, "/admin/settings/oidc", form)
	assert.Contains(t, page, "meshora-admins, operators")
	assert.Contains(t, page, "/admin/oidc/callback")

	_, document, _, err := readOIDCSettings(path)
	require.NoError(t, err)

	var config map[string]any
	require.NoError(t, document.Decode(&config))
	assert.Equal(t, map[string]any{
		"enabled": true, "session_ttl": "8h", "oidc": map[string]any{"allowed_groups": []any{"meshora-admins", "operators"}},
	}, config["web_admin"])
	oidcSettings, ok := config["oidc"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"device-users"}, oidcSettings["allowed_groups"])
	// The running server must only activate saved permissions after restart.
	assert.Empty(t, env.handler.adminOIDCGroups)
	form.Set("admin_groups", "")
	env.action(csrf, "/admin/settings/oidc", form)

	_, document, _, err = readOIDCSettings(path)
	require.NoError(t, err)

	groups := settingsMappingValue(settingsMappingValue(settingsMappingValue(document.Content[0], "web_admin"), "oidc"), "allowed_groups")

	var values []string
	require.NoError(t, groups.Decode(&values))
	assert.Empty(t, values)
}

func TestOIDCButtonTextSettings(t *testing.T) {
	env, path := settingsTestEnv(t, `server_url: http://localhost:8080
web_admin:
  enabled: true
  oidc:
    allowed_groups: [meshora-admins]
`)
	resp, _ := env.post(env.client, "/admin/settings/oidc-button", url.Values{"button_text": {"SSO"}})
	assert.Equal(t, "/admin/login", resp.Header.Get("Location"))

	csrf := env.signedIn()
	resp, _ = env.post(env.client, "/admin/settings/oidc-button", url.Values{"button_text": {"SSO"}})
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	label := `Company SSO <script>alert("x")</script>`
	_, page := env.action(csrf, "/admin/settings/oidc-button", url.Values{"button_text": {label}})
	assert.Contains(t, page, "active immediately")
	assert.NotContains(t, page, "<script>")
	assert.Contains(t, page, "&lt;script&gt;")

	_, document, _, err := readOIDCSettings(path)
	require.NoError(t, err)

	var saved struct {
		Admin struct {
			OIDC struct {
				Text   string   `yaml:"button_text"`
				Groups []string `yaml:"allowed_groups"`
			} `yaml:"oidc"`
		} `yaml:"web_admin"`
	}
	require.NoError(t, document.Decode(&saved))
	assert.Equal(t, label, saved.Admin.OIDC.Text)
	assert.Equal(t, []string{"meshora-admins"}, saved.Admin.OIDC.Groups)
	assert.Nil(t, settingsMappingValue(document.Content[0], "oidc"), "Changing button text must not require or create provider credentials")
	// Render through the public handler: an anonymous browser immediately sees it.
	env.handler.activeOIDC.Issuer = settingsIssuer
	env.handler.activeOIDC.ClientID = "meshora"
	env.handler.adminOIDCGroups = []string{"meshora-admins"}
	_, page = env.get(newClient(t), "/admin/login?local=1")
	assert.Contains(t, page, "Company SSO &lt;script&gt;")
	assert.NotContains(t, page, "<script>")

	previous, err := os.ReadFile(path)
	require.NoError(t, err)

	for _, invalid := range []string{strings.Repeat("a", 101), "line1\nline2"} {
		env.action(csrf, "/admin/settings/oidc-button", url.Values{"button_text": {invalid}})

		unchanged, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, previous, unchanged)
		assert.Equal(t, label, env.handler.oidcButtonText)
	}

	env.action(csrf, "/admin/settings/oidc-button", url.Values{"button_text": {"  "}})
	_, page = env.get(newClient(t), "/admin/login?local=1")
	assert.Contains(t, page, "Sign in with Authentik")
	assert.Empty(t, env.handler.oidcButtonText)
}

func TestOIDCSettingsAdminRolesPersistIndependently(t *testing.T) {
	env, path := settingsTestEnv(t, `server_url: http://localhost:8080
web_admin:
  enabled: true
  oidc:
    allowed_groups: [meshora-admins]
    button_text: Company SSO
oidc:
  issuer: https://auth.example.com/application/o/meshora/
  client_id: meshora
  client_secret: test-secret
`)
	csrf := env.signedIn()
	form := url.Values{"issuer": {settingsIssuer}, "client_id": {"meshora"}, "admin_roles": {" meshora-admin, operator "}, "roles_claim": {"app_roles"}}
	_, page := env.action(csrf, "/admin/settings/oidc", form)
	assert.Contains(t, page, "Restart required")

	_, document, _, err := readOIDCSettings(path)
	require.NoError(t, err)

	adminOIDC := settingsMappingValue(settingsMappingValue(document.Content[0], "web_admin"), "oidc")

	var values struct {
		Groups []string `yaml:"allowed_groups"`
		Roles  []string `yaml:"allowed_roles"`
		Claim  string   `yaml:"roles_claim"`
		Text   string   `yaml:"button_text"`
	}
	require.NoError(t, adminOIDC.Decode(&values))
	assert.Equal(t, []string{"meshora-admins"}, values.Groups)
	assert.Equal(t, []string{"meshora-admin", "operator"}, values.Roles)
	assert.Equal(t, "app_roles", values.Claim)
	assert.Equal(t, "Company SSO", values.Text)
	assert.Empty(t, env.handler.adminOIDCRoles, "Authorization restrictions take effect only after restart")

	previous, err := os.ReadFile(path)
	require.NoError(t, err)
	form.Set("roles_claim", "invalid claim")
	env.action(csrf, "/admin/settings/oidc", form)

	current, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, previous, current)
}
