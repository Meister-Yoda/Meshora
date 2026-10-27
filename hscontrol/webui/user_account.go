package webui

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/juanfont/headscale/hscontrol/db"
	"github.com/juanfont/headscale/hscontrol/templates"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/rs/zerolog/log"
)

func (h *Handler) accountPage(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)

	user, err := h.state.GetUserByID(sess.userID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	account, err := h.state.UserAccount(sess.userID)
	if err != nil {
		http.Error(w, "Konto nicht verfügbar", http.StatusInternalServerError)
		return
	}

	var nodes []templates.AdminNodeRow

	for _, node := range h.state.ListNodesByUser(sess.userID).All() {
		if !node.IsTagged() {
			nodes = append(nodes, nodeRow(node))
		}
	}

	writeHTML(w, 200, templates.UserAccountPage(h.pageInfo(r), userRow(user.View(), len(nodes)), nodes, account.IsAdmin).Render())
}

func (h *Handler) accountPasswordPage(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	writeHTML(w, 200, templates.UserPasswordPage(h.pageInfo(r), sess.mustChangePassword, sess.oidcSubject != "" || sess.passkey).Render())
}

func (h *Handler) saveAccountPassword(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	if r.PostForm.Get("password") != r.PostForm.Get("password_confirm") {
		h.redirect(w, r, "/account/password", failure("Die Passwörter stimmen nicht überein."))
		return
	}

	err := h.state.ChangeUserPassword(sess.userID, sess.accountRevision, r.PostForm.Get("current_password"), r.PostForm.Get("password"), sess.oidcSubject != "" || sess.passkey)
	if err != nil {
		h.redirect(w, r, "/account/password", failure("Passwort nicht geändert: %v", err))
		return
	}

	h.logout(w, r)
}

func (h *Handler) saveAccountProfile(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)

	_, changes, err := h.state.UpdateUser(sess.userID, func(u *types.User) error { u.DisplayName = formValue(r, "display_name"); return nil })
	if err != nil {
		h.redirect(w, r, "/account", failure("Profil nicht gespeichert: %v", err))
		return
	}

	h.change(changes)
	h.redirect(w, r, "/account", success("Profil gespeichert."))
}

func (h *Handler) accountDevice(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	node, ok := h.state.GetNodeByID(types.NodeID(id))
	if !ok || node.IsTagged() || node.TypedUserID() != sessionFrom(r).userID {
		http.NotFound(w, r)
		return
	}

	writeHTML(w, 200, templates.UserDevicePage(h.pageInfo(r), nodeRow(node)).Render())
}

func (h *Handler) setAccountAdmin(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	err := h.state.SetUserAdmin(types.UserID(id), r.PostForm.Get("is_admin") == "on")

	target := fmt.Sprintf("/users/%d", id)
	if err != nil {
		h.redirect(w, r, target, failure("Admin-Zugang nicht geändert: %v", err))
		return
	}

	h.redirect(w, r, target, success("Admin-Zugang gespeichert. Bestehende Sitzungen dieses Benutzers sind beendet."))
}

func (h *Handler) resetAccountPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if types.UserID(id) == sessionFrom(r).userID {
		h.redirect(w, r, "/account/password", failure("Das eigene Passwort bitte hier ändern."))
		return
	}

	password, err := h.state.ResetUserPassword(types.UserID(id))

	target := fmt.Sprintf("/users/%d", id)
	if err != nil {
		h.redirect(w, r, target, failure("Startpasswort nicht erstellt: %v", err))
		return
	}

	h.redirect(w, r, target, &templates.AdminFlash{Message: "Startpasswort nur jetzt kopieren und sicher weitergeben. Beim ersten Login muss es geändert werden.", Secret: password})
}

func (h *Handler) saveUserProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	email, err := db.NormalizeAccountEmail(r.PostForm.Get("email"))

	target := fmt.Sprintf("/users/%d", id)
	if err == nil {
		_, changes, saveErr := h.state.UpdateUser(types.UserID(id), func(u *types.User) error { u.Email = email; u.DisplayName = formValue(r, "display_name"); return nil })

		err = saveErr
		if err == nil {
			h.change(changes)
		}
	}

	if err != nil {
		h.redirect(w, r, target, failure("Profil nicht gespeichert: %v", err))
		return
	}

	h.redirect(w, r, target, success("Profil gespeichert. Die UID bleibt unverändert."))
}

func (h *Handler) mergeAccountPage(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	source, err := h.state.GetUserByID(types.UserID(id))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	users, err := h.state.ListAllUsers()
	if err != nil {
		http.Error(w, "Benutzer nicht verfügbar", http.StatusInternalServerError)
		return
	}

	var choices []templates.AdminChoice

	for _, user := range users {
		if user.ID != source.ID {
			choices = append(choices, templates.AdminChoice{Value: user.StringID(), Label: fmt.Sprintf("UID %d · %s", user.ID, user.Username())})
		}
	}

	writeHTML(w, 200, templates.UserMergePage(h.pageInfo(r), source.StringID(), source.Username(), choices).Render())
}

