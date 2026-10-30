package webui

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/juanfont/headscale/hscontrol/templates"
)

func (h *Handler) invitationsPage(w http.ResponseWriter, r *http.Request) {
	invites, err := h.state.UserInvitations()
	if err != nil {
		http.Error(w, "Einladungen nicht verfügbar", http.StatusInternalServerError)
		return
	}

	proof, err := h.adminUserSecurity(r, sessionFrom(r).userID)
	if err != nil {
		http.Error(w, "Anmeldung nicht verfügbar", http.StatusInternalServerError)
		return
	}

	writeHTML(w, http.StatusOK, templates.InvitationsPage(h.pageInfo(r), invites, proof).Render())
}

func (h *Handler) createInvitation(w http.ResponseWriter, r *http.Request) {
	if h.emailAccountProof(r) != nil {
		h.redirect(w, r, "/invitations", failure("Eigene Anmeldung bitte zuerst bestätigen."))
		return
	}

	sess := sessionFrom(r)

	key := "invite:" + strconv.FormatUint(uint64(sess.userID), 10)
	if h.blocked(key) {
		h.redirect(w, r, "/invitations", failure("%s", msgTooManyAttempts))
		return
	}

	h.recordFailure(key)
	h.settingsMu.Lock()
	cfg := h.mailConfig
	h.settingsMu.Unlock()

	if !validMailConfig(cfg) {
		h.redirect(w, r, "/invitations", failure("Mailversand zuerst einrichten."))
		return
	}

	mode := r.PostForm.Get("mode")
	if mode == "oidc" && !h.adminOIDCEnabled() {
		h.redirect(w, r, "/invitations", failure("OIDC zuerst einrichten."))
		return
	}

	token, id := randomToken(), randomToken()
	digest := hashToken(token)

	err := h.state.CreateUserInvitation(sess.userID, sess.accountRevision, r.PostForm.Get("email"), mode, id, digest)
	if err != nil {
		h.redirect(w, r, "/invitations", failure("Einladung nicht erstellt. Adresse und Anmeldeart prüfen; vorhandene Konten können nicht eingeladen werden."))
		return
	}

	invite, err := h.state.UserInvitation(digest)
	if err != nil {
		h.redirect(w, r, "/invitations", failure("Einladung nicht verfügbar."))
		return
	}

	link := strings.TrimRight(h.serverURL, "/") + "/invite#token=" + token + "&mode=" + mode

	err = h.emailSender(r.Context(), cfg, invite.Email, "Du wurdest zu Meshora eingeladen. Dieser Link ist 24 Stunden gültig und kann einmal verwendet werden:\n\n"+link+"\n\nFalls du keine Einladung erwartest, ignoriere diese Nachricht.")
	if err != nil {
		_ = h.state.CancelUserInvitation(id, digest)
		h.redirect(w, r, "/invitations", failure("Einladung konnte nicht versendet werden. Bitte Mailversand prüfen."))

		return
	}

	h.redirect(w, r, "/invitations", success("Einladung an den Mailserver übergeben."))
}

func (h *Handler) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	if h.emailAccountProof(r) != nil {
		h.redirect(w, r, "/invitations", failure("Eigene Anmeldung bitte zuerst bestätigen."))
		return
	}

	sess := sessionFrom(r)
	if h.state.RevokeUserInvitation(sess.userID, sess.accountRevision, r.PostForm.Get("invitation_id")) != nil {
		h.redirect(w, r, "/invitations", failure("Einladung konnte nicht widerrufen werden."))
		return
	}

	h.redirect(w, r, "/invitations", success("Einladung widerrufen."))
}

func (h *Handler) invitationPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", adminCSP+"; script-src 'self'")

	token := randomToken()
	h.setCookie(w, r, loginCSRFCookie, token, int(securityTTL.Seconds()))
	writeHTML(w, http.StatusOK, templates.InvitationPage(h.publicPath, h.rootPath("/invite"), token, "").Render())
}

func (h *Handler) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", adminCSP+"; script-src 'self'")

	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if r.ParseForm() != nil || !h.validSecurityOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	cookie, err := r.Cookie(loginCSRFCookie)
	if err != nil || !tokensEqual(cookie.Value, r.PostForm.Get("csrf_token")) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	key := "accept-invite:" + clientIP(r)
	if h.blocked(key) {
		http.Error(w, msgTooManyAttempts, http.StatusTooManyRequests)
		return
	}

	h.recordFailure(key)

	token := r.PostForm.Get("token")
	if len(token) < 32 || len(token) > 128 {
		http.Error(w, "Einladung ungültig oder abgelaufen", http.StatusBadRequest)
		return
	}

	digest := hashToken(token)

	invite, err := h.state.UserInvitation(digest)
	if err != nil {
		http.Error(w, "Einladung ungültig oder abgelaufen", http.StatusBadRequest)
		return
	}

	if invite.Mode == "oidc" {
		if !h.adminOIDCEnabled() {
			http.Error(w, "OIDC derzeit nicht verfügbar", http.StatusServiceUnavailable)
			return
		}

		h.startOIDC(w, r, false, digest)

		return
	}

	password := r.PostForm.Get("password")
	if password != r.PostForm.Get("password_confirm") {
		http.Error(w, "Passwörter stimmen nicht überein", http.StatusBadRequest)
		return
	}

	_, err = h.state.AcceptUserInvitation(digest, "local", password, nil)
	if err != nil {
		http.Error(w, "Einladung nicht angenommen. Link und Passwortanforderungen prüfen.", http.StatusBadRequest)
		return
	}

	h.setCookie(w, r, loginCSRFCookie, "", -1)
	http.Redirect(w, r, h.loginPath()+"?local=1", http.StatusSeeOther)
}
