package webui

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

func securityTestKey(t *testing.T, env *testEnv) {
	t.Helper()
	path := t.TempDir() + "/noise.key"
	require.NoError(t, os.WriteFile(path, []byte("test-private-server-key-with-32-bytes-minimum"), 0o600))
	env.handler.runtimeConfig.NoisePrivateKeyPath = path
}

func TestSecurityTOTPEnrollmentAndLogin(t *testing.T) {
	env := newTestEnv(t)
	securityTestKey(t, env)
	csrf := env.signedIn()
	another := newClient(t)
	response, _ := env.login(another, testAdmin, testPassword)
	require.Equal(t, 303, response.StatusCode)
	response, _ = env.post(env.client, "/account/security/totp/begin", url.Values{"current_password": {testPassword}})
	require.Equal(t, 403, response.StatusCode)
	response, page := env.post(env.client, "/account/security/totp/begin", url.Values{"csrf_token": {csrf}, "current_password": {testPassword}})
	require.Equal(t, 200, response.StatusCode)

	match := regexp.MustCompile(`<code[^>]*id="totp-secret"[^>]*>([^<]+)</code>`).FindStringSubmatch(page)
	require.Len(t, match, 2)
	secret := html.UnescapeString(match[1])
	response, _ = env.get(env.client, "/account/security/totp/qr")
	require.Equal(t, 200, response.StatusCode)
	require.Equal(t, "image/png", response.Header.Get("Content-Type"))
	response, _ = env.get(newClient(t), "/account/security/totp/qr")
	require.Equal(t, 303, response.StatusCode)

	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)

	response, page = env.post(env.client, "/account/security/totp/enable", url.Values{"csrf_token": {csrf}, "code": {code}})
	require.Equal(t, 200, response.StatusCode)

	recoveryMatch := regexp.MustCompile(`(?s)id="recovery-codes"[^>]*><code>(.*?)</code>`).FindStringSubmatch(page)
	require.Len(t, recoveryMatch, 2)
	codes := strings.Fields(html.UnescapeString(recoveryMatch[1]))
	require.Len(t, codes, 10)

	response, _ = env.get(another, "/admin/")
	require.Equal(t, "/", response.Header.Get("Location"))
	response, _ = env.get(env.client, "/admin/")
	require.Equal(t, "/", response.Header.Get("Location"))
	response, _ = env.login(env.client, testAdmin, testPassword)
	require.Equal(t, "/login/2fa", response.Header.Get("Location"))
	response, _ = env.get(env.client, "/admin/users")
	require.Equal(t, "/", response.Header.Get("Location"))

	_, page = env.get(env.client, "/login/2fa")
	token := csrfFrom(t, page)
	response, _ = env.post(env.client, "/login/2fa", url.Values{"code": {codes[0]}})
	require.Equal(t, 403, response.StatusCode)
	response, _ = env.post(env.client, "/login/2fa", url.Values{"csrf_token": {token}, "code": {"invalid"}})
	require.Equal(t, 401, response.StatusCode)
	response, _ = env.post(env.client, "/login/2fa", url.Values{"csrf_token": {token}, "code": {codes[0]}})
	require.Equal(t, "/admin/", response.Header.Get("Location"))
	response, _ = env.post(env.client, "/login/2fa", url.Values{"csrf_token": {token}, "code": {codes[1]}})
	require.Equal(t, 403, response.StatusCode)
	response, _ = env.get(env.client, "/admin/")
	require.Equal(t, 200, response.StatusCode)
	// The used recovery code cannot authorize another login.
	response, _ = env.login(another, testAdmin, testPassword)
	require.Equal(t, "/login/2fa", response.Header.Get("Location"))

	_, page = env.get(another, "/login/2fa")
	token = csrfFrom(t, page)
	response, _ = env.post(another, "/login/2fa", url.Values{"csrf_token": {token}, "code": {codes[0]}})
	require.Equal(t, 401, response.StatusCode)
	// A fresh TOTP step completes the login; the step used for enrollment cannot.
	response, _ = env.post(another, "/login/2fa", url.Values{"csrf_token": {token}, "code": {code}})
	require.Equal(t, 401, response.StatusCode)

	next, err := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	require.NoError(t, err)

	response, _ = env.post(another, "/login/2fa", url.Values{"csrf_token": {token}, "code": {next}})
	require.Equal(t, "/admin/", response.Header.Get("Location"))

	account, err := env.state.AuthenticateUserAccount(testAdmin, testPassword)
	require.NoError(t, err)
	security, err := env.state.UserSecurity(types.UserID(account.UserID))
	require.NoError(t, err)
	require.NotContains(t, string(security.EncryptedTOTPSecret), secret)
}

