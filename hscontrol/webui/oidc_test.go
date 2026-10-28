package webui

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Exercise actual discovery, PKCE code exchange, JWKS retrieval and signature
// validation rather than mocking the verifier or trusting unsigned claims.
func TestAdminOIDCFlow(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	for _, tc := range []struct {
		name   string
		status int
	}{
		{"success", http.StatusSeeOther},
		{"no_admin", http.StatusSeeOther},
		{"required_enrollment", http.StatusSeeOther},
		{"required_totp", http.StatusSeeOther},
		{"existing_totp", http.StatusSeeOther},
		{"existing_passkey", http.StatusUnauthorized},
		{"policy_revocation", http.StatusSeeOther},
		{"new_verified", http.StatusSeeOther},
		{"new_unverified_required", http.StatusForbidden},
		{"new_unverified_allowed", http.StatusSeeOther},
		{"unverified_local", http.StatusForbidden},
		{"missing_email", http.StatusForbidden},
		{"roles_only", http.StatusSeeOther},
		{"custom_roles_claim", http.StatusSeeOther},
		{"role_and_group", http.StatusSeeOther},
		{"group_cannot_bypass_role", http.StatusForbidden},
		{"missing_roles", http.StatusForbidden},
		{"malformed_roles", http.StatusForbidden},
		{"wrong_group", http.StatusForbidden},
		{"missing_groups", http.StatusForbidden},
		{"malformed_groups", http.StatusUnauthorized},
		{"expired", http.StatusUnauthorized},
		{"wrong_audience", http.StatusUnauthorized},
		{"wrong_issuer", http.StatusUnauthorized},
		{"wrong_nonce", http.StatusUnauthorized},
		{"empty_subject", http.StatusUnauthorized},
		{"invalid_signature", http.StatusUnauthorized},
		{"missing_id_token", http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			user, _, err := env.state.CreateUser(types.User{Name: "oidc-account", Email: "oidc-admin@example.test"})
			require.NoError(t, err)

			if tc.name != "no_admin" && !strings.HasPrefix(tc.name, "new_") && tc.name != "unverified_local" {
				require.NoError(t, env.state.SetUserAdmin(types.UserID(user.ID), true))
			}

			if tc.name == "required_enrollment" || tc.name == "required_totp" {
				actor, actorErr := env.state.AuthenticateUserAccount(testAdmin, testPassword)
				require.NoError(t, actorErr)
				require.NoError(t, env.state.SetUserAuthPolicy(types.UserID(actor.UserID), types.UserID(user.ID), actor.Revision, true, "keep", "keep", "OIDC Pflicht-Test"))

				if tc.name == "required_totp" {
					securityTestKey(t, env)
					account, loadErr := env.state.UserAccount(types.UserID(user.ID))
					require.NoError(t, loadErr)

					generated, generateErr := totp.Generate(totp.GenerateOpts{Issuer: "Test", AccountName: user.Email})
					require.NoError(t, generateErr)

					encryptionKey, keyErr := env.handler.securityKey()
					require.NoError(t, keyErr)
					require.NoError(t, env.state.EnableUserTOTP(types.UserID(user.ID), account.Revision, generated.Secret(), encryptionKey, -1, []string{recoveryDigest("OIDC-TEST-RECOVERY-CODE")}))
				}
			}

			if tc.name == "existing_totp" {
				securityTestKey(t, env)
				account, loadErr := env.state.UserAccount(types.UserID(user.ID))
				require.NoError(t, loadErr)

				generated, generateErr := totp.Generate(totp.GenerateOpts{Issuer: "Test", AccountName: user.Email})
				require.NoError(t, generateErr)

				encryptionKey, keyErr := env.handler.securityKey()
				require.NoError(t, keyErr)
				require.NoError(t, env.state.EnableUserTOTP(types.UserID(user.ID), account.Revision, generated.Secret(), encryptionKey, -1, []string{recoveryDigest("OIDC-TEST-RECOVERY-CODE")}))
			}

			if tc.name == "existing_passkey" {
				account, loadErr := env.state.UserAccount(types.UserID(user.ID))
				require.NoError(t, loadErr)
				require.NoError(t, env.state.AddUserPasskey(types.UserID(user.ID), account.Revision, types.UserPasskey{ID: "oidc-existing-key", Name: "Existing passkey", CredentialJSON: "{}"}))
			}
			var issuer string

			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				switch r.URL.Path {
				case "/.well-known/openid-configuration":
					assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
						"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token",
						"jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"},
					}))
				case "/keys":
					assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
						"kty": "RSA", "kid": "test", "alg": "RS256", "use": "sig",
						"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
						"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
					}}}))
				case "/token":
					assert.NoError(t, r.ParseForm())

					code := strings.Split(r.Form.Get("code"), ":")
					if !assert.Len(t, code, 2) {
						return
					}

					challenge := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
					assert.Equal(t, code[1], base64.RawURLEncoding.EncodeToString(challenge[:]))
					assert.Equal(t, env.server.URL+"/admin/oidc/callback", r.Form.Get("redirect_uri"))

					claims := map[string]any{
						"iss": issuer, "sub": "stable-admin-subject", "aud": "meshora", "exp": time.Now().Add(time.Hour).Unix(),
						"iat": time.Now().Unix(), "nonce": code[0], "groups": []string{"meshora-admins"},
						// An existing local username must not link the SSO session to that account.
						"preferred_username": testAdmin, "email": "oidc-admin@example.test", "email_verified": true,
					}

					if strings.HasPrefix(tc.name, "new_") {
						claims["email"] = "new-oidc@example.test"
					}

					if strings.Contains(tc.name, "unverified") {
						claims["email_verified"] = false
					}

					if tc.name == "missing_email" {
						delete(claims, "email")
					}
					claims["roles"] = []string{"meshora-admin"}
					if tc.name == "roles_only" {
						delete(claims, "groups")
					}

					if tc.name == "custom_roles_claim" {
						claims["app_roles"] = claims["roles"]
						delete(claims, "roles")
					}

					if tc.name == "group_cannot_bypass_role" {
						claims["roles"] = []string{"viewer"}
					}

					if tc.name == "missing_roles" {
						delete(claims, "roles")
					}

					if tc.name == "malformed_roles" {
						claims["roles"] = "meshora-admin"
					}

					switch tc.name {
					case "wrong_group":
						claims["groups"] = []string{"meshora-users"}
					case "missing_groups":
						delete(claims, "groups")
					case "malformed_groups":
						claims["groups"] = "meshora-admins"
					case "expired":
						claims["exp"] = time.Now().Add(-time.Hour).Unix()
					case "wrong_audience":
						claims["aud"] = "another-app"
					case "wrong_issuer":
						claims["iss"] = "https://untrusted.example.com"
					case "wrong_nonce":
						claims["nonce"] = "forged"
					case "empty_subject":
						claims["sub"] = ""
					}

					header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test"}`))
					payload, err := json.Marshal(claims)
					assert.NoError(t, err)

					unsigned := header + "." + base64.RawURLEncoding.EncodeToString(payload)
					digest := sha256.Sum256([]byte(unsigned))
					signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
					assert.NoError(t, err)

					if tc.name == "invalid_signature" {
						signature[0] ^= 1
					}

					response := map[string]any{"access_token": "test-access", "token_type": "Bearer", "id_token": unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)}
					if tc.name == "missing_id_token" {
						delete(response, "id_token")
					}

					assert.NoError(t, json.NewEncoder(w).Encode(response))
				default:
					http.NotFound(w, r)
				}
			}))
			defer provider.Close()

			issuer = provider.URL
			env.handler.serverURL = env.server.URL
			env.handler.activeOIDC = types.OIDCConfig{Issuer: issuer, ClientID: "meshora", ClientSecret: "test-secret", EmailVerifiedRequired: tc.name != "new_unverified_allowed" && tc.name != "unverified_local"}
			env.handler.adminOIDCGroups = []string{"meshora-admins"}

			switch tc.name {
			case "roles_only", "custom_roles_claim", "role_and_group", "group_cannot_bypass_role", "missing_roles", "malformed_roles":
				env.handler.adminOIDCRoles = []string{"meshora-admin"}
				if tc.name == "roles_only" || tc.name == "custom_roles_claim" {
					env.handler.adminOIDCGroups = nil
				}

				if tc.name == "custom_roles_claim" {
					env.handler.adminOIDCRolesClaim = "app_roles"
				}
			}

			_, page := env.get(env.client, "/?local=1")
			assert.Contains(t, page, "Sign in with Authentik")
			resp, _ := env.post(env.client, "/login/oidc", url.Values{"csrf_token": {csrfFrom(t, page)}})
			require.Equal(t, http.StatusSeeOther, resp.StatusCode)
			authorization, err := url.Parse(resp.Header.Get("Location"))
			require.NoError(t, err)

			query := authorization.Query()
			assert.Equal(t, "S256", query.Get("code_challenge_method"))
			assert.Contains(t, strings.Fields(query.Get("scope")), "openid")

			var binding *http.Cookie

			for _, cookie := range resp.Cookies() {
				if cookie.Name == adminOIDCCookie {
					binding = cookie
				}
			}

			require.NotNil(t, binding)
			assert.Equal(t, http.SameSiteLaxMode, binding.SameSite)
			assert.True(t, binding.HttpOnly)
			assert.Equal(t, "/admin/oidc/callback", binding.Path)

			callback := "/admin/oidc/callback?" + url.Values{
				"state": {query.Get("state")}, "code": {query.Get("nonce") + ":" + query.Get("code_challenge")},
			}.Encode()
			// The browser cookie is required; a stolen state alone cannot log in.
			other := newClient(t)
			resp, _ = env.get(other, callback)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

			resp, callbackPage := env.get(env.client, callback)
			if tc.name == "new_unverified_required" {
				assert.Contains(t, callbackPage, "email_verified fehlt oder ist false")
			}

			if tc.name == "missing_email" {
				assert.Contains(t, callbackPage, "keine gültige E-Mail-Adresse")
			}

			if tc.name == "unverified_local" {
				assert.Contains(t, callbackPage, "bestätigte E-Mail")
			}
			require.Equal(t, tc.status, resp.StatusCode)

			if tc.name == "required_totp" || tc.name == "existing_totp" {
				require.Equal(t, "/login/2fa", resp.Header.Get("Location"))

				blocked, _ := env.get(env.client, "/admin/")
				require.Equal(t, "/", blocked.Header.Get("Location"))

				_, challengePage := env.get(env.client, "/login/2fa")
				resp, _ = env.post(env.client, "/login/2fa", url.Values{"csrf_token": {csrfFrom(t, challengePage)}, "code": {"OIDC-TEST-RECOVERY-CODE"}})
				require.Equal(t, http.StatusSeeOther, resp.StatusCode)
			}
			if tc.status == http.StatusSeeOther {
				target := "/admin/"
				if tc.name == "no_admin" || strings.HasPrefix(tc.name, "new_") {
					target = "/account"
				}

				if tc.name == "required_enrollment" {
					target = "/account/security"
				}
				assert.Equal(t, target, resp.Header.Get("Location"))
				resp, page = env.get(env.client, target)
				assert.Equal(t, http.StatusOK, resp.StatusCode)

				if tc.name == "no_admin" || strings.HasPrefix(tc.name, "new_") {
					assert.Contains(t, page, "Mein Konto")
				} else if tc.name == "required_enrollment" {
					assert.Contains(t, page, "Sichere Anmeldung erforderlich")

					blocked, _ := env.get(env.client, "/admin/users")
					assert.Equal(t, "/account/security", blocked.Header.Get("Location"))
				} else {
					assert.Contains(t, page, "Overview")
				}

				u, _ := url.Parse(env.server.URL + "/admin/")

				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/admin/", nil)
				for _, c := range env.client.Jar.Cookies(u) {
					req.AddCookie(c)
				}

				sess, ok := env.handler.currentSession(req)
				require.True(t, ok)
				assert.Zero(t, sess.adminID)
				assert.Equal(t, "stable-admin-subject", sess.oidcSubject)
				// The usual session CSRF enforcement also applies to SSO admins.
				resp, _ = env.post(env.client, "/logout", url.Values{})
				assert.Equal(t, http.StatusForbidden, resp.StatusCode)

				if tc.name == "required_totp" || tc.name == "existing_totp" {
					assert.True(t, sess.strongAuth)
				}

				if tc.name == "required_enrollment" {
					assert.True(t, sess.enrollmentOnly)
				}

				if tc.name == "policy_revocation" {
					actor, actorErr := env.state.AuthenticateUserAccount(testAdmin, testPassword)
					require.NoError(t, actorErr)
					require.NoError(t, env.state.SetUserAuthPolicy(types.UserID(actor.UserID), types.UserID(user.ID), actor.Revision, true, "keep", "keep", "OIDC Sitzungsentzug"))
					_, alive := env.handler.currentSession(req)
					assert.False(t, alive, "enforcement must revoke existing OIDC sessions too")
				}
				sess.expires = time.Now().Add(-time.Second)
				_, ok = env.handler.currentSession(req)
				assert.False(t, ok)
			} else {
				resp, _ = env.get(env.client, "/admin/")
				assert.Equal(t, "/", resp.Header.Get("Location"))
			}
			// Even restoring the flow cookie cannot replay a consumed callback.
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, callback, nil)
			request.AddCookie(binding)

			recorder := httptest.NewRecorder()
			env.handler.oidcCallback(recorder, request)
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
}

