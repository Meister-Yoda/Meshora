package webui

import (
	"cmp"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image/png"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/juanfont/headscale/hscontrol/db"
	"github.com/juanfont/headscale/hscontrol/templates"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"
	"github.com/pquerna/otp/totp"
)

const (
	securityCookie = "meshora_auth_challenge"
	securityTTL    = 5 * time.Minute
)

var (
	errSecurityFlow           = errors.New("die Sicherheitsprüfung abgelaufen; bitte erneut beginnen")
	errExistingFactorRequired = errors.New("ein bereits eingerichteter Faktor muss zuerst bestätigt werden")
)

const msgExistingFactorRequired = "Bestätige die Anmeldung mit deinem eingerichteten Passkey oder Authenticator-Code. Vorher ist kein Kontozugriff möglich."

type securityFlow struct {
	Kind        string
	UserID      types.UserID
	Revision    uint64
	CSRF        string
	Secret      string
	URI         string
	Name        string
	Session     webauthn.SessionData
	OIDCSubject string
	OIDCExpiry  time.Time
}

func (h *Handler) securityKey() ([]byte, error) {
	material, err := os.ReadFile(h.runtimeConfig.NoisePrivateKeyPath)
	if err != nil || len(material) < 32 {
		return nil, db.ErrSecurityKey
	}

	digest := sha256.Sum256(append([]byte("Meshora TOTP encryption v1\x00"), material...))

	return digest[:], nil
}

func (h *Handler) securityPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", adminCSP+"; script-src 'self'; connect-src 'self'")

	sess := sessionFrom(r)

	account, err := h.state.UserAccount(sess.userID)
	if err != nil {
		http.Error(w, "Konto nicht verfügbar", http.StatusInternalServerError)
		return
	}

	security, err := h.state.UserSecurity(sess.userID)
	if err != nil {
		http.Error(w, "Sicherheitseinstellungen nicht verfügbar", http.StatusInternalServerError)
		return
	}

	records, err := h.state.UserPasskeys(sess.userID)
	if err != nil {
		http.Error(w, "Passkeys nicht verfügbar", http.StatusInternalServerError)
		return
	}

	policy, err := h.state.UserAuthPolicy(sess.userID)
	if err != nil {
		http.Error(w, "Sicherheitsrichtlinie nicht verfügbar", http.StatusInternalServerError)
		return
	}

	_, passkeyErr := h.webAuthn()
	writeHTML(w, 200, templates.UserSecurityPage(h.pageInfo(r), len(account.PasswordHash) > 0, len(security.EncryptedTOTPSecret) > 0, passkeyErr == nil, records, policy, sess.enrollmentOnly).Render())
}

func (h *Handler) validSecurityOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}

	configured, err := url.Parse(h.serverURL)

	return err == nil && origin == configured.Scheme+"://"+configured.Host
}

// proveSecurityChange requires the local password and an enabled second factor anew.
func (h *Handler) proveSecurityChange(r *http.Request) error {
	sess := sessionFrom(r)

	rateKey := "security:" + strconv.FormatUint(uint64(sess.userID), 10)
	if h.blocked(rateKey) {
		return errSecurityFlow
	}

	h.recordFailure(rateKey)

	if !h.validSecurityOrigin(r) {
		return errCSRFMismatch
	}

	err := h.requireVerifiedFactor(sess)
	if err != nil {
		return err
	}
	account, err := h.state.UserAccount(sess.userID)
	if err != nil {
		return err
	}

	if account.Revision != sess.accountRevision || len(account.PasswordHash) == 0 || account.MustChangePassword {
		return db.ErrAccountChanged
	}

	verified, err := h.state.AuthenticateUserAccount(cmp.Or(account.LoginEmail.String, account.LegacyLogin.String), r.PostForm.Get("current_password"))
	if err != nil || verified.UserID != uint64(sess.userID) {
		return db.ErrWebAdminInvalidCredentials
	}

	security, err := h.state.UserSecurity(sess.userID)
	if err != nil {
		return err
	}

	policy, err := h.state.UserAuthPolicy(sess.userID)
	if err != nil {
		return err
	}

	if len(security.EncryptedTOTPSecret) > 0 && !policy.TOTPBypassedAt(time.Now()) {
		err = h.verifySecondFactor(sess.userID, sess.accountRevision, r.PostForm.Get("code"))
		if err != nil {
			return err
		}
	}

	h.clearFailures(rateKey)

	return nil
}

