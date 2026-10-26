package webui

import (
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"tailscale.com/tailcfg"
	"tailscale.com/types/dnstype"
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

	_, callbacks := env.get(env.client, "/admin/settings/provider")
	assert.Contains(t, callbacks, "http://localhost:8080/oidc/callback")
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
	assert.Equal(t, "/", resp.Header.Get("Location"))

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
		"admin_groups":     {" meshora-admins, operators, "},
		"settings_section": {"access"},
	}
	_, page := env.action(csrf, "/admin/settings/oidc", form)
	assert.Contains(t, page, "meshora-admins, operators")

	_, callbacks := env.get(env.client, "/admin/settings/provider")
	assert.Contains(t, callbacks, "/admin/oidc/callback")

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
	assert.Equal(t, "/", resp.Header.Get("Location"))

	csrf := env.signedIn()
	resp, _ = env.post(env.client, "/admin/settings/oidc-button", url.Values{"button_text": {"SSO"}})
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	label := `Company SSO <script>alert("x")</script>`
	_, page := env.action(csrf, "/admin/settings/oidc-button", url.Values{"button_text": {label}, "settings_section": {"appearance"}})
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
	_, page = env.get(newClient(t), "/?local=1")
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
	_, page = env.get(newClient(t), "/?local=1")
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

func TestSettingsSubpages(t *testing.T) {
	env, _ := settingsTestEnv(t, "server_url: http://localhost:8080\n")
	env.signedIn()

	for _, tc := range []struct{ section, want, absent string }{
		{"provider", `id="f-issuer"`, `id="f-admin_roles"`},
		{"access", `id="f-admin_roles"`, `id="f-issuer"`},
		{"appearance", `id="f-button_text"`, `id="f-admin_roles"`},
	} {
		resp, page := env.get(env.client, "/admin/settings/"+tc.section)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, page, tc.want)
		assert.NotContains(t, page, tc.absent)
		assert.Contains(t, page, `id="settings-`+tc.section+`"`)
	}

	resp, _ := env.get(env.client, "/admin/settings/callbacks")
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, "/admin/settings/provider", resp.Header.Get("Location"))
	resp, _ = env.get(env.client, "/admin/settings/unknown")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestOIDCSettingsUserGroups(t *testing.T) {
	env, path := settingsTestEnv(t, `server_url: http://localhost:8080
web_admin:
  enabled: true
  oidc:
    allowed_groups: [meshora-admins]
oidc:
  issuer: https://auth.example.com/application/o/meshora/
  client_id: meshora
  client_secret: test-secret
  allowed_groups: [existing-users]
  scope: [openid, profile, email, groups]
`)
	csrf := env.signedIn()
	_, page := env.get(env.client, "/admin/settings/access")
	assert.Contains(t, page, `name="user_groups"`)
	assert.Contains(t, page, `value="existing-users"`)

	form := url.Values{
		"issuer": {settingsIssuer}, "client_id": {"meshora"},
		"user_groups":      {" meshora-users, operators, , "},
		"settings_section": {"access"},
	}
	_, page = env.action(csrf, "/admin/settings/oidc", form)
	assert.Contains(t, page, `value="meshora-users, operators"`)
	assert.Contains(t, page, "Restart required")

	_, document, saved, err := readOIDCSettings(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"meshora-users", "operators"}, saved.AllowedGroups)

	admin := settingsMappingValue(settingsMappingValue(document.Content[0], "web_admin"), "oidc")

	var adminGroups []string
	require.NoError(t, settingsMappingValue(admin, "allowed_groups").Decode(&adminGroups))
	assert.Equal(t, []string{"meshora-admins"}, adminGroups)
	assert.Empty(t, env.handler.activeOIDC.AllowedGroups, "permissions only change after restart")

	// Saving the provider page must preserve the separately configured groups.
	form.Del("user_groups")
	form.Set("settings_section", "provider")
	env.action(csrf, "/admin/settings/oidc", form)

	_, _, saved, err = readOIDCSettings(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"meshora-users", "operators"}, saved.AllowedGroups)

	// Clearing the field explicitly removes the group restriction.
	form.Set("user_groups", "")
	form.Set("settings_section", "access")
	env.action(csrf, "/admin/settings/oidc", form)

	_, _, saved, err = readOIDCSettings(path)
	require.NoError(t, err)
	assert.Empty(t, saved.AllowedGroups)
}

