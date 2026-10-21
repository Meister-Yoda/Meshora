package webui

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	assert.Contains(t, page, "Gespeichert.")
	assert.Contains(t, page, "Neustart erforderlich")
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
	assert.Contains(t, page, "Leer lassen")
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
		assert.NotContains(t, page, "Gespeichert.")

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
	assert.Contains(t, page, "konnte nicht gelesen werden")

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
	assert.Contains(t, page, "Gespeichert.")
}