func totpStep(secret, code string, now time.Time) (int64, error) {
	current := now.Unix() / 30
	for _, step := range []int64{current, current - 1, current + 1} {
		if step < 0 {
			continue
		}

		expected, err := hotp.GenerateCodeCustom(secret, uint64(step), hotp.ValidateOpts{Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err != nil {
			return -1, err
		}

		if tokensEqual(expected, code) {
			return step, nil
		}
	}

	return -1, db.ErrSecondFactor
}

func recoveryDigest(code string) string {
	normalized := strings.ToUpper(strings.NewReplacer("-", "", " ", "", "\n", "", "\r", "").Replace(strings.TrimSpace(code)))
	digest := sha256.Sum256([]byte(normalized))

	return hex.EncodeToString(digest[:])
}

func (h *Handler) verifySecondFactor(id types.UserID, revision uint64, code string) error {
	security, err := h.state.UserSecurity(id)
	if err != nil {
		return err
	}

	if len(security.EncryptedTOTPSecret) == 0 {
		return db.ErrSecondFactor
	}

	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return h.state.ConsumeSecondFactor(id, revision, -1, recoveryDigest(code))
	}

	key, err := h.securityKey()
	if err != nil {
		return err
	}

	secret, err := db.OpenTOTP(security, key)
	if err != nil {
		return err
	}

	step, err := totpStep(secret, code, time.Now())
	if err != nil {
		return err
	}

	return h.state.ConsumeSecondFactor(id, revision, step, "")
}

func sessionSecurityKey(r *http.Request, kind string) string {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}

	return kind + ":" + hashToken(cookie.Value)
}

func (h *Handler) beginTOTP(w http.ResponseWriter, r *http.Request) {
	err := h.proveSecurityChange(r)
	if err != nil {
		h.redirectPublic(w, r, "/account/security", failure("Bestätigung fehlgeschlagen: %v", err))
		return
	}

	sess := sessionFrom(r)

	security, err := h.state.UserSecurity(sess.userID)
	if err != nil {
		h.redirectPublic(w, r, "/account/security", failure("Sicherheitseinstellungen nicht verfügbar."))
		return
	}

	policy, err := h.state.UserAuthPolicy(sess.userID)
	if err != nil || (len(security.EncryptedTOTPSecret) > 0 && !policy.TOTPBypassedAt(time.Now())) {
		h.redirectPublic(w, r, "/account/security", failure("2FA ist bereits aktiv oder nicht verfügbar."))
		return
	}

	_, err = h.securityKey()
	if err != nil {
		h.redirectPublic(w, r, "/account/security", failure("2FA nicht verfügbar: %v", err))
		return
	}

	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Meshora", AccountName: sess.username, SecretSize: 32})
	if err != nil {
		http.Error(w, "2FA konnte nicht vorbereitet werden", http.StatusInternalServerError)
		return
	}

	h.securityFlows.Add(sessionSecurityKey(r, "totp"), securityFlow{Kind: "totp-setup", UserID: sess.userID, Revision: sess.accountRevision, Secret: key.Secret(), URI: key.URL()})
	h.showTOTPSetup(w, r, key.Secret())
}

func (h *Handler) showTOTPSetup(w http.ResponseWriter, r *http.Request, secret string) {
	writeHTML(w, 200, templates.UserTOTPSetup(h.pageInfo(r), secret).Render())
}