const dnsTestConfig = `# retain top-level comment
server_url: http://localhost:8080
prefixes:
  v4: 100.64.0.0/10
  v6: fd7a:115c:a1e0::/48
noise:
  private_key_path: noise_private.key
database:
  type: sqlite3
  sqlite:
    path: test.db
dns:
  magic_dns: true
  base_domain: mesh.example.test
  override_local_dns: true
  nameservers:
    global:
      - 192.0.2.53 # retain resolver comment
    split:
      corp.example.test: [192.0.2.54] # retain split comment
      local.example.test: []
    future_resolver_option: keep-me
  search_domains: [corp.example.test]
  extra_records:
    - {name: app.mesh.example.test, type: A, value: 100.64.0.9}
  future_dns_option: {enabled: true, values: [one, two]}
unrelated: {keep: "yes"}
`

func dnsFormFor(t *testing.T, env *testEnv) url.Values {
	t.Helper()

	response, page := env.get(env.client, "/admin/dns?lang=en")
	require.Equal(t, http.StatusOK, response.StatusCode)

	match := regexp.MustCompile(`name="revision"[^>]*value="([^"]+)"`).FindStringSubmatch(page)
	require.Len(t, match, 2, page)

	return url.Values{
		"revision": {match[1]}, "magic_dns": {"true"}, "base_domain": {"mesh.example.test"},
		"override_local_dns": {"true"}, "global": {"192.0.2.53"}, "search": {"corp.example.test"},
		"split": {"corp.example.test: [192.0.2.54]\nlocal.example.test: []"},
	}
}

func dnsSetTestRuntime(env *testEnv) {
	cfg := env.handler.runtimeConfig
	cfg.DNSConfig.MagicDNS = true
	cfg.DNSConfig.BaseDomain = "mesh.example.test"
	cfg.DNSConfig.OverrideLocalDNS = true
	cfg.DNSConfig.Nameservers.Global = []string{"192.0.2.53"}
	cfg.DNSConfig.Nameservers.Split = map[string][]string{"corp.example.test": {"192.0.2.54"}, "local.example.test": {}}
	cfg.DNSConfig.SearchDomains = []string{"corp.example.test"}
	cfg.TailcfgDNSConfig = &tailcfg.DNSConfig{
		Proxied: true, Resolvers: []*dnstype.Resolver{{Addr: "192.0.2.53"}},
		Domains:      []string{"mesh.example.test", "corp.example.test"},
		Routes:       map[string][]*dnstype.Resolver{"corp.example.test": {{Addr: "192.0.2.54"}}, "local.example.test": {}, "64.100.in-addr.arpa": {}},
		ExtraRecords: []tailcfg.DNSRecord{{Name: "app.mesh.example.test", Type: "A", Value: "100.64.0.9"}},
	}
}

func TestDNSSettingsPreserveAndRestart(t *testing.T) {
	env, path := settingsTestEnv(t, dnsTestConfig)
	dnsSetTestRuntime(env)
	csrf := env.signedIn()
	_, page := env.get(env.client, "/admin/dns?lang=en")
	assert.NotContains(t, page, "Saved DNS settings differ")
	assert.Contains(t, page, "64.100.in-addr.arpa")
	assert.Contains(t, page, "Resolved locally by the client")
	assert.Contains(t, page, "This does not confirm that a client applied it or resolved a name")
	form := dnsFormFor(t, env)
	form.Set("global", "192.0.2.55\n2001:db8::53\nhttps://dns.example.test/dns-query?profile=keep")
	form.Set("search", "corp.example.test\nlab.example.test.")
	form.Set("split", "corp.example.test: [192.0.2.54]\nlocal.example.test: []\nlab.example.test: [2001:db8::54]")

	previous := env.handler.runtimeConfig.CloneTailcfgDNSConfig()
	_, page = env.action(csrf, "/admin/dns", form)
	assert.Contains(t, page, "DNS settings saved.")
	assert.Contains(t, page, "Saved DNS settings differ")
	assert.Equal(t, previous, env.handler.runtimeConfig.CloneTailcfgDNSConfig(), "saving must not activate DNS")
	assert.Equal(t, "192.0.2.53", env.handler.runtimeConfig.DNSConfig.Nameservers.Global[0])
	assert.Zero(t, env.changeCount(), "saving must not send a DNS activation to clients")

	_, doc, saved, _, err := readDNSSettings(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"192.0.2.55", "2001:db8::53", "https://dns.example.test/dns-query?profile=keep"}, saved.Nameservers.Global)
	assert.Contains(t, saved.Nameservers.Split, "local.example.test")
	assert.Empty(t, saved.Nameservers.Split["local.example.test"])

	var decoded map[string]any
	require.NoError(t, doc.Decode(&decoded))
	assert.Equal(t, map[string]any{"keep": "yes"}, decoded["unrelated"])
	dns, ok := decoded["dns"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"enabled": true, "values": []any{"one", "two"}}, dns["future_dns_option"])
	nameservers, ok := dns["nameservers"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "keep-me", nameservers["future_resolver_option"])
	assert.NotContains(t, dns, "extra_records_path")
	assert.Len(t, dns["extra_records"], 1)

	bytes, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(bytes), "# retain top-level comment")
	assert.Contains(t, string(bytes), "# retain split comment")
	assert.Contains(t, string(bytes), "local.example.test: []")

	stat, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), stat.Mode().Perm())
	// Run the actual startup configuration loader in a subprocess: no shared
	// Viper globals, network traffic, database opening or production files.
	resultPath := filepath.Join(t.TempDir(), "loaded.json")
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestDNSConfigLoaderProcess$") //nolint:gosec // current test binary

	cmd.Env = append(os.Environ(), "MESHORA_DNS_TEST_CONFIG="+path, "MESHORA_DNS_TEST_RESULT="+resultPath)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	result, err := os.ReadFile(resultPath)
	require.NoError(t, err)

	var loaded struct {
		DNS     types.DNSConfig
		Tailcfg *tailcfg.DNSConfig
	}
	require.NoError(t, json.Unmarshal(result, &loaded))
	assert.Equal(t, saved.Nameservers.Global, loaded.DNS.Nameservers.Global)
	assert.Equal(t, saved.SearchDomains, loaded.DNS.SearchDomains)
	assert.Equal(t, saved.Nameservers.Split, loaded.DNS.Nameservers.Split)
	require.NotNil(t, loaded.Tailcfg)
	assert.Equal(t, saved.Nameservers.Global[0], loaded.Tailcfg.Resolvers[0].Addr)
	assert.Contains(t, loaded.Tailcfg.Clone().Routes, "local.example.test", "empty resolver list must survive client config cloning")
	// A newly constructed handler sees the new startup snapshot, not stale values.
	env.handler.runtimeConfig = &types.Config{DNSConfig: loaded.DNS, TailcfgDNSConfig: loaded.Tailcfg}
	_, page = env.get(env.client, "/admin/dns?lang=en")
	assert.NotContains(t, page, "Saved DNS settings differ")
	assert.Contains(t, page, "2001:db8::54")
}

