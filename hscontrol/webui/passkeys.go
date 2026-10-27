package webui

import (
	"bytes"
	"cmp"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/juanfont/headscale/hscontrol/assets"
	"github.com/juanfont/headscale/hscontrol/types"
)

var (
	errPasskey       = errors.New("der Passkey konnte nicht bestätigt werden; bitte erneut versuchen")
	errPasskeyOrigin = errors.New("passkeys benötigen eine gültige HTTPS-Serveradresse (oder localhost)")
)

type passkeyUser struct {
	User        *types.User
	Records     []types.UserPasskey
	Credentials []webauthn.Credential
}

func (u *passkeyUser) WebAuthnID() []byte {
	return binary.BigEndian.AppendUint64(nil, uint64(u.User.ID))
}

func (u *passkeyUser) WebAuthnName() string {
	return cmp.Or(u.User.Email, u.User.DisplayName, u.User.Username())
}

func (u *passkeyUser) WebAuthnDisplayName() string {
	return cmp.Or(u.User.DisplayName, u.WebAuthnName())
}
func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.Credentials }
func (h *Handler) passkeyUser(id types.UserID) (*passkeyUser, error) {
	user, err := h.state.GetUserByID(id)
	if err != nil {
		return nil, err
	}

	records, err := h.state.UserPasskeys(id)
	if err != nil {
		return nil, err
	}

	result := &passkeyUser{User: user, Records: records}
	for _, record := range records {
		var credential webauthn.Credential

		err = json.Unmarshal([]byte(record.CredentialJSON), &credential)
		if err != nil {
			return nil, err
		}

		result.Credentials = append(result.Credentials, credential)
	}

	return result, nil
}

func (h *Handler) webAuthn() (*webauthn.WebAuthn, error) {
	server, err := url.Parse(h.serverURL)
	if err != nil || server.Hostname() == "" {
		return nil, errPasskeyOrigin
	}

	ip := net.ParseIP(server.Hostname())

	local := server.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if server.Scheme != "https" && (server.Scheme != "http" || !local) {
		return nil, errPasskeyOrigin
	}

	return webauthn.New(&webauthn.Config{RPDisplayName: "Meshora", RPID: server.Hostname(), RPOrigins: []string{server.Scheme + "://" + server.Host}, AuthenticatorSelection: protocol.AuthenticatorSelection{ResidentKey: protocol.ResidentKeyRequirementRequired, UserVerification: protocol.VerificationRequired}, AttestationPreference: protocol.PreferNoAttestation})
}

func (h *Handler) beginPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	err := h.proveSecurityChange(r)
	if err != nil {
		http.Error(w, "Passwort oder zweiter Faktor ungültig", http.StatusForbidden)
		return
	}

	sess := sessionFrom(r)

	name := strings.TrimSpace(r.PostForm.Get("name"))
	if name == "" || len(name) > 80 {
		http.Error(w, "Bitte einen Namen mit höchstens 80 Bytes angeben", http.StatusBadRequest)
		return
	}

	instance, err := h.webAuthn()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	user, err := h.passkeyUser(sess.userID)
	if err != nil {
		http.Error(w, "Konto nicht verfügbar", http.StatusInternalServerError)
		return
	}

	options, data, err := instance.BeginRegistration(user, webauthn.WithExclusions(webauthn.Credentials(user.Credentials).CredentialDescriptors()))
	if err != nil {
		http.Error(w, errPasskey.Error(), http.StatusBadRequest)
		return
	}

	h.securityFlows.Add(sessionSecurityKey(r, "register"), securityFlow{Kind: "passkey-register", UserID: sess.userID, Revision: sess.accountRevision, Name: name, Session: *data})
	securityJSON(w, options)
}

func (h *Handler) finishPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	h.securityFlowsMu.Lock()
	defer h.securityFlowsMu.Unlock()

	key := sessionSecurityKey(r, "register")
	flow, ok := h.securityFlows.Get(key)
	h.securityFlows.Remove(key)

	sess := sessionFrom(r)
	if !ok || flow.Kind != "passkey-register" || flow.UserID != sess.userID || flow.Revision != sess.accountRevision || !h.validSecurityOrigin(r) {
		http.Error(w, errSecurityFlow.Error(), http.StatusForbidden)
		return
	}

	instance, err := h.webAuthn()
	if err != nil {
		http.Error(w, errPasskey.Error(), http.StatusBadRequest)
		return
	}

	user, err := h.passkeyUser(sess.userID)
	if err != nil {
		http.Error(w, errPasskey.Error(), http.StatusBadRequest)
		return
	}

	credential, err := instance.FinishRegistration(user, flow.Session, r)
	if err != nil {
		http.Error(w, errPasskey.Error(), http.StatusBadRequest)
		return
	}

	data, err := json.Marshal(credential)
	if err != nil {
		http.Error(w, errPasskey.Error(), http.StatusInternalServerError)
		return
	}

	err = h.state.AddUserPasskey(sess.userID, sess.accountRevision, types.UserPasskey{ID: base64.RawURLEncoding.EncodeToString(credential.ID), Name: flow.Name, CredentialJSON: string(data)})
	if err != nil {
		http.Error(w, "Passkey konnte nicht gespeichert werden", http.StatusConflict)
		return
	}

	h.endCurrentSession(w, r)
	securityJSON(w, map[string]string{"redirect": h.loginPath() + "?local=1"})
}