func (h *Handler) totpQR(w http.ResponseWriter, r *http.Request) {
	flow, ok := h.securityFlows.Get(sessionSecurityKey(r, "totp"))
	if !ok || flow.UserID != sessionFrom(r).userID || flow.Revision != sessionFrom(r).accountRevision {
		http.NotFound(w, r)
		return
	}

	key, err := otp.NewKeyFromURL(flow.URI)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	image, err := key.Image(240, 240)
	if err != nil {
		http.Error(w, "QR-Code nicht verfügbar", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	_ = png.Encode(w, image)
}

func newRecoveryCodes() ([]string, []string, error) {
	codes, hashes := make([]string, 10), make([]string, 10)
	for i := range codes {
		var raw [16]byte

		_, err := rand.Read(raw[:])
		if err != nil {
			return nil, nil, err
		}

		value := strings.ToUpper(hex.EncodeToString(raw[:]))
		codes[i] = value[:8] + "-" + value[8:16] + "-" + value[16:24] + "-" + value[24:]
		hashes[i] = recoveryDigest(codes[i])
	}

	return codes, hashes, nil
}

func (h *Handler) enableTOTP(w http.ResponseWriter, r *http.Request) {
	h.securityFlowsMu.Lock()
	defer h.securityFlowsMu.Unlock()

	flowKey := sessionSecurityKey(r, "totp")
	flow, ok := h.securityFlows.Get(flowKey)
	sess := sessionFrom(r)

	rateKey := "totp-setup:" + strconv.FormatUint(uint64(sess.userID), 10)
	if !ok || flow.UserID != sess.userID || flow.Revision != sess.accountRevision || h.blocked(rateKey) {
		h.redirectPublic(w, r, "/account/security", failure("%v", errSecurityFlow))
		return
	}

	h.recordFailure(rateKey)

	step, err := totpStep(flow.Secret, strings.TrimSpace(r.PostForm.Get("code")), time.Now())
	if err != nil {
		h.redirectPublic(w, r, "/account/security", failure("Code ungültig. Einrichtung bitte erneut beginnen."))
		return
	}

	key, err := h.securityKey()
	if err != nil {
		http.Error(w, "2FA-Schlüssel nicht verfügbar", http.StatusInternalServerError)
		return
	}

	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		http.Error(w, "Recovery-Codes nicht verfügbar", http.StatusInternalServerError)
		return
	}

	err = h.state.EnableUserTOTP(sess.userID, sess.accountRevision, flow.Secret, key, step, hashes)
	if err != nil {
		h.redirectPublic(w, r, "/account/security", failure("2FA nicht aktiviert: %v", err))
		return
	}

	h.securityFlows.Remove(flowKey)
	h.endCurrentSession(w, r)
	writeHTML(w, 200, templates.UserRecoveryCodes(h.publicPath, h.loginPath(), codes).Render())
}

func (h *Handler) disableTOTP(w http.ResponseWriter, r *http.Request) {
	err := h.proveSecurityChange(r)
	if err != nil {
		h.redirectPublic(w, r, "/account/security", failure("Bestätigung fehlgeschlagen: %v", err))
		return
	}

	sess := sessionFrom(r)

	err = h.state.DisableUserTOTP(sess.userID, sess.accountRevision)
	if err != nil {
		h.redirectPublic(w, r, "/account/security", failure("2FA nicht entfernt: %v", err))
		return
	}

	h.logout(w, r)
}

func (h *Handler) startSecondFactor(w http.ResponseWriter, r *http.Request, account *types.UserAccount, authentication *session) {
	h.endCurrentSession(w, r)

	token := randomToken()

	flow := securityFlow{Kind: "totp-login", UserID: types.UserID(account.UserID), Revision: account.Revision, CSRF: randomToken()}
	if authentication != nil {
		flow.OIDCSubject = authentication.oidcSubject
		flow.OIDCExpiry = authentication.expires
	}

	h.securityFlows.Add(hashToken(token), flow)
	h.setCookie(w, r, securityCookie, token, int(securityTTL.Seconds()))
	http.Redirect(w, r, h.loginPath()+"login/2fa", http.StatusSeeOther)
}

func (h *Handler) secondFactorPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", adminCSP+"; script-src 'self'; connect-src 'self'")
	flow, ok := h.loginSecurityFlow(r, "totp-login")
	if !ok {
		http.Redirect(w, r, h.loginPath()+"?local=1", http.StatusSeeOther)
		return
	}

	writeHTML(w, 200, h.secondFactorView(flow, ""))
}