func TestDNSConfigLoaderProcess(t *testing.T) {
	path := os.Getenv("MESHORA_DNS_TEST_CONFIG")
	if path == "" {
		t.Skip("isolated startup loader subprocess")
	}

	require.NoError(t, types.LoadConfig(path, true))

	cfg, err := types.LoadServerConfig()
	require.NoError(t, err)
	data, err := json.Marshal(struct {
		DNS     types.DNSConfig
		Tailcfg *tailcfg.DNSConfig
	}{cfg.DNSConfig, cfg.CloneTailcfgDNSConfig()})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(os.Getenv("MESHORA_DNS_TEST_RESULT"), data, 0o600)) //nolint:gosec // G703: private parent-created test output path, in an isolated child process.
}

func TestDNSValidation(t *testing.T) {
	good := url.Values{
		"magic_dns": {"true"}, "base_domain": {"mesh.example.test"}, "override_local_dns": {"true"},
		"global": {"192.0.2.53\n2001:db8::53\nhttps://dns.example.test/dns-query"},
		"search": {"corp.example.test"}, "split": {"corp.example.test: [192.0.2.54]\nlocal.example.test: []"},
	}

	for _, tc := range []struct{ name, key, value string }{
		{"missing base", "base_domain", ""},
		{"trailing base dot", "base_domain", "mesh.example.test."},
		{"base labels", "base_domain", "bad..example"},
		{"base overlaps control", "base_domain", "example.test"},
		{"base case overlaps", "base_domain", "CONTROL.EXAMPLE.TEST"},
		{"bad global", "global", "not a resolver"},
		{"http global", "global", "http://dns.example.test/dns-query"},
		{"credentials", "global", "https://user:password@dns.example.test/dns-query"},
		{"bad port", "global", "https://dns.example.test:99999/dns-query"},
		{"empty override", "global", ""},
		{"bad search", "search", "has spaces.example.test"},
		{"bad split YAML", "split", "["},
		{"split scalar", "split", "corp.example.test: 192.0.2.53"},
		{"null resolver", "split", "corp.example.test: null"},
		{"bad resolver", "split", "corp.example.test: [file:///etc/hosts]"},
		{"split duplicate", "split", "corp.example.test: []\ncorp.example.test: []"},
		{"split alias", "split", "corp.example.test: &dns [192.0.2.53]\nlab.example.test: *dns"},
		{"split uppercase", "split", "Corp.example.test: []"},
		{"split documents", "split", "{}\n---\n{}"},
		{"invalid boolean", "magic_dns", "yes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{}
			for key, values := range good {
				form[key] = slices.Clone(values)
			}

			form.Set(tc.key, tc.value)
			_, _, err := parseDNSForm(form, "https://control.example.test")
			assert.NotEmpty(t, err)
		})
	}

	_, _, validation := parseDNSForm(good, "https://control.example.test")
	assert.Empty(t, validation)
	good.Set("magic_dns", "false")
	good.Set("base_domain", "")
	good.Set("override_local_dns", "false")
	good.Set("global", "")
	good.Set("search", "")
	good.Set("split", "{}")
	_, _, validation = parseDNSForm(good, "https://control.example.test")
	assert.Empty(t, validation, "disabling MagicDNS and using local DNS must permit empty lists")
	good.Del("split")
	_, _, validation = parseDNSForm(good, "https://control.example.test")
	assert.NotEmpty(t, validation, "missing fields must not erase config")
}

