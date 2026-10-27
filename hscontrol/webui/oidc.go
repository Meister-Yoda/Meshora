package webui

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/juanfont/headscale/hscontrol/db"
	hsstate "github.com/juanfont/headscale/hscontrol/state"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"
)

const adminOIDCCookie = "meshora_admin_oidc"

type adminOIDCFlow struct {
	nonce    string
	verifier string
	browser  string
	provider *oidc.Provider
	config   oauth2.Config
}

func (h *Handler) adminOIDCEnabled() bool {
	return h.activeOIDC.Issuer != "" && h.activeOIDC.ClientID != ""
}

// oidcLogin starts the WebUI authorization flow. Administrator access requires
// an explicit account flag; email-based linking requires a verified mailbox.
func (h *Handler) oidcLogin(w http.ResponseWriter, r *http.Request) {
	if !h.adminOIDCEnabled() {
		http.NotFound(w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if r.ParseForm() != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	cookie, err := r.Cookie(loginCSRFCookie)
	if err != nil || !tokensEqual(cookie.Value, r.PostForm.Get("csrf_token")) {
		h.renderLogin(w, r, http.StatusForbidden, "", "Your login form expired. Please try again.")
		return
	}

	h.startOIDC(w, r, false)
}

// startOIDC uses prompt=none only for the automatic attempt. All failures render
// the local login directly, avoiding an authentication redirect loop.
func (h *Handler) startOIDC(w http.ResponseWriter, r *http.Request, automatic bool) {
	rateKey := "oidc:" + clientIP(r)
	if h.blocked(rateKey) {
		h.renderLogin(w, r, http.StatusTooManyRequests, "", msgTooManyAttempts)
		return
	}

	h.recordFailure(rateKey)

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	provider, err := oidc.NewProvider(ctx, h.activeOIDC.Issuer)
	if err != nil {
		h.renderLogin(w, r, http.StatusServiceUnavailable, "", "Authentik is unavailable. Use your local admin account or try again later.")
		return
	}

	scopes := slices.Clone(h.activeOIDC.Scope)
	for _, scope := range []string{oidc.ScopeOpenID, "profile", "email"} {
		if !slices.Contains(scopes, scope) {
			scopes = append(scopes, scope)
		}
	}

	config := oauth2.Config{
		ClientID: h.activeOIDC.ClientID, ClientSecret: h.activeOIDC.ClientSecret,
		Endpoint: provider.Endpoint(), RedirectURL: h.serverURL + "/admin/oidc/callback", Scopes: scopes,
	}
	state, browser, nonce, verifier := randomToken(), randomToken(), randomToken(), oauth2.GenerateVerifier()
	h.oidcFlows.Add(hashToken(state), adminOIDCFlow{
		nonce: nonce, verifier: verifier, browser: hashToken(browser), provider: provider, config: config,
	})
	h.setOIDCCookie(w, r, browser, 600)

	options := []oauth2.AuthCodeOption{oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)}
	if automatic {
		options = append(options, oauth2.SetAuthURLParam("prompt", "none"))
	}

	http.Redirect(w, r, config.AuthCodeURL(state, options...), http.StatusSeeOther)
}

// Only this short-lived browser-binding cookie is Lax so a top-level GET from
// Authentik can carry it. Dashboard session and login-CSRF cookies stay Strict.
func (h *Handler) setOIDCCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	//nolint:gosec // G124: Secure follows configured public HTTPS URL or direct TLS.
	http.SetCookie(w, &http.Cookie{
		Name: adminOIDCCookie, Value: value, Path: h.publicPath + "/oidc/callback",
		MaxAge: maxAge, HttpOnly: true, Secure: h.secureCookies || r.TLS != nil, SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if !h.adminOIDCEnabled() {
		http.NotFound(w, r)
		return
	}

	state, code := r.URL.Query().Get("state"), r.URL.Query().Get("code")

	cookie, err := r.Cookie(adminOIDCCookie)
	if state == "" || err != nil || cookie.Value == "" {
		h.renderLogin(w, r, http.StatusBadRequest, "", "Invalid or expired admin login. Start again from the login page.")
		return
	}
	// Consume matching flows atomically. A different browser cannot consume them.
	h.oidcFlowsMu.Lock()

	flow, ok := h.oidcFlows.Get(hashToken(state))
	if ok && tokensEqual(flow.browser, hashToken(cookie.Value)) {
		h.oidcFlows.Remove(hashToken(state))
	} else {
		ok = false
	}
	h.oidcFlowsMu.Unlock()

	if !ok {
		h.renderLogin(w, r, http.StatusBadRequest, "", "Invalid or expired admin login. Start again from the login page.")
		return
	}

	h.setOIDCCookie(w, r, "", -1)

	if code == "" || r.URL.Query().Get("error") != "" {
		h.renderLogin(w, r, http.StatusUnauthorized, "", "Authentik sign-in was cancelled or rejected.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	token, err := flow.config.Exchange(ctx, code, oauth2.VerifierOption(flow.verifier))
	if err != nil {
		h.oidcRejected(w, r)
		return
	}

	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		h.oidcRejected(w, r)
		return
	}

	idToken, err := flow.provider.Verifier(&oidc.Config{ClientID: h.activeOIDC.ClientID}).Verify(ctx, raw)
	if err != nil || !tokensEqual(idToken.Nonce, flow.nonce) || idToken.Subject == "" {
		h.oidcRejected(w, r)
		return
	}

	var adminClaims struct {
		Groups []string `json:"groups"`
	}
	if idToken.Claims(&adminClaims) != nil {
		h.oidcRejected(w, r)
		return
	}

	adminGroupAllowed := len(h.adminOIDCGroups) == 0
	for _, group := range adminClaims.Groups {
		if group != "" && slices.Contains(h.adminOIDCGroups, group) {
			adminGroupAllowed = true
		}
	}

	adminRoleAllowed := len(h.adminOIDCRoles) == 0
	if !adminRoleAllowed {
		var rawClaims map[string]json.RawMessage
		var roles []string
		if idToken.Claims(&rawClaims) == nil && json.Unmarshal(rawClaims[cmp.Or(h.adminOIDCRolesClaim, "roles")], &roles) == nil {
			for _, role := range roles {
				if role != "" && slices.Contains(h.adminOIDCRoles, role) {
					adminRoleAllowed = true
				}
			}
		}
	}

	var claims types.OIDCClaims
	if idToken.Claims(&claims) != nil {
		h.oidcRejected(w, r)
		return
	}

	claims.Iss, claims.Sub = idToken.Issuer, idToken.Subject

	user, err := h.state.ResolveOIDCUser(&claims, h.activeOIDC.EmailVerifiedRequired)
	if err != nil {
		log.Warn().Err(err).Bool("email_present", claims.Email != "").Bool("email_verified", bool(claims.EmailVerified)).Msg("WebUI OIDC account resolution failed")

		message := "OIDC-Konto konnte nicht geladen werden. Bitte Serverprotokoll prüfen."

		switch {
		case errors.Is(err, hsstate.ErrOIDCEmailUnverified):
			message = "Der OIDC-Anbieter hat deine E-Mail-Adresse nicht bestätigt (email_verified fehlt oder ist false). Bitte die E-Mail-Verifizierung beim Anbieter abschließen."
		case errors.Is(err, db.ErrAccountEmail):
			message = "Der OIDC-Anbieter liefert keine gültige E-Mail-Adresse. Bitte den email-Scope und das E-Mail-Mapping beim Anbieter prüfen."
		case errors.Is(err, hsstate.ErrOIDCEmailConflict), errors.Is(err, db.ErrAccountConflict):
			message = "Die OIDC-Identität konnte keinem eindeutigen Konto zugeordnet werden. Bestehende Konten können nur über eine bestätigte E-Mail verknüpft werden; bitte die Kontozuordnung administrativ prüfen."
		}

		h.renderLogin(w, r, http.StatusForbidden, "", message)
		return
	}

	account, err := h.state.UserAccount(types.UserID(user.ID))
	if err != nil {
		h.oidcRejected(w, r)
		return
	}

	if account.IsAdmin && (!adminGroupAllowed || !adminRoleAllowed) {
		h.renderLogin(w, r, http.StatusForbidden, "", "Your Authentik account does not satisfy the configured admin groups and roles.")
		return
	}

	policy, err := h.state.UserAuthPolicy(types.UserID(user.ID))
	if err != nil {
		h.oidcRejected(w, r)
		return
	}

	authentication := &session{oidcSubject: idToken.Subject, expires: idToken.Expiry}

	if policy.RequireStrongAuth && !policy.TOTPBypassedAt(time.Now()) {
		security, securityErr := h.state.UserSecurity(types.UserID(user.ID))
		if securityErr != nil {
			h.oidcRejected(w, r)
			return
		}

		if len(security.EncryptedTOTPSecret) > 0 {
			h.startSecondFactor(w, r, account, authentication)
			return
		}
	}

	h.issueSession(w, r, account, user, authentication)
}

func (h *Handler) oidcRejected(w http.ResponseWriter, r *http.Request) {
	h.renderLogin(w, r, http.StatusUnauthorized, "", "Authentik sign-in could not be verified. Please start again.")
}