func TestPasskeyChallengeGuards(t *testing.T) {
	env := newTestEnv(t)
	env.server.URL = strings.Replace(env.server.URL, "127.0.0.1", "localhost", 1)
	env.handler.serverURL = env.server.URL
	_, page := env.get(env.client, "/?local=1")
	csrf := csrfFrom(t, page)
	request := func(client *http.Client, path, token, origin, body string) int {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, env.server.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("X-CSRF-Token", token)
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		require.NoError(t, err)

		defer response.Body.Close()

		_, _ = io.Copy(io.Discard, response.Body)

		return response.StatusCode
	}
	require.Equal(t, 403, request(env.client, "/login/passkeys/begin", "", env.server.URL, ""))
	require.Equal(t, 403, request(env.client, "/login/passkeys/begin", csrf, "https://evil.example", ""))
	require.Equal(t, 200, request(env.client, "/login/passkeys/begin", csrf, env.server.URL, ""))
	require.Equal(t, 403, request(newClient(t), "/login/passkeys/finish", csrf, env.server.URL, "{}"))
	require.Equal(t, 401, request(env.client, "/login/passkeys/finish", csrf, env.server.URL, "{}"))
	require.Equal(t, 403, request(env.client, "/login/passkeys/finish", csrf, env.server.URL, "{}"))
	response, _ := env.get(env.client, "/admin/")
	require.Equal(t, "/", response.Header.Get("Location"))

	env.handler.serverURL = "http://insecure.example"
	_, err := env.handler.webAuthn()
	require.ErrorIs(t, err, errPasskeyOrigin)
}

