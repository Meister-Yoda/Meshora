package webui

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
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
	return h.activeOIDC.Issuer != "" && h.activeOIDC.ClientID != "" && (len(h.adminOIDCGroups) > 0 || len(h.adminOIDCRoles) > 0)
}

// oidcLogin starts a separate admin authorization flow. Tailnet access never
// grants admin access, and local admin accounts are never linked by email/name.
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

	var claims struct {
		Groups   []string `json:"groups"`
		Username string   `json:"preferred_username"`
	}
	if idToken.Claims(&claims) != nil {
		h.oidcRejected(w, r)
		return
	}
	// If role restrictions are configured, they are mandatory, including when
	// a legacy group restriction also matches. Configured group and role filters
	// are ANDed; each individual list matches any one exact value.
	allowedGroup := len(h.adminOIDCGroups) == 0
	for _, group := range claims.Groups {
		if group != "" && slices.Contains(h.adminOIDCGroups, group) {
			allowedGroup = true
		}
	}

	allowedRole := len(h.adminOIDCRoles) == 0
	if !allowedRole {
		var rawClaims map[string]json.RawMessage
		if idToken.Claims(&rawClaims) != nil {
			h.oidcRejected(w, r)
			return
		}

		var roles []string
		if json.Unmarshal(rawClaims[cmp.Or(h.adminOIDCRolesClaim, "roles")], &roles) != nil {
			h.renderLogin(w, r, http.StatusForbidden, "", "Your Authentik account does not have an authorized admin role.")
			return
		}

		for _, role := range roles {
			if role != "" && slices.Contains(h.adminOIDCRoles, role) {
				allowedRole = true
			}
		}
	}

	if !allowedGroup || !allowedRole {
		h.renderLogin(w, r, http.StatusForbidden, "", "Your Authentik account does not satisfy the configured admin groups and roles.")
		return
	}

	username := claims.Username
	if username == "" {
		username = idToken.Subject
	}

	sessionToken := randomToken()
	h.sessions.Add(hashToken(sessionToken), &session{
		oidcSubject: idToken.Subject, username: username, csrf: randomToken(), expires: idToken.Expiry,
	})
	h.setCookie(w, r, loginCSRFCookie, "", -1)
	h.setCookie(w, r, sessionCookie, sessionToken, 0)
	http.Redirect(w, r, h.publicPath+"/", http.StatusSeeOther)
}

func (h *Handler) oidcRejected(w http.ResponseWriter, r *http.Request) {
	h.renderLogin(w, r, http.StatusUnauthorized, "", "Authentik sign-in could not be verified. Please start again.")
}