func TestDNSSettingsConflictValidationAndOverrides(t *testing.T) {
	env, path := settingsTestEnv(t, dnsTestConfig)
	dnsSetTestRuntime(env)
	csrf := env.signedIn()
	form := dnsFormFor(t, env)
	form.Set("csrf_token", csrf)

	original, err := os.ReadFile(path)
	require.NoError(t, err)
	form.Set("global", "<script>alert(1)</script>")
	resp, page := env.post(env.client, "/admin/dns", form)
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	assert.Contains(t, page, "&lt;script&gt;")
	assert.NotContains(t, page, "<script>")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, original, data)
	form.Set("global", "192.0.2.60")

	external := append(slices.Clone(original), []byte("\nexternal_value: retained\n")...)
	require.NoError(t, os.WriteFile(path, external, 0o600)) //nolint:gosec // G703: path belongs to settingsTestEnv and t.TempDir, never user input.

	resp, page = env.post(env.client, "/admin/dns", form)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Contains(t, page, "configuration file changed")

	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, external, data)
	form = dnsFormFor(t, env)
	form.Set("csrf_token", csrf)
	t.Setenv("HEADSCALE_DNS_NAMESERVERS_GLOBAL", "198.51.100.53")

	resp, page = env.post(env.client, "/admin/dns", form)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Contains(t, page, "environment overrides take precedence")
	assert.Contains(t, page, "HEADSCALE_DNS_NAMESERVERS_GLOBAL")
	assert.NotContains(t, page, "198.51.100.53", "override values are not leaked")
	_, page = env.get(env.client, "/admin/dns?lang=de")
	assert.Contains(t, page, "DNS-Umgebungsvariablen haben Vorrang")
	assert.NotContains(t, page, `action="/admin/dns"`)

	resp, _ = env.post(env.client, "/admin/dns", url.Values{"revision": {form.Get("revision")}})
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	resp, _ = env.get(newClient(t), "/admin/dns")
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
}

func TestDNSSettingsAmbiguousFilesAreReadOnly(t *testing.T) {
	for _, config := range []string{
		"dns: &dns {magic_dns: false}\n",
		"defaults: &dns {magic_dns: false}\ndns: *dns\n",
		"defaults: &defaults {dns: {magic_dns: false}}\n<<: *defaults\n",
		"dns: {magic_dns: false, magic_dns: true}\n",
		"dns: {}\n---\ndns: {}\n",
		"dns: {nameservers: [invalid]}\n",
	} {
		t.Run(config, func(t *testing.T) {
			env, path := settingsTestEnv(t, config)
			csrf := env.signedIn()
			_, page := env.get(env.client, "/admin/dns?lang=en")
			assert.Contains(t, page, "Read-only DNS settings")

			resp, _ := env.post(env.client, "/admin/dns", url.Values{"csrf_token": {csrf}})
			assert.Equal(t, http.StatusConflict, resp.StatusCode)

			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, config, string(data))
		})
	}
}

func TestDNSFileRecordsReferencePreserved(t *testing.T) {
	config := strings.Replace(dnsTestConfig, "  extra_records:\n    - {name: app.mesh.example.test, type: A, value: 100.64.0.9}", "  extra_records_path: /tmp/isolated-extra-records.json", 1)
	env, path := settingsTestEnv(t, config)
	dnsSetTestRuntime(env)
	csrf := env.signedIn()
	form := dnsFormFor(t, env)
	form.Set("global", "192.0.2.60")
	env.action(csrf, "/admin/dns", form)

	resolved, doc, saved, revision, err := readDNSSettings(path)
	assert.Equal(t, path, resolved)
	assert.NotEmpty(t, revision)
	assert.Equal(t, []string{"192.0.2.60"}, saved.Nameservers.Global)
	require.NoError(t, err)

	var data map[string]any
	require.NoError(t, doc.Decode(&data))
	dns, ok := data["dns"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "/tmp/isolated-extra-records.json", dns["extra_records_path"])
	assert.NotContains(t, dns, "extra_records")
}