func TestMandatoryAuthenticationAndAdminRecovery(t *testing.T) {
	env := newTestEnv(t)
	securityTestKey(t, env)
	adminCSRF := env.signedIn()
	user, _, err := env.state.CreateUser(types.User{Name: "mandatory-user", Email: "mandatory@example.test"})
	require.NoError(t, err)

	id := types.UserID(user.ID)
	temporary, err := env.state.ResetUserPassword(id)
	require.NoError(t, err)
	account, err := env.state.AuthenticateUserAccount(user.Email, temporary)
	require.NoError(t, err)

	password := "personal test password for policy"
	require.NoError(t, env.state.ChangeUserPassword(id, account.Revision, temporary, password, false))

	first, second := newClient(t), newClient(t)
	for _, client := range []*http.Client{first, second} {
		response, _ := env.login(client, user.Email, password)
		require.Equal(t, "/account", response.Header.Get("Location"))
	}

	target := "/admin/users/" + user.StringID() + "/auth-policy"
	policyForm := func(totpAction, passkeyAction string) url.Values {
		return url.Values{"csrf_token": {adminCSRF}, "require_strong_auth": {"on"}, "totp_pause": {totpAction}, "passkey_pause": {passkeyAction}, "current_password": {testPassword}, "reason": {"Support-Test"}}
	}
	response, _ := env.post(env.client, target, url.Values{"require_strong_auth": {"on"}})
	require.Equal(t, 403, response.StatusCode)

	invalid := policyForm("keep", "keep")
	invalid.Set("current_password", "wrong password")
	response, _ = env.post(env.client, target, invalid)
	require.Equal(t, 303, response.StatusCode)

	policy, err := env.state.UserAuthPolicy(id)
	require.NoError(t, err)
	require.False(t, policy.RequireStrongAuth)

	response, _ = env.post(env.client, target, policyForm("keep", "keep"))
	require.Equal(t, 303, response.StatusCode)

	for _, client := range []*http.Client{first, second} {
		response, _ = env.get(client, "/account")
		require.Equal(t, "/", response.Header.Get("Location"))
	}

	response, _ = env.login(first, user.Email, password)
	require.Equal(t, "/account/security", response.Header.Get("Location"))

	_, page := env.get(first, "/account/security")
	csrf := csrfFrom(t, page)
	require.Contains(t, page, "Sichere Anmeldung erforderlich")

	for _, path := range []string{"/admin/", "/admin/users", "/account", "/admin/keys", "/account/devices/1"} {
		response, _ = env.get(first, path)
		require.Equal(t, "/account/security", response.Header.Get("Location"), path)
	}

	response, _ = env.post(first, "/account/profile", url.Values{"csrf_token": {csrf}, "display_name": {"bypassed"}})
	require.Equal(t, "/account/security", response.Header.Get("Location"))

	original, err := env.state.GetUserByID(id)
	require.NoError(t, err)
	require.NotEqual(t, "bypassed", original.DisplayName)

	response, page = env.post(first, "/account/security/totp/begin", url.Values{"csrf_token": {csrf}, "current_password": {password}})
	require.Equal(t, 200, response.StatusCode)

	match := regexp.MustCompile(`<code[^>]*id="totp-secret"[^>]*>([^<]+)</code>`).FindStringSubmatch(page)
	require.Len(t, match, 2)
	secret := html.UnescapeString(match[1])
	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)

	response, _ = env.post(first, "/account/security/totp/enable", url.Values{"csrf_token": {csrf}, "code": {code}})
	require.Equal(t, 200, response.StatusCode)
	response, _ = env.login(first, user.Email, password)
	require.Equal(t, "/login/2fa", response.Header.Get("Location"))
	// An administrator can grant a bounded exception without erasing the factor.
	response, _ = env.post(env.client, target, policyForm("15m", "15m"))
	require.Equal(t, 303, response.StatusCode)
	response, _ = env.login(first, user.Email, password)
	require.Equal(t, "/account", response.Header.Get("Location"))

	security, err := env.state.UserSecurity(id)
	require.NoError(t, err)
	require.NotEmpty(t, security.EncryptedTOTPSecret)

	policy, err = env.state.UserAuthPolicy(id)
	require.NoError(t, err)
	parsed, err := url.Parse(env.server.URL + "/account")
	require.NoError(t, err)

	var token string

	for _, cookie := range first.Jar.Cookies(parsed) {
		if cookie.Name == sessionCookie {
			token = cookie.Value
		}
	}

	live, ok := env.handler.sessions.Get(hashToken(token))
	require.True(t, ok)
	require.True(t, live.validUntil.Equal(*policy.TOTPSuspendedUntil), "exception sessions must expire with the exception")

	_, page = env.get(first, "/account/security")
	csrf = csrfFrom(t, page)
	require.Contains(t, page, "Befristete Admin-Ausnahme")

	response, _ = env.post(first, target, policyForm("keep", "keep"))
	require.Equal(t, 403, response.StatusCode, "ordinary users cannot change administrator policy")
	// Recovery reconnects a new authenticator with the password, then resumes 2FA immediately.
	response, page = env.post(first, "/account/security/totp/begin", url.Values{"csrf_token": {csrf}, "current_password": {password}})
	require.Equal(t, 200, response.StatusCode)

	match = regexp.MustCompile(`<code[^>]*id="totp-secret"[^>]*>([^<]+)</code>`).FindStringSubmatch(page)
	require.Len(t, match, 2)
	replacement := html.UnescapeString(match[1])
	require.NotEqual(t, secret, replacement)
	code, err = totp.GenerateCode(replacement, time.Now())
	require.NoError(t, err)

	response, _ = env.post(first, "/account/security/totp/enable", url.Values{"csrf_token": {csrf}, "code": {code}})
	require.Equal(t, 200, response.StatusCode)

	policy, err = env.state.UserAuthPolicy(id)
	require.NoError(t, err)
	require.Nil(t, policy.TOTPSuspendedUntil)
	require.True(t, policy.RequireStrongAuth)

	response, _ = env.login(first, user.Email, password)
	require.Equal(t, "/login/2fa", response.Header.Get("Location"))
	// Even otherwise-valid password-only sessions are rejected when their support deadline passes.
	response, _ = env.post(env.client, target, policyForm("15m", "keep"))
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	currentAccount, loadErr := env.state.UserAccount(id)
	require.NoError(t, loadErr)
	env.handler.sessions.Add(hashToken(token), &session{userID: id, accountRevision: currentAccount.Revision, enrollmentOnly: true, validUntil: time.Now().Add(time.Minute)})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/account", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	_, ok = env.handler.currentSession(req)
	require.True(t, ok, "otherwise-valid sessions must be accepted before their deadline")
	env.handler.sessions.Add(hashToken(token), &session{userID: id, accountRevision: currentAccount.Revision, enrollmentOnly: true, validUntil: time.Now().Add(-time.Second)})
	_, ok = env.handler.currentSession(req)
	require.False(t, ok)
}