func TestAdminOIDCDisabledAndCSRF(t *testing.T) {
	env := newTestEnv(t)
	_, page := env.get(env.client, "/?local=1")
	assert.NotContains(t, page, "Sign in with Authentik")

	resp, _ := env.post(env.client, "/login/oidc", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	env.handler.activeOIDC = types.OIDCConfig{Issuer: "https://auth.example.com", ClientID: "meshora"}
	env.handler.adminOIDCGroups = []string{"meshora-admins"}
	resp, _ = env.post(env.client, "/login/oidc", url.Values{"csrf_token": {"forged"}})
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	resp, _ = env.get(env.client, "/admin/oidc/callback?state=forged&code=forged")
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	// The browser-binding cookie must stay Secure behind a TLS-terminating proxy.
	env.handler.secureCookies = true
	recorder := httptest.NewRecorder()
	env.handler.setOIDCCookie(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), "test", 600)
	require.Len(t, recorder.Result().Cookies(), 1)
	assert.True(t, recorder.Result().Cookies()[0].Secure)
}

func TestAdminOIDCAutomaticFallback(t *testing.T) {
	env := newTestEnv(t)

	var issuer string

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys",
		})
	}))
	defer provider.Close()

	issuer = provider.URL
	env.handler.activeOIDC = types.OIDCConfig{Issuer: issuer, ClientID: "meshora"}
	env.handler.adminOIDCRoles = []string{"meshora-admin"}
	resp, _ := env.get(env.client, "/")
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	target, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "none", target.Query().Get("prompt"))
	assert.Equal(t, "S256", target.Query().Get("code_challenge_method"))
	callback := "/admin/oidc/callback?" + url.Values{"state": {target.Query().Get("state")}, "error": {"login_required"}}.Encode()
	resp, page := env.get(env.client, callback)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("Location"), "Failure must render local login without an automatic redirect loop")
	assert.Contains(t, page, `name="password"`)
	assert.Contains(t, page, "Use local admin login")
	resp, page = env.get(env.client, "/?local=1")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, page, `name="password"`)
	// Language switching must not automatically bounce away from the login page.
	resp, _ = env.get(env.client, "/?lang=de")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	// An unreachable provider must render a working local form as well.
	provider.Close()

	resp, page = env.get(env.client, "/")
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.Contains(t, page, `name="password"`)
	assert.Empty(t, resp.Header.Get("Location"))
}