const policyViewFixture = `{
 // Keep this exact comment in the complete source.
 "groups": {"group:ops": ["alice@", "group:auditors"], "group:auditors": ["bob@"]},
 "hosts": {"application": "192.0.2.80"},
 "tagOwners": {"tag:router": ["group:ops"], "tag:service": ["tag:router"]},
 "acls": [{"action":"accept","src":["group:ops"],"dst":["tag:service:443"],"proto":"tcp","futureNested":{"keep":"nested-value"}}],
 "grants": [{"src":["tag:router"],"dst":["application"],"ip":["tcp:443"],"app":{"example.test/cap":[{"value":true}]},"via":["tag:router"]}],
 "autoApprovers": {"routes":{"192.0.2.0/24":["tag:router"]},"exitNode":["group:ops"],"futureApproval":["preserved"]},
 "ssh": [{"action":"check","src":["group:ops"],"dst":["tag:service"],"users":["deploy"],"checkPeriod":"1h","acceptEnv":["LANG"]}],
 "nodeAttrs": [{"target":["tag:router"],"attr":["nextdns:test-profile","nextdns:no-device-info"],"ipPool":["100.64.0.0/24"]}],
 "tests": [{"src":"alice@","accept":["tag:service:443"],"deny":["tag:service:22"]}],
 "sshTests": [{"src":"alice@","dst":"tag:service","user":"deploy","check":true}],
 "randomizeClientPort": true,
 "futureSection": {"nested":["<script>alert(1)</script>",{"untouched":true}]},
}`

func TestPolicyViewDatabaseReadOnly(t *testing.T) {
	env := newTestEnv(t)
	stored, err := env.state.SetPolicyInDB(policyViewFixture)
	require.NoError(t, err)

	owner := env.state.CreateUserForTest("policy-creator")
	node := env.state.CreateRegisteredNodeForTest(owner, "policy-router")
	node.User = owner
	node.Tags = []string{"tag:router"}
	env.state.PutNodeInStoreForTest(*node)
	csrf := env.signedIn()
	beforeDNS := env.handler.runtimeConfig.CloneTailcfgDNSConfig()
	response, page := env.get(env.client, "/admin/policy?lang=en")
	require.Equal(t, http.StatusOK, response.StatusCode)

	for _, text := range []string{
		"Read-only policy source overview", "Database", strconv.FormatUint(uint64(stored.ID), 10),
		"Network rules (ACLs)", "Groups", "Tags and tag owners", "Automatic route approvals",
		"Network and capability grants", "SSH rules", "Device attributes", "Declared policy tests",
		"Declared SSH tests", "Client port randomization", "Additional policy section",
		"futureSection", "futureNested", "futureApproval", "nested-value", "test-profile",
		"Recorded tagged devices", "policy-router", "Owning tags",
		"Loaded revision",
		"Configured rules do not prove allowed access",
		"Policy reloads do not activate DNS settings",
		"Keep this exact comment", "&lt;script&gt;",
	} {
		assert.Contains(t, page, text)
	}

	assert.NotContains(t, page, "<script>")
	assert.NotContains(t, page, "policy-creator", "creator must not be presented as tagged-device owner")
	assert.NotContains(t, page, `action="/admin/policy"`)

	actual, err := env.state.GetPolicy()
	require.NoError(t, err)
	assert.Equal(t, stored.ID, actual.ID)
	assert.Equal(t, policyViewFixture, actual.Data)
	assert.Zero(t, env.changeCount())
	assert.Equal(t, beforeDNS, env.handler.runtimeConfig.CloneTailcfgDNSConfig())
	resp, _ := env.post(env.client, "/admin/policy", url.Values{"csrf_token": {csrf}, "policy": {"{}"}})
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
	resp, _ = env.get(newClient(t), "/admin/policy")
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)

	_, german := env.get(env.client, "/admin/policy?lang=de")
	assert.Contains(t, german, "Nur lesende Übersicht")
	assert.Contains(t, german, "Geladene Revision")
}

func TestPolicyViewSourceIsNotLoadedProof(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.state.SetPolicy([]byte(`{"groups":{"group:loaded":["alice@"]}}`))
	require.NoError(t, err)
	// Store a distinct source without reloading: the page must never label it active.
	_, err = env.state.SetPolicyInDB(`{"groups":{"group:source":["bob@"]}}`)
	require.NoError(t, err)
	env.signedIn()
	_, page := env.get(env.client, "/admin/policy?lang=en")
	assert.Contains(t, page, "group:source")
	assert.NotContains(t, page, "group:loaded")
	assert.Contains(t, page, "source and loaded snapshot are separate evidence")
	assert.Zero(t, env.changeCount())
}