func TestExistingPasskeyBlocksPasswordOnlyAccountAccess(t *testing.T) {
	for _, required := range []bool{false, true} {
		t.Run(fmt.Sprintf("mandatory_%t", required), func(t *testing.T) {
			env := newTestEnv(t)
			securityTestKey(t, env)
			user, _, err := env.state.CreateUser(types.User{Name: "existing-passkey-user", Email: "existing-passkey@example.test"})
			require.NoError(t, err)

			id := types.UserID(user.ID)
			temporary, err := env.state.ResetUserPassword(id)
			require.NoError(t, err)
			account, err := env.state.UserAccount(id)
			require.NoError(t, err)

			password := "known correct password for regression"
			require.NoError(t, env.state.ChangeUserPassword(id, account.Revision, temporary, password, false))

			if required {
				actor, actorErr := env.state.AuthenticateUserAccount(testAdmin, testPassword)
				require.NoError(t, actorErr)
				require.NoError(t, env.state.SetUserAuthPolicy(types.UserID(actor.UserID), id, actor.Revision, true, "keep", "keep", "Regression"))
			}

			account, err = env.state.UserAccount(id)
			require.NoError(t, err)
			// Public credential metadata is sufficient here; a real signed login is tested in Chromium.
			require.NoError(t, env.state.AddUserPasskey(id, account.Revision, types.UserPasskey{ID: "existing-key", Name: "Existing passkey", CredentialJSON: "{}"}))
			attacker := newClient(t)
			response, page := env.login(attacker, user.Email, password)
			require.Equal(t, http.StatusUnauthorized, response.StatusCode)
			require.Empty(t, response.Header.Get("Location"))
			require.Contains(t, page, "Bestätige die Anmeldung")
			require.Contains(t, page, `data-passkey-login`)
			require.NotContains(t, page, "2FA einrichten")
			require.NotContains(t, page, "Passkey hinzufügen")

			for _, cookie := range response.Cookies() {
				require.False(t, cookie.Name == sessionCookie && cookie.Value != "", "password must not issue a session")
			}

			loginCSRF := csrfFrom(t, page)

			for _, path := range []string{"/account/security", "/account/password", "/account", "/admin/users"} {
				response, _ = env.get(attacker, path)
				require.Equal(t, "/", response.Header.Get("Location"), path)
			}

			for _, path := range []string{"/account/security/totp/begin", "/account/security/totp/enable", "/account/security/passkeys/begin", "/account/security/passkeys/finish", "/account/security/passkeys/delete", "/account/password"} {
				response, _ = env.post(attacker, path, url.Values{"csrf_token": {loginCSRF}, "current_password": {password}, "password": {"attacker replacement password"}, "password_confirm": {"attacker replacement password"}, "name": {"attacker-key"}, "passkey_id": {"existing-key"}, "code": {"123456"}})
				require.Equal(t, "/", response.Header.Get("Location"), path)
			}

			security, err := env.state.UserSecurity(id)
			require.NoError(t, err)
			require.Empty(t, security.EncryptedTOTPSecret)

			keys, err := env.state.UserPasskeys(id)
			require.NoError(t, err)
			require.Len(t, keys, 1)
			require.Equal(t, "existing-key", keys[0].ID)

			_, err = env.state.AuthenticateUserAccount(user.Email, password)
			require.NoError(t, err)
			// Reject formerly issued weak enrollment sessions as well, even with the current account revision.
			account, err = env.state.UserAccount(id)
			require.NoError(t, err)

			token := randomToken()
			weak := &session{userID: id, accountRevision: account.Revision, enrollmentOnly: true, csrf: randomToken()}
			env.handler.sessions.Add(hashToken(token), weak)
			legacyRequest, err := http.NewRequestWithContext(t.Context(), http.MethodPost, env.server.URL+"/account/security/totp/begin", strings.NewReader(url.Values{"csrf_token": {weak.csrf}, "current_password": {password}}.Encode()))
			require.NoError(t, err)
			legacyRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			legacyRequest.AddCookie(&http.Cookie{Name: sessionCookie, Value: token, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
			legacyResponse, err := attacker.Do(legacyRequest)
			require.NoError(t, err)

			defer legacyResponse.Body.Close()

			_, err = io.Copy(io.Discard, legacyResponse.Body)
			require.NoError(t, err)
			require.Equal(t, http.StatusSeeOther, legacyResponse.StatusCode)
			require.Equal(t, "/", legacyResponse.Header.Get("Location"))
			_, alive := env.handler.sessions.Get(hashToken(token))
			require.False(t, alive)
			// Handler-level verification also fails closed if called independently of routing.
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/account/security/totp/begin", strings.NewReader(url.Values{"current_password": {password}}.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			require.NoError(t, request.ParseForm())
			request = request.WithContext(withSession(request.Context(), weak))
			require.ErrorIs(t, env.handler.proveSecurityChange(request), errExistingFactorRequired)
		})
	}
}

func TestOIDCPasskeyConfirmation(t *testing.T) {
	for _, scenario := range []string{"success", "other-account", "revision", "expired", "signature", "csrf", "suspended"} {
		t.Run(scenario, func(t *testing.T) {
			env := newTestEnv(t)
			env.handler.serverURL = "http://localhost:8080"
			env.handler.activeOIDC = types.OIDCConfig{Issuer: "https://issuer.example.test", ClientID: "meshora"}
			user, _, err := env.state.CreateUser(types.User{Name: "oidc-passkey", Email: "oidc-passkey@example.test"})
			require.NoError(t, err)

			uid := types.UserID(user.ID)
			account, err := env.state.UserAccount(uid)
			require.NoError(t, err)
			require.Empty(t, account.PasswordHash)

			boundID := uid

			if scenario == "other-account" {
				other, _, createErr := env.state.CreateUser(types.User{Name: "other-oidc", Email: "other-oidc@example.test"})
				require.NoError(t, createErr)

				boundID = types.UserID(other.ID)
			}

			private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			require.NoError(t, err)
			public, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: private.X.FillBytes(make([]byte, 32)), -3: private.Y.FillBytes(make([]byte, 32))})
			require.NoError(t, err)

			id := []byte("oidc-test-passkey")
			credential := webauthn.Credential{ID: id, PublicKey: public, AttestationType: "none"}
			encoded, err := json.Marshal(credential)
			require.NoError(t, err)
			require.NoError(t, env.state.AddUserPasskey(uid, account.Revision, types.UserPasskey{ID: base64.RawURLEncoding.EncodeToString(id), Name: "Test", CredentialJSON: string(encoded)}))

			boundAccount, err := env.state.UserAccount(boundID)
			require.NoError(t, err)

			expiry := time.Now().Add(time.Hour)
			if scenario == "expired" {
				expiry = time.Now().Add(-time.Minute)
			}

			recorder := httptest.NewRecorder()
			env.handler.startSecondFactor(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, env.server.URL+"/admin/oidc/callback", nil), boundAccount, &session{oidcSubject: "bound-subject", expires: expiry})

			client := newClient(t)
			target, err := url.Parse(env.server.URL)
			require.NoError(t, err)
			client.Jar.SetCookies(target, recorder.Result().Cookies())
			pageResponse, page := env.get(client, "/login/2fa")
			require.Equal(t, http.StatusOK, pageResponse.StatusCode)

			challengeRequest := httptest.NewRequestWithContext(t.Context(), http.MethodGet, env.server.URL+"/login/2fa", nil)
			for _, cookie := range client.Jar.Cookies(target) {
				challengeRequest.AddCookie(cookie)
			}

			flow, flowOK := env.handler.loginSecurityFlow(challengeRequest, "totp-login")
			require.True(t, flowOK)

			csrf := flow.CSRF

			if scenario == "success" {
				require.Contains(t, page, "Mit Passkey bestätigen")
			}

			request := func(path string, body string, token string) (int, []byte) {
				req, requestErr := http.NewRequestWithContext(t.Context(), http.MethodPost, env.server.URL+path, strings.NewReader(body))
				require.NoError(t, requestErr)
				req.Header.Set("X-CSRF-Token", token)
				req.Header.Set("Origin", env.handler.serverURL)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", "application/json")
				res, doErr := client.Do(req)
				require.NoError(t, doErr)

				data, readErr := io.ReadAll(res.Body)
				require.NoError(t, readErr)
				require.NoError(t, res.Body.Close())

				return res.StatusCode, data
			}
			response, options := request("/login/passkeys/begin", "", csrf)
			require.Equal(t, http.StatusOK, response)

			var challenge struct{ PublicKey struct{ Challenge string } }
			require.NoError(t, json.Unmarshal(options, &challenge))
			require.NotEmpty(t, challenge.PublicKey.Challenge)
			data, err := json.Marshal(map[string]any{"type": "webauthn.get", "challenge": challenge.PublicKey.Challenge, "origin": env.handler.serverURL})
			require.NoError(t, err)

			rp, _ := url.Parse(env.handler.serverURL)
			rpHash := sha256.Sum256([]byte(rp.Hostname()))
			authData := append(rpHash[:], byte(5))
			authData = binary.BigEndian.AppendUint32(authData, 1)
			clientHash := sha256.Sum256(data)
			signed := sha256.Sum256(append(append([]byte{}, authData...), clientHash[:]...))
			signature, err := ecdsa.SignASN1(rand.Reader, private, signed[:])
			require.NoError(t, err)

			if scenario == "signature" {
				signature[0] ^= 1
			}

			assertion, err := json.Marshal(map[string]any{
				"id": base64.RawURLEncoding.EncodeToString(id), "rawId": base64.RawURLEncoding.EncodeToString(id), "type": "public-key",
				"response": map[string]string{"authenticatorData": base64.RawURLEncoding.EncodeToString(authData), "clientDataJSON": base64.RawURLEncoding.EncodeToString(data), "signature": base64.RawURLEncoding.EncodeToString(signature), "userHandle": base64.RawURLEncoding.EncodeToString(binary.BigEndian.AppendUint64(nil, uint64(uid)))},
			})
			require.NoError(t, err)

			if scenario == "revision" {
				require.NoError(t, env.state.SetUserAdmin(boundID, true))
			}

			if scenario == "csrf" {
				csrf = "forged"
			}

			if scenario == "suspended" {
				actor, authErr := env.state.AuthenticateUserAccount(testAdmin, testPassword)
				require.NoError(t, authErr)
				require.NoError(t, env.state.SetUserAuthPolicy(types.UserID(actor.UserID), uid, actor.Revision, false, "keep", "15m", "test suspension"))
			}

			response, result := request("/login/passkeys/finish", string(assertion), csrf)
			if scenario == "success" {
				require.Equal(t, http.StatusOK, response, string(result))
				require.Contains(t, string(result), "/account")

				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, env.server.URL+"/account", nil)
				for _, cookie := range client.Jar.Cookies(target) {
					req.AddCookie(cookie)
				}

				sess, ok := env.handler.currentSession(req)
				require.True(t, ok)
				require.Equal(t, uid, sess.userID)
				require.Equal(t, "bound-subject", sess.oidcSubject)
				require.True(t, sess.strongAuth)
				require.True(t, sess.passkey)
				require.WithinDuration(t, expiry, sess.expires, time.Second)
			} else {
				require.True(t, response == http.StatusForbidden || response == http.StatusUnauthorized, string(result))

				blocked, _ := env.get(client, "/account")
				require.Equal(t, http.StatusSeeOther, blocked.StatusCode)
			}

			if scenario != "csrf" {
				response, _ = request("/login/passkeys/finish", string(assertion), csrf)
				require.Equal(t, http.StatusForbidden, response)
			}
		})
	}
}