func (h *Handler) mergeAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	target, err := strconv.ParseUint(r.PostForm.Get("target"), 10, 64)
	if err != nil || r.PostForm.Get("confirm") != "on" {
		h.redirect(w, r, fmt.Sprintf("/users/%d/merge", id), failure("Zielkonto wählen und Zusammenführung ausdrücklich bestätigen."))
		return
	}

	changes, err := h.state.MergeUserAccounts(types.UserID(id), types.UserID(target), r.PostForm.Get("keep_password") == "on")
	h.change(changes)

	if err != nil {
		h.redirect(w, r, fmt.Sprintf("/users/%d/merge", id), failure("Konten nicht zusammengeführt: %v", err))
		return
	}

	h.redirect(w, r, fmt.Sprintf("/users/%d", target), success("Konten zusammengeführt. Die Ziel-UID bleibt erhalten; Geräte und Zugangsdaten wurden übertragen."))
}

func supportSessionFresh(sess *session, account *types.UserAccount) bool {
	fresh := !sess.authenticatedAt.IsZero() && time.Now().Before(sess.authenticatedAt.Add(5*time.Minute))
	return fresh && (sess.strongAuth || (sess.oidcSubject != "" && len(account.PasswordHash) == 0))
}

func (h *Handler) adminUserSecurity(r *http.Request, id types.UserID) (*templates.AdminUserSecurity, error) {
	policy, err := h.state.UserAuthPolicy(id)
	if err != nil {
		return nil, err
	}

	security, err := h.state.UserSecurity(id)
	if err != nil {
		return nil, err
	}

	keys, err := h.state.UserPasskeys(id)
	if err != nil {
		return nil, err
	}

	actor := sessionFrom(r)

	account, err := h.state.UserAccount(actor.userID)
	if err != nil {
		return nil, err
	}

	result := &templates.AdminUserSecurity{Policy: *policy, TOTPEnabled: len(security.EncryptedTOTPSecret) > 0, PasskeyCount: len(keys)}
	if !supportSessionFresh(actor, account) {
		if len(account.PasswordHash) == 0 {
			result.NeedOIDCLogin = true
		} else {
			result.NeedPassword = true

			ownSecurity, loadErr := h.state.UserSecurity(actor.userID)
			if loadErr != nil {
				return nil, loadErr
			}

			ownPolicy, loadErr := h.state.UserAuthPolicy(actor.userID)
			if loadErr != nil {
				return nil, loadErr
			}

			result.NeedCode = len(ownSecurity.EncryptedTOTPSecret) > 0 && !ownPolicy.TOTPBypassedAt(time.Now())
		}
	}

	return result, nil
}

func (h *Handler) saveUserAuthPolicy(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	actor := sessionFrom(r)
	account, err := h.state.UserAccount(actor.userID)
	target := fmt.Sprintf("/users/%d", id)

	if err != nil {
		http.Error(w, "Konto nicht verfügbar", http.StatusInternalServerError)
		return
	}

	if !supportSessionFresh(actor, account) {
		if len(account.PasswordHash) == 0 {
			h.redirect(w, r, target, failure("Bitte vor dieser Änderung erneut über OIDC anmelden."))
			return
		}

		err = h.proveSecurityChange(r)
		if err != nil {
			h.redirect(w, r, target, failure("Eigene Anmeldung nicht bestätigt: %v", err))
			return
		}
	}

	required := r.PostForm.Get("require_strong_auth") == "on"
	totpAction, passkeyAction := r.PostForm.Get("totp_pause"), r.PostForm.Get("passkey_pause")
	reason := strings.TrimSpace(r.PostForm.Get("reason"))

	err = h.state.SetUserAuthPolicy(actor.userID, types.UserID(id), actor.accountRevision, required, totpAction, passkeyAction, reason)
	if err != nil {
		h.redirect(w, r, target, failure("Anmeldesicherheit nicht geändert: %v", err))
		return
	}

	log.Info().Uint64("actor_uid", uint64(actor.userID)).Uint64("target_uid", id).Bool("require_strong_auth", required).Str("totp_pause", totpAction).Str("passkey_pause", passkeyAction).Str("reason", reason).Msg("user authentication policy changed; sessions revoked")

	if actor.userID == types.UserID(id) {
		h.logout(w, r)
		return
	}

	h.redirect(w, r, target, success("Anmeldesicherheit gespeichert. Alle WebUI-Sitzungen dieses Nutzers sind beendet."))
}