func TestPolicyViewFileStates(t *testing.T) {
	env := newTestEnv(t)
	path := filepath.Join(t.TempDir(), "policy.hujson")
	env.handler.runtimeConfig.ConfigFilePath = filepath.Join(filepath.Dir(path), "config.yaml")
	env.handler.runtimeConfig.Policy.Mode = types.PolicyModeFile
	env.handler.runtimeConfig.Policy.Path = filepath.Base(path)
	env.signedIn()
	_, page := env.get(env.client, "/admin/policy?lang=en")
	assert.Contains(t, page, "policy source could not be read completely")

	for _, tc := range []struct{ name, source, want string }{
		{"empty", "", "No explicit policy in the source"},
		{"minimal", "{}", "Show complete policy source"},
		{"invalid", "{ invalid", "The source cannot be structured"},
		{"wrong shape", "[1,2]", "The source cannot be structured"},
		{"full", policyViewFixture, "futureSection"},
		{"duplicate", `{"groups":{"group:first":["alice@"]},"groups":{"group:second":["bob@"]}}`, "group:first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, os.WriteFile(path, []byte(tc.source), 0o600))

			response, body := env.get(env.client, "/admin/policy?lang=en")
			assert.Equal(t, http.StatusOK, response.StatusCode)
			assert.Contains(t, body, tc.want)

			if tc.name == "duplicate" {
				assert.Contains(t, body, "group:second")
			}

			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, tc.source, string(data))
		})
	}

	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat(" ", maxPolicyDisplayBytes+1)), 0o600))

	_, page = env.get(env.client, "/admin/policy?lang=en")
	assert.Contains(t, page, "policy exceeds the display size limit")
	assert.NotContains(t, page, "Show complete policy source")

	env.handler.runtimeConfig.Policy.Path = ""
	_, page = env.get(env.client, "/admin/policy?lang=en")
	assert.Contains(t, page, "No explicit policy in the source")
	assert.Zero(t, env.changeCount())
}

func TestPolicyDisplayPreservesFields(t *testing.T) {
	sections, err := policyDisplaySections([]byte(policyViewFixture))
	require.NoError(t, err)
	require.Len(t, sections, 12)
	assert.Equal(t, "groups", sections[0].Key)
	assert.Equal(t, "futureSection", sections[11].Key)
	assert.Contains(t, sections[3].Entries[0].Fields[4].Value, "nested-value")

	fields, err := policyObjectFields([]byte(`{"duplicate":1,"duplicate":2,"nested":{"x":1,"x":2}}`))
	require.NoError(t, err)
	require.Len(t, fields, 3)
	assert.Equal(t, "1", fields[0].Value)
	assert.Equal(t, "2", fields[1].Value)
	assert.Contains(t, fields[2].Value, `"x": 1`)
	assert.Contains(t, fields[2].Value, `"x": 2`)
}

var errPolicyEditorWebWrite = errors.New("injected editor source failure")

const (
	policyEditorOriginal  = "{\n// original source\n\"acls\":[]\n}"
	policyEditorCandidate = "{\n// retain this comment exactly\n\"acls\":[],\"hosts\":{\"app\":\"192.0.2.80\"}\n}"
)

func policyEditorTestEnv(t *testing.T, mode types.PolicyMode) (*testEnv, string) {
	t.Helper()
	env := newTestEnv(t)
	cfg := env.handler.runtimeConfig
	cfg.Policy.Mode = mode

	path := ""
	if mode == types.PolicyModeFile {
		path = filepath.Join(t.TempDir(), "policy.hujson")
		require.NoError(t, os.WriteFile(path, []byte(policyEditorOriginal), 0o600))
		cfg.Policy.Path = path
	} else {
		_, err := env.state.SetPolicyInDB(policyEditorOriginal)
		require.NoError(t, err)
	}

	_, err := env.state.SetPolicy([]byte(policyEditorOriginal))
	require.NoError(t, err)

	return env, path
}

func policyEditorField(t *testing.T, page, name string) string {
	t.Helper()

	match := regexp.MustCompile(`name="` + regexp.QuoteMeta(name) + `"[^>]*value="([^"]*)"`).FindStringSubmatch(page)
	require.Len(t, match, 2, name)

	return html.UnescapeString(match[1])
}

func policyEditorForm(t *testing.T, page, csrf, text string) url.Values {
	t.Helper()

	return url.Values{
		"csrf_token": {csrf}, "policy_text": {text},
		"source_revision":  {policyEditorField(t, page, "source_revision")},
		"loaded_revision":  {policyEditorField(t, page, "loaded_revision")},
		"validation_token": {policyEditorField(t, page, "validation_token")},
	}
}