func (h *Handler) deletePasskey(w http.ResponseWriter, r *http.Request) {
	err := h.proveSecurityChange(r)
	if err != nil {
		h.redirect(w, r, "/account/security", failure("Bestätigung fehlgeschlagen: %v", err))
		return
	}

	sess := sessionFrom(r)

	err = h.state.DeleteUserPasskey(sess.userID, sess.accountRevision, r.PostForm.Get("passkey_id"))
	if err != nil {
		h.redirect(w, r, "/account/security", failure("Passkey nicht entfernt: %v", err))
		return
	}

	h.logout(w, r)
}

func (h *Handler) passkeyLoginCSRF(r *http.Request) bool {
	token := r.Header.Get("X-CSRF-Token")
	if token == "" || !h.validSecurityOrigin(r) {
		return false
	}

	if sess, ok := h.currentSession(r); ok && tokensEqual(sess.csrf, token) {
		return true
	}

	cookie, err := r.Cookie(loginCSRFCookie)

	return err == nil && cookie.Value != "" && tokensEqual(cookie.Value, token)
}

func (h *Handler) beginPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	if !h.passkeyLoginCSRF(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	rateKey := "passkey:" + clientIP(r)
	if h.blocked(rateKey) {
		http.Error(w, msgTooManyAttempts, http.StatusTooManyRequests)
		return
	}

	h.recordFailure(rateKey)

	instance, err := h.webAuthn()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	options, data, err := instance.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		http.Error(w, errPasskey.Error(), http.StatusBadRequest)
		return
	}

	token := randomToken()
	h.securityFlows.Add(hashToken(token), securityFlow{Kind: "passkey-login", Session: *data})
	h.setCookie(w, r, securityCookie, token, int(securityTTL.Seconds()))
	securityJSON(w, options)
}

func (h *Handler) finishPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	if !h.passkeyLoginCSRF(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)

	h.securityFlowsMu.Lock()
	defer h.securityFlowsMu.Unlock()

	flow, ok := h.loginSecurityFlow(r, "passkey-login")
	if !ok {
		http.Error(w, errSecurityFlow.Error(), http.StatusForbidden)
		return
	}

	cookie, _ := r.Cookie(securityCookie)
	h.securityFlows.Remove(hashToken(cookie.Value))
	h.setCookie(w, r, securityCookie, "", -1)

	instance, err := h.webAuthn()
	if err != nil {
		http.Error(w, errPasskey.Error(), http.StatusBadRequest)
		return
	}

	var account *types.UserAccount

	user, credential, err := instance.FinishPasskeyLogin(func(rawID, handle []byte) (webauthn.User, error) {
		if len(handle) != 8 {
			return nil, errPasskey
		}

		candidate, loadErr := h.passkeyUser(types.UserID(binary.BigEndian.Uint64(handle)))
		if loadErr != nil {
			return nil, errPasskey
		}

		matched := false

		for _, key := range candidate.Credentials {
			if bytes.Equal(rawID, key.ID) {
				matched = true
			}
		}

		if !matched {
			return nil, errPasskey
		}

		account, loadErr = h.state.UserAccount(types.UserID(candidate.User.ID))
		if loadErr != nil || len(account.PasswordHash) == 0 {
			return nil, errPasskey
		}

		policy, policyErr := h.state.UserAuthPolicy(types.UserID(candidate.User.ID))
		if policyErr != nil || policy.PasskeysSuspendedAt(time.Now()) {
			return nil, errPasskey
		}

		return candidate, nil
	}, flow.Session, r)
	if err != nil || credential.Authenticator.CloneWarning {
		http.Error(w, errPasskey.Error(), http.StatusUnauthorized)
		return
	}

	candidate, ok := user.(*passkeyUser)
	if !ok {
		http.Error(w, errPasskey.Error(), http.StatusUnauthorized)
		return
	}

	previous := ""

	id := base64.RawURLEncoding.EncodeToString(credential.ID)
	for _, record := range candidate.Records {
		if record.ID == id {
			previous = record.CredentialJSON
		}
	}

	updated, err := json.Marshal(credential)
	if err != nil {
		http.Error(w, errPasskey.Error(), http.StatusInternalServerError)
		return
	}

	err = h.state.UseUserPasskey(types.UserID(candidate.User.ID), account.Revision, id, previous, string(updated))
	if err != nil {
		http.Error(w, errPasskey.Error(), http.StatusUnauthorized)
		return
	}

	h.clearFailures("passkey:" + clientIP(r))
	h.issueSession(w, r, account, candidate.User, &session{passkey: true, strongAuth: true})
}

// securityScript serves the WebAuthn enhancement without third-party scripts.
func (h *Handler) securityScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = w.Write(assets.SecurityJS)
}