func (h *Handler) loginSecurityFlow(r *http.Request, kind string) (securityFlow, bool) {
	cookie, err := r.Cookie(securityCookie)
	if err != nil {
		return securityFlow{}, false
	}

	flow, ok := h.securityFlows.Get(hashToken(cookie.Value))

	return flow, ok && flow.Kind == kind
}

func (h *Handler) finishSecondFactor(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", adminCSP+"; script-src 'self'; connect-src 'self'")
	h.securityFlowsMu.Lock()
	defer h.securityFlowsMu.Unlock()

	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if r.ParseForm() != nil || !h.validSecurityOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	flow, ok := h.loginSecurityFlow(r, "totp-login")
	if !ok || !tokensEqual(flow.CSRF, r.PostForm.Get(templates.AdminCSRFField)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	rateKey := "mfa:" + strconv.FormatUint(uint64(flow.UserID), 10)
	if h.blocked(rateKey) {
		http.Error(w, msgTooManyAttempts, http.StatusTooManyRequests)
		return
	}

	h.recordFailure(rateKey)

	err := h.verifySecondFactor(flow.UserID, flow.Revision, r.PostForm.Get("code"))
	if err != nil {
		writeHTML(w, 401, h.secondFactorView(flow, "Code ungültig oder bereits verwendet."))
		return
	}

	cookie, _ := r.Cookie(securityCookie)
	h.securityFlows.Remove(hashToken(cookie.Value))
	h.setCookie(w, r, securityCookie, "", -1)

	account, err := h.state.UserAccount(flow.UserID)
	if err != nil || account.Revision != flow.Revision {
		http.Error(w, "Anmeldung abgelaufen", http.StatusUnauthorized)
		return
	}

	user, err := h.state.GetUserByID(flow.UserID)
	if err != nil {
		http.Error(w, "Anmeldung abgelaufen", http.StatusUnauthorized)
		return
	}

	h.clearFailures(rateKey)
	h.issueSession(w, r, account, user, &session{strongAuth: true, oidcSubject: flow.OIDCSubject, expires: flow.OIDCExpiry})
}

func (h *Handler) endCurrentSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookie)
	if err == nil {
		h.sessions.Remove(hashToken(cookie.Value))
	}

	h.setCookie(w, r, sessionCookie, "", -1)
}

func (h *Handler) issueSession(w http.ResponseWriter, r *http.Request, account *types.UserAccount, user *types.User, authentication *session) {
	// Rotate any existing browser session after full authentication.
	h.endCurrentSession(w, r)

	token := randomToken()
	authentication.userID = types.UserID(user.ID)
	authentication.accountRevision = account.Revision
	authentication.isAdmin = account.IsAdmin
	authentication.mustChangePassword = account.MustChangePassword
	authentication.username = cmp.Or(user.Email, account.LegacyLogin.String, user.DisplayName, user.Name)
	authentication.csrf = randomToken()
	authentication.authenticatedAt = time.Now()

	err := h.applyAuthPolicy(authentication)
	if errors.Is(err, errExistingFactorRequired) {
		h.renderLogin(w, r, http.StatusUnauthorized, authentication.username, msgExistingFactorRequired)
		return
	}
	if err != nil {
		http.Error(w, "Sicherheitsrichtlinie nicht verfügbar", http.StatusServiceUnavailable)
		return
	}

	h.sessions.Add(hashToken(token), authentication)
	h.setCookie(w, r, loginCSRFCookie, "", -1)
	h.setCookie(w, r, sessionCookie, token, 0)
	h.clearFailures("ip:" + clientIP(r))

	target := h.rootPath("/account")
	if account.IsAdmin {
		target = h.publicPath + "/"
	}

	if authentication.enrollmentOnly {
		target = h.rootPath("/account/security")
	}

	if account.MustChangePassword {
		target = h.rootPath("/account/password")
	}

	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		securityJSON(w, map[string]string{"redirect": target})
		return
	}

	http.Redirect(w, r, target, http.StatusSeeOther)
}

func securityJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	err := json.NewEncoder(w).Encode(payload)
	if err != nil {
		http.Error(w, "Antwort konnte nicht erstellt werden", http.StatusInternalServerError)
	}
}

func enrollmentPath(path string) bool {
	switch path {
	case "/account/security", "/account/security/totp/begin", "/account/security/totp/qr", "/account/security/totp/enable", "/account/security/passkeys/begin", "/account/security/passkeys/finish", "/account/password", "/logout":
		return true
	default:
		return false
	}
}

// hasEnrolledFactor intentionally counts suspended passkeys too: only an explicit
// administrative 2FA recovery exception may permit password-only access.
func (h *Handler) hasEnrolledFactor(id types.UserID) (bool, error) {
	security, err := h.state.UserSecurity(id)
	if err != nil {
		return false, err
	}

	keys, err := h.state.UserPasskeys(id)
	if err != nil {
		return false, err
	}

	return len(security.EncryptedTOTPSecret) > 0 || len(keys) > 0, nil
}

// requireVerifiedFactor also rejects weak sessions issued before this guard was deployed.
func (h *Handler) requireVerifiedFactor(authentication *session) error {
	if authentication.strongAuth {
		return nil
	}
	policy, err := h.state.UserAuthPolicy(authentication.userID)
	if err != nil {
		return err
	}

	if policy.TOTPBypassedAt(time.Now()) {
		return nil
	}

	enrolled, err := h.hasEnrolledFactor(authentication.userID)
	if err != nil {
		return err
	}

	if enrolled {
		return errExistingFactorRequired
	}

	return nil
}

// applyAuthPolicy permits initial enrollment only when no factor exists yet.
func (h *Handler) applyAuthPolicy(authentication *session) error {
	policy, err := h.state.UserAuthPolicy(authentication.userID)
	if err != nil {
		return err
	}
	now := time.Now()
	bypass := policy.TOTPBypassedAt(now)

	authentication.enrollmentOnly = false
	if !authentication.strongAuth && !bypass {
		enrolled, loadErr := h.hasEnrolledFactor(authentication.userID)
		if loadErr != nil {
			return loadErr
		}

		if enrolled {
			return errExistingFactorRequired
		}

		authentication.enrollmentOnly = policy.RequireStrongAuth
		if authentication.enrollmentOnly {
			authentication.validUntil = now.Add(10 * time.Minute)
		}
	}

	if !authentication.strongAuth && bypass {
		authentication.validUntil = *policy.TOTPSuspendedUntil
	}
	return nil
}

// The challenge is not a session and grants no access to account routes.
func (h *Handler) secondFactorView(flow securityFlow, message string) string {
	security, securityErr := h.state.UserSecurity(flow.UserID)
	keys, keysErr := h.state.UserPasskeys(flow.UserID)
	policy, policyErr := h.state.UserAuthPolicy(flow.UserID)

	totp, passkey := false, false
	if securityErr == nil && keysErr == nil && policyErr == nil {
		totp = len(security.EncryptedTOTPSecret) > 0 && !policy.TOTPBypassedAt(time.Now())
		passkey = flow.OIDCSubject != "" && len(keys) > 0 && !policy.PasskeysSuspendedAt(time.Now())
	}

	return templates.UserSecondFactor(h.publicPath, h.loginPath(), flow.CSRF, message, totp, passkey).Render()
}