func TestPolicyEditorFullFlow(t *testing.T) {
	for _, mode := range []types.PolicyMode{types.PolicyModeFile, types.PolicyModeDB} {
		t.Run(string(mode), func(t *testing.T) {
			env, path := policyEditorTestEnv(t, mode)
			csrf := env.signedIn()
			_, page := env.get(env.client, "/admin/policy/edit?lang=en")
			source, loaded, err := env.state.PolicyEditorState()
			require.NoError(t, err)
			form := policyEditorForm(t, page, csrf, policyEditorCandidate)
			response, page := env.post(env.client, "/admin/policy/validate", form)
			require.Equal(t, http.StatusOK, response.StatusCode)
			assert.Contains(t, page, "Change preview")
			assert.Contains(t, page, "retain this comment exactly")
			assert.Contains(t, page, "2. Save validated draft")

			current, snapshot, err := env.state.PolicyEditorState()
			require.NoError(t, err)
			assert.Equal(t, source, current)
			assert.Equal(t, loaded, snapshot)
			form = policyEditorForm(t, page, csrf, policyEditorCandidate)
			response, _ = env.post(env.client, "/admin/policy/save", form)
			require.Equal(t, http.StatusSeeOther, response.StatusCode)

			current, snapshot, err = env.state.PolicyEditorState()
			require.NoError(t, err)
			assert.Equal(t, policyEditorCandidate, current.Text)
			assert.Equal(t, loaded, snapshot)
			assert.Zero(t, env.changeCount())

			if path != "" {
				raw, readErr := os.ReadFile(path)
				require.NoError(t, readErr)
				assert.Equal(t, policyEditorCandidate, string(raw))
			}

			_, page = env.get(env.client, "/admin/policy/edit?lang=en")
			form = policyEditorForm(t, page, csrf, policyEditorCandidate)
			response, _ = env.post(env.client, "/admin/policy/activate", form)
			require.Equal(t, http.StatusSeeOther, response.StatusCode)

			snapshot = env.state.PolicySnapshot()
			assert.Equal(t, policyEditorCandidate, snapshot.Text)
			assert.Greater(t, snapshot.Revision, loaded.Revision)
			assert.Positive(t, env.changeCount())
		})
	}
}

func TestPolicyEditorRejectsUnsafeDrafts(t *testing.T) {
	env, _ := policyEditorTestEnv(t, types.PolicyModeDB)
	csrf := env.signedIn()
	source, loaded, err := env.state.PolicyEditorState()
	require.NoError(t, err)

	for _, text := range []string{
		`{"acls":[{"action":"drop"}]}`, `{"acls":[],"acls":[]}`,
		`{"unknown":{"keep":"<script>alert(1)</script>"}}`,
		`{"groups":{"group:ops":["one@"],"group:ops":[]}}`,
	} {
		_, page := env.get(env.client, "/admin/policy/edit?lang=en")
		form := policyEditorForm(t, page, csrf, text)
		response, page := env.post(env.client, "/admin/policy/validate", form)
		assert.Equal(t, http.StatusUnprocessableEntity, response.StatusCode)
		assert.NotContains(t, page, "2. Save validated draft")
		assert.NotContains(t, page, "<script>")

		if strings.Contains(text, "unknown") {
			assert.Contains(t, page, "unknown")
			assert.Contains(t, page, "&lt;script&gt;")
		}

		current, snapshot, readErr := env.state.PolicyEditorState()
		require.NoError(t, readErr)
		assert.Equal(t, source, current)
		assert.Equal(t, loaded, snapshot)
	}

	for _, action := range []string{"validate", "save", "activate"} {
		resp, _ := env.post(env.client, "/admin/policy/"+action, url.Values{"policy_text": {"{}"}})
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, action)
	}

	assert.Zero(t, env.changeCount())
}

func TestPolicyEditorStaleAndChangedDraft(t *testing.T) {
	env, _ := policyEditorTestEnv(t, types.PolicyModeDB)
	csrf := env.signedIn()
	_, page := env.get(env.client, "/admin/policy/edit?lang=en")
	form := policyEditorForm(t, page, csrf, policyEditorCandidate)
	response, page := env.post(env.client, "/admin/policy/validate", form)
	require.Equal(t, http.StatusOK, response.StatusCode)
	validated := policyEditorForm(t, page, csrf, policyEditorCandidate)
	validated.Set("policy_text", "{}")
	response, _ = env.post(env.client, "/admin/policy/save", validated)
	assert.Equal(t, http.StatusConflict, response.StatusCode)
	validated.Set("policy_text", policyEditorCandidate)

	_, err := env.state.SetPolicyInDB("{\n// external write\n}")
	require.NoError(t, err)

	response, page = env.post(env.client, "/admin/policy/save", validated)
	assert.Equal(t, http.StatusConflict, response.StatusCode)
	assert.Contains(t, page, "source or loaded policy changed")
	// An unrelated API activation invalidates an already reviewed loaded revision.
	_, page = env.get(env.client, "/admin/policy/edit?lang=en")
	old := policyEditorForm(t, page, csrf, policyEditorCandidate)
	_, err = env.state.SetPolicy([]byte("{}"))
	require.NoError(t, err)

	response, _ = env.post(env.client, "/admin/policy/validate", old)
	assert.Equal(t, http.StatusConflict, response.StatusCode)
	response, _ = env.post(env.client, "/admin/policy/activate", old)
	assert.Equal(t, http.StatusConflict, response.StatusCode)
}

