package webui

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/juanfont/headscale/hscontrol/db"
	"github.com/juanfont/headscale/hscontrol/templates"
	"github.com/juanfont/headscale/hscontrol/types"
)

func (h *Handler) emailAccountProof(r *http.Request) error {
	actor := sessionFrom(r)

	account, err := h.state.UserAccount(actor.userID)
	if err != nil {
		return err
	}

	if account.Revision != actor.accountRevision {
		return db.ErrAccountChanged
	}

	if supportSessionFresh(actor, account) {
		return nil
	}

	return h.proveSecurityChange(r)
}

func (h *Handler) accountEmailState(r *http.Request, user *types.User, account *types.UserAccount) (templates.UserEmailState, error) {
	id := sessionFrom(r).userID

	policy, err := h.state.UserAuthPolicy(id)
	if err != nil {
		return templates.UserEmailState{}, err
	}

	h.settingsMu.Lock()
	available := validMailConfig(h.mailConfig)
	h.settingsMu.Unlock()

	result := templates.UserEmailState{
		Allowed:      !policy.EmailChangeBlocked,
		Managed:      user.ProviderIdentifier.Valid || (user.Provider != "" && user.Provider != "cli"),
		Available:    available,
		NeedPassword: !supportSessionFresh(sessionFrom(r), account),
	}

	if result.Allowed && result.Available && !result.Managed && result.NeedPassword {
		security, securityErr := h.state.UserSecurity(id)
		if securityErr != nil {
			return result, securityErr
		}

		result.NeedCode = len(security.EncryptedTOTPSecret) > 0 && !policy.TOTPBypassedAt(time.Now())
	}

	pending, err := h.state.PendingUserEmailChange(id)
	if err != nil {
		return result, err
	}

	if pending != nil && pending.AccountRevision == account.Revision {
		result.Pending = pending.Email
		result.ExpiresAt = pending.ExpiresAt
	}

	return result, nil
}

func (h *Handler) accountEmailPage(w http.ResponseWriter, r *http.Request) {
	id := sessionFrom(r).userID

	user, err := h.state.GetUserByID(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	account, err := h.state.UserAccount(id)
	if err != nil {
		http.Error(w, "Konto nicht verfügbar", http.StatusInternalServerError)
		return
	}

	email, err := h.accountEmailState(r, user, account)
	if err != nil {
		http.Error(w, "Konto nicht verfügbar", http.StatusInternalServerError)
		return
	}

	writeHTML(w, http.StatusOK, templates.UserEmailPage(h.pageInfo(r), userRow(user.View(), 0), email).Render())
}

func (h *Handler) requestAccountEmail(w http.ResponseWriter, r *http.Request) {
	actor := sessionFrom(r)

	key := fmt.Sprintf("email-request:%d", actor.userID)
	if h.blocked(key) {
		http.Error(w, "Bitte später erneut versuchen.", http.StatusTooManyRequests)
		return
	}

	h.recordFailure(key)

	proofErr := h.emailAccountProof(r)
	if proofErr != nil {
		h.redirectPublic(w, r, "/account/email", failure("Anmeldung bitte zuerst bestätigen."))
		return
	}

	h.settingsMu.Lock()
	cfg := h.mailConfig
	h.settingsMu.Unlock()

	if !validMailConfig(cfg) {
		h.redirectPublic(w, r, "/account/email", failure("Der Mailversand ist noch nicht eingerichtet."))
		return
	}

	email := strings.TrimSpace(r.PostForm.Get("email"))
	token := randomToken()
	digest := hashToken(token)

	err := h.state.BeginUserEmailChange(actor.userID, actor.accountRevision, email, digest)
	if err != nil {
		h.redirectPublic(w, r, "/account/email", failure("E-Mail-Adresse nicht geändert: %v", err))
		return
	}

	link := strings.TrimRight(h.serverURL, "/") + "/account/email/confirm#token=" + token
	body := "Du hast eine neue Anmeldeadresse für Meshora angefordert.\n\nMelde dich bei deinem Meshora-Konto an und oeffne danach diesen Link:\n" + link + "\n\nDer Link gilt 20 Minuten und kann einmal verwendet werden. Bis zur Bestätigung bleibt deine bisherige Anmeldung gültig.\nFalls du dies nicht angefordert hast, ignoriere diese Nachricht."

	err = h.emailSender(r.Context(), cfg, email, body)
	if err != nil {
		_ = h.state.CancelUserEmailChange(actor.userID, digest)
		h.redirectPublic(w, r, "/account/email", failure("%v", errAccountMail))

		return
	}

	h.redirectPublic(w, r, "/account/email", success("Bestätigungs-E-Mail versendet. Die bisherige Anmeldung bleibt bis zur Bestätigung gültig."))
}

func (h *Handler) accountEmailConfirmation(w http.ResponseWriter, r *http.Request) {
	writeHTML(w, http.StatusOK, templates.UserEmailConfirmation(h.pageInfo(r)).Render())
}

func (h *Handler) confirmAccountEmail(w http.ResponseWriter, r *http.Request) {
	token := r.PostForm.Get("token")
	if len(token) < 32 || len(token) > 128 {
		http.Error(w, "Bestätigung ungültig.", http.StatusBadRequest)
		return
	}

	actor := sessionFrom(r)

	changes, err := h.state.ConfirmUserEmailChange(actor.userID, actor.accountRevision, hashToken(token))
	if err != nil {
		h.redirectPublic(w, r, "/account/email", failure("Bestätigung ungültig, abgelaufen oder vom Administrator gesperrt."))
		return
	}

	h.change(changes)
	h.logout(w, r)
}

func (h *Handler) saveUserEmailPolicy(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	target := fmt.Sprintf("/users/%d", id)

	proofErr := h.emailAccountProof(r)
	if proofErr != nil {
		h.redirect(w, r, target, failure("Eigene Anmeldung bitte zuerst bestätigen."))
		return
	}

	actor := sessionFrom(r)

	err := h.state.SetUserEmailChangeBlocked(actor.userID, types.UserID(id), actor.accountRevision, r.PostForm.Get("allow_email_change") != "on")
	if err != nil {
		h.redirect(w, r, target, failure("E-Mail-Freigabe nicht gespeichert."))
		return
	}

	if actor.userID == types.UserID(id) {
		h.logout(w, r)
		return
	}

	h.redirect(w, r, target, success("E-Mail-Freigabe gespeichert. Bisherige Bestätigungslinks und Sitzungen dieses Nutzers sind beendet."))
}