func TestPolicyEditorSaveFailureReportsUnchanged(t *testing.T) {
	env, _ := policyEditorTestEnv(t, types.PolicyModeDB)
	csrf := env.signedIn()
	source, loaded, err := env.state.PolicyEditorState()
	require.NoError(t, err)

	_, page := env.get(env.client, "/admin/policy/edit?lang=en")
	response, page := env.post(env.client, "/admin/policy/validate", policyEditorForm(t, page, csrf, policyEditorCandidate))
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.NoError(t, env.state.DB().DB.Callback().Create().Before("gorm:create").Register("editor_web_fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "policies" {
			_ = tx.AddError(errPolicyEditorWebWrite)
		}
	}))
	t.Cleanup(func() { _ = env.state.DB().DB.Callback().Create().Remove("editor_web_fail") })
	response, page = env.post(env.client, "/admin/policy/save", policyEditorForm(t, page, csrf, policyEditorCandidate))
	assert.Equal(t, http.StatusInternalServerError, response.StatusCode)
	assert.Contains(t, page, "previous source and active policy were retained")

	current, snapshot, err := env.state.PolicyEditorState()
	require.NoError(t, err)
	assert.Equal(t, source, current)
	assert.Equal(t, loaded, snapshot)
}

func TestPolicyEditorValidCommentEscaping(t *testing.T) {
	env, _ := policyEditorTestEnv(t, types.PolicyModeDB)
	csrf := env.signedIn()
	_, page := env.get(env.client, "/admin/policy/edit?lang=en")
	candidate := "{\n// </textarea><script>alert(1)</script>\n\"acls\":[]\n}"
	response, page := env.post(env.client, "/admin/policy/validate", policyEditorForm(t, page, csrf, candidate))
	require.Equal(t, http.StatusOK, response.StatusCode)
	assert.NotContains(t, page, "<script>")
	assert.Contains(t, page, "&lt;/textarea&gt;")
	response, _ = env.post(env.client, "/admin/policy/save", policyEditorForm(t, page, csrf, candidate))
	require.Equal(t, http.StatusSeeOther, response.StatusCode)

	source, _, err := env.state.PolicyEditorState()
	require.NoError(t, err)
	assert.Equal(t, candidate, source.Text)
}

func TestPolicyEditorExpiredValidationAndLargeDraft(t *testing.T) {
	env, _ := policyEditorTestEnv(t, types.PolicyModeDB)
	csrf := env.signedIn()
	_, page := env.get(env.client, "/admin/policy/edit?lang=en")
	candidate := strings.Repeat("// large retained comment\n", 1000) + "{}"
	response, page := env.post(env.client, "/admin/policy/validate", policyEditorForm(t, page, csrf, candidate))
	require.Equal(t, http.StatusOK, response.StatusCode, "policy forms must support more than the ordinary 16 KiB admin form limit")
	form := policyEditorForm(t, page, csrf, candidate)
	parsed, err := url.Parse(env.server.URL)
	require.NoError(t, err)

	parsed.Path = "/admin/"
	foundSession := false

	for _, cookie := range env.client.Jar.Cookies(parsed) {
		if cookie.Name == sessionCookie {
			foundSession = true
			sess, ok := env.handler.sessions.Get(hashToken(cookie.Value))
			require.True(t, ok)
			sess.mu.Lock()
			require.NotNil(t, sess.policyDraft)
			sess.policyDraft.ValidatedAt = time.Now().Add(-16 * time.Minute)
			sess.mu.Unlock()
		}
	}

	require.True(t, foundSession, "expiry injection must target the authenticated /admin session cookie")

	response, _ = env.post(env.client, "/admin/policy/save", form)
	assert.Equal(t, http.StatusConflict, response.StatusCode)

	_, page = env.get(env.client, "/admin/policy/edit?lang=en")
	response, page = env.post(env.client, "/admin/policy/validate", policyEditorForm(t, page, csrf, candidate))
	require.Equal(t, http.StatusOK, response.StatusCode)
	form = policyEditorForm(t, page, csrf, candidate)
	form.Set("validation_token", "foreign-or-forged")
	response, _ = env.post(env.client, "/admin/policy/save", form)
	assert.Equal(t, http.StatusConflict, response.StatusCode)

	source, _, err := env.state.PolicyEditorState()
	require.NoError(t, err)
	assert.Equal(t, policyEditorOriginal, source.Text)
}
