package webui

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/juanfont/headscale/hscontrol/db"
	"github.com/juanfont/headscale/hscontrol/state"
	"github.com/juanfont/headscale/hscontrol/templates"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/util"
	"github.com/rs/zerolog/log"
)

// Dashboard actions. Each one calls the same state functions as the matching
// v1 API handler (hscontrol/api/v1) and hands the resulting changes to
// h.change so connected nodes see them. Every POST ends in a 303 redirect
// with a flash message stored in the session (see session.setFlash).

// errAuthRejected is the verdict handed to a waiting client when an admin
// rejects its request, like the v1 authReject handler.
var errAuthRejected = errors.New("auth request rejected by an admin")

var (
	errTagMissingPrefix = errors.New("tags must start with \"tag:\"")
	errTagNotLowercase  = errors.New("tags must be lowercase")
	errNoTags           = errors.New("enter at least one tag; tagged nodes always keep one")
)

// keyExpiries are the lifetimes offered for new pre-auth keys.
var keyExpiries = []templates.AdminChoice{
	{Value: "1h", Label: "1 hour"},
	{Value: "24h", Label: "1 day", Selected: true},
	{Value: "168h", Label: "7 days"},
	{Value: "720h", Label: "30 days"},
	{Value: "2160h", Label: "90 days"},
}

func (h *Handler) redirect(w http.ResponseWriter, r *http.Request, path string, f *templates.AdminFlash) {
	if path == "/settings" {
		switch section := r.PostForm.Get("settings_section"); section {
		case "provider", "access", "appearance", "callbacks":
			path += "/" + section
		}
	}

	if f != nil {
		sessionFrom(r).setFlash(f)
	}

	//nolint:gosec // G710: path is always built by the handlers from IDs, never taken from the request
	http.Redirect(w, r, h.publicPath+path, http.StatusSeeOther)
}

func success(format string, args ...any) *templates.AdminFlash {
	return &templates.AdminFlash{Message: fmt.Sprintf(format, args...)}
}

func failure(format string, args ...any) *templates.AdminFlash {
	return &templates.AdminFlash{Error: true, Message: fmt.Sprintf(format, args...)}
}

func urlID(r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)

	return id, err == nil && id > 0
}

func formValue(r *http.Request, name string) string {
	return strings.TrimSpace(r.PostForm.Get(name))
}

// userChoices lists all users for a <select>.
func (h *Handler) userChoices() ([]templates.AdminChoice, error) {
	users, err := h.state.ListAllUsers()
	if err != nil {
		return nil, err
	}

	choices := make([]templates.AdminChoice, 0, len(users))
	for i := range users {
		u := users[i].View()
		label := u.Username()

		if d := u.DisplayName(); d != "" && d != label {
			label = d + " (" + label + ")"
		}

		choices = append(choices, templates.AdminChoice{
			Value: strconv.FormatUint(uint64(u.ID()), 10),
			Label: label,
		})
	}

	slices.SortFunc(choices, func(a, b templates.AdminChoice) int {
		return cmp.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label))
	})

	return choices, nil
}

func serverError(w http.ResponseWriter, msg string, err error) {
	log.Error().Err(err).Msg("web admin: " + msg)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

// Users

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	email, err := db.NormalizeAccountEmail(formValue(r, "email"))
	if err != nil {
		h.redirect(w, r, "/users", failure("Bitte eine gültige E-Mail-Adresse eingeben."))
		return
	}

	user, changes, err := h.state.CreateUser(types.User{Name: "user-" + hashToken(randomToken())[:16], Email: email})
	if err != nil {
		h.redirect(w, r, "/users", failure("Benutzer nicht angelegt: %v", err))
		return
	}

	if r.PostForm.Get("is_admin") == "on" {
		err = h.state.SetUserAdmin(types.UserID(user.ID), true)
		if err != nil {
			h.redirect(w, r, "/users", failure("Benutzer angelegt, Admin-Zugang nicht gesetzt: %v", err))
			return
		}
	}

	h.change(changes)
	h.redirect(w, r, fmt.Sprintf("/users/%d", user.ID), success("Benutzer angelegt. UID: %d. Ein lokales Startpasswort kann hier erzeugt werden.", user.ID))
}

func (h *Handler) userDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		http.NotFound(w, r)

		return
	}

	user, err := h.state.GetUserByID(types.UserID(id))
	if err != nil {
		http.NotFound(w, r)

		return
	}

	u := user.View()

	nodes := []templates.AdminNodeRow{}

	for _, n := range h.state.ListNodesByUser(types.UserID(id)).All() {
		if !n.IsTagged() {
			nodes = append(nodes, nodeRow(n))
		}
	}

	row := userRow(u, len(nodes))

	account, accountErr := h.state.UserAccount(types.UserID(id))
	if accountErr == nil {
		row.IsAdmin = account.IsAdmin
		row.HasPassword = len(account.PasswordHash) > 0
		row.MustChangePassword = account.MustChangePassword
	}

	writeHTML(w, http.StatusOK, templates.AdminUserDetail(h.pageInfo(r), row, nodes, h.zoneAssignment(types.UserID(id), 0)).Render())
}

func userRow(u types.UserView, nodeCount int) templates.AdminUserRow {
	provider := "CLI"
	if u.Provider() == util.RegisterMethodOIDC {
		provider = "OIDC"
	}

	return templates.AdminUserRow{
		ID:          u.ID(),
		Name:        u.Name(),
		DisplayName: u.DisplayName(),
		Email:       u.Email(),
		Provider:    provider,
		NodeCount:   nodeCount,
		CreatedAt:   u.Model().CreatedAt,
	}
}

func (h *Handler) renameUser(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		http.NotFound(w, r)

		return
	}

	back := fmt.Sprintf("/users/%d", id)

	name := formValue(r, "name")
	if name == "" {
		h.redirect(w, r, back, failure("Enter the new username."))

		return
	}

	_, c, err := h.state.RenameUser(types.UserID(id), name)
	if err != nil {
		if errors.Is(err, db.ErrUserNotFound) {
			http.NotFound(w, r)

			return
		}

		h.redirect(w, r, back, failure("Could not rename the user: %v", err))

		return
	}

	h.change(c)
	h.redirect(w, r, back, success("Renamed the user to %s.", name))
}

func (h *Handler) confirmDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		http.NotFound(w, r)

		return
	}

	user, err := h.state.GetUserByID(types.UserID(id))
	if err != nil {
		http.NotFound(w, r)

		return
	}

	u := user.View()

	writeHTML(w, http.StatusOK, templates.AdminConfirm(h.pageInfo(r), templates.AdminConfirmInfo{
		Section: "Users",
		Title:   "Delete user",
		Subject: u.Display(),
		Message: "This deletes the user and their pre-auth keys. It cannot be undone. " +
			"Users who still own devices cannot be deleted; delete those devices first.",
		Action: fmt.Sprintf("/users/%d/delete", id),
		Button: "Delete user",
		Cancel: fmt.Sprintf("/users/%d", id),
	}).Render())
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		http.NotFound(w, r)

		return
	}

	user, err := h.state.GetUserByID(types.UserID(id))
	if err != nil {
		http.NotFound(w, r)

		return
	}

	name := user.View().Display()

	c, err := h.state.DeleteUser(types.UserID(id))
	if err != nil {
		msg := failure("Could not delete the user: %v", err)
		if errors.Is(err, db.ErrUserStillHasNodes) {
			msg = failure("%s still owns devices. Delete them first.", name)
		}

		h.redirect(w, r, fmt.Sprintf("/users/%d", id), msg)

		return
	}

	h.change(c)
	h.redirect(w, r, "/users", success("Deleted user %s.", name))
}

// Nodes

func (h *Handler) nodeFromURL(w http.ResponseWriter, r *http.Request) (types.NodeView, bool) {
	id, ok := urlID(r)
	if !ok {
		http.NotFound(w, r)

		return types.NodeView{}, false
	}

	node, ok := h.state.GetNodeByID(types.NodeID(id))
	if !ok {
		http.NotFound(w, r)

		return types.NodeView{}, false
	}

	return node, true
}

func nodePath(node types.NodeView) string {
	return fmt.Sprintf("/nodes/%d", node.ID().Uint64())
}

func (h *Handler) nodeDetail(w http.ResponseWriter, r *http.Request) {
	node, ok := h.nodeFromURL(w, r)
	if !ok {
		return
	}

	writeHTML(w, http.StatusOK, templates.AdminNodeDetail(h.pageInfo(r), nodeRow(node), templates.AdminNodeExtra{
		Zone:           h.zoneAssignment(0, node.ID()),
		MachineKey:     shortMachineKey(node.MachineKey().String()),
		RegisterMethod: node.RegisterMethod(),
		CreatedAt:      node.CreatedAt(),
		Routes:         h.routeRows(node),
	}).Render())
}

func (h *Handler) renameNode(w http.ResponseWriter, r *http.Request) {
	node, ok := h.nodeFromURL(w, r)
	if !ok {
		return
	}

	name := formValue(r, "name")
	if name == "" {
		h.redirect(w, r, nodePath(node), failure("Enter the new name."))

		return
	}

	renamed, c, err := h.state.RenameNode(node.ID(), name)
	h.change(c)

	if err != nil {
		h.redirect(w, r, nodePath(node), failure("Could not rename the node: %v", err))

		return
	}

	h.redirect(w, r, nodePath(node), success("Renamed the node to %s.", renamed.GivenName()))
}

// parseTags splits a comma or space separated tag list and checks each tag
// the way the v1 API does.
func parseTags(raw string) ([]string, error) {
	var tags []string

	for _, tag := range strings.FieldsFunc(raw, func(c rune) bool { return c == ',' || c == ' ' }) {
		switch {
		case !strings.HasPrefix(tag, "tag:"):
			return nil, fmt.Errorf("%w: %s", errTagMissingPrefix, tag)
		case strings.ToLower(tag) != tag:
			return nil, fmt.Errorf("%w: %s", errTagNotLowercase, tag)
		}

		if !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
	}

	return tags, nil
}

func (h *Handler) setNodeTags(w http.ResponseWriter, r *http.Request) {
	node, ok := h.nodeFromURL(w, r)
	if !ok {
		return
	}

	tags, err := parseTags(r.PostForm.Get("tags"))
	if err == nil && len(tags) == 0 {
		err = errNoTags
	}

	if err != nil {
		h.redirect(w, r, nodePath(node), failure("Could not set the tags: %v", err))

		return
	}

	_, c, err := h.state.SetNodeTags(node.ID(), tags)
	h.change(c)

	if err != nil {
		h.redirect(w, r, nodePath(node), failure("Could not set the tags: %v", err))

		return
	}

	h.redirect(w, r, nodePath(node), success("Set tags to %s.", strings.Join(tags, ", ")))
}

func (h *Handler) confirmExpireNode(w http.ResponseWriter, r *http.Request) {
	node, ok := h.nodeFromURL(w, r)
	if !ok {
		return
	}

	writeHTML(w, http.StatusOK, templates.AdminConfirm(h.pageInfo(r), templates.AdminConfirmInfo{
		Section: "Nodes",
		Title:   "Expire node",
		Subject: node.GivenName(),
		Message: "The device is disconnected from the tailnet until its user signs in again.",
		Action:  nodePath(node) + "/expire",
		Button:  "Expire node",
		Cancel:  nodePath(node),
	}).Render())
}

func (h *Handler) expireNode(w http.ResponseWriter, r *http.Request) {
	node, ok := h.nodeFromURL(w, r)
	if !ok {
		return
	}

	now := time.Now()

	_, c, err := h.state.SetNodeExpiry(node.ID(), &now)
	h.change(c)

	if err != nil {
		h.redirect(w, r, nodePath(node), failure("Could not expire the node: %v", err))

		return
	}

	h.redirect(w, r, nodePath(node), success("Expired %s.", node.GivenName()))
}

func (h *Handler) confirmDeleteNode(w http.ResponseWriter, r *http.Request) {
	node, ok := h.nodeFromURL(w, r)
	if !ok {
		return
	}

	writeHTML(w, http.StatusOK, templates.AdminConfirm(h.pageInfo(r), templates.AdminConfirmInfo{
		Section: "Nodes",
		Title:   "Delete node",
		Subject: node.GivenName(),
		Message: "The device is removed from the tailnet. It has to be registered again to reconnect.",
		Action:  nodePath(node) + "/delete",
		Button:  "Delete node",
		Cancel:  nodePath(node),
	}).Render())
}

func (h *Handler) deleteNode(w http.ResponseWriter, r *http.Request) {
	node, ok := h.nodeFromURL(w, r)
	if !ok {
		return
	}

	changes, err := h.state.DeleteNode(node)
	h.change(changes...)

	if err != nil {
		h.redirect(w, r, nodePath(node), failure("Could not delete the node: %v", err))

		return
	}

	h.redirect(w, r, "/nodes", success("Deleted %s.", node.GivenName()))
}

// Pending auth requests

// openAuthRequest returns the undecided pending request named in the URL. A
// missing or already decided request redirects back to the list.
func (h *Handler) openAuthRequest(w http.ResponseWriter, r *http.Request) (types.AuthID, *types.AuthRequest, bool) {
	id, err := types.AuthIDFromString(chi.URLParam(r, "authID"))
	if err != nil {
		http.NotFound(w, r)

		return "", nil, false
	}

	req, ok := h.state.GetAuthCacheEntry(id)
	if !ok || req.IsFinished() {
		h.redirect(w, r, "/pending", failure("That request is no longer pending."))

		return "", nil, false
	}

	return id, req, true
}

func (h *Handler) pendingReview(w http.ResponseWriter, r *http.Request) {
	id, req, ok := h.openAuthRequest(w, r)
	if !ok {
		return
	}

	row, ok := h.pendingRow(state.AuthCacheEntry{ID: id, Request: req})
	if !ok {
		http.NotFound(w, r)

		return
	}

	users, err := h.userChoices()
	if err != nil {
		serverError(w, "listing users", err)

		return
	}

	writeHTML(w, http.StatusOK, templates.AdminPendingReview(h.pageInfo(r), row, users).Render())
}

func (h *Handler) assignPending(w http.ResponseWriter, r *http.Request) {
	id, req, ok := h.openAuthRequest(w, r)
	if !ok {
		return
	}

	back := "/pending/" + id.String()

	if !req.IsRegistration() {
		h.redirect(w, r, back, failure("Only device registrations can be assigned to a user."))

		return
	}

	userID, err := strconv.ParseUint(formValue(r, "user_id"), 10, 64)
	if err != nil {
		h.redirect(w, r, back, failure("Choose a user."))

		return
	}

	user, err := h.state.GetUserByID(types.UserID(userID))
	if err != nil {
		h.redirect(w, r, back, failure("That user no longer exists."))

		return
	}

	// Same sequence as the v1 authRegister handler.
	node, nodeChange, err := h.state.HandleNodeFromAuthPath(id, types.UserID(user.ID), nil, util.RegisterMethodCLI)
	if err != nil {
		h.change(nodeChange)
		h.redirect(w, r, back, failure("Could not register the device: %v", err))

		return
	}

	routeChange, err := h.state.AutoApproveRoutes(node)
	h.change(nodeChange, routeChange)

	if err != nil {
		log.Error().Err(err).Uint64("node.id", node.ID().Uint64()).Msg("web admin: auto approving routes")
		h.redirect(w, r, nodePath(node), failure("Registered %s, but could not auto-approve its routes. Check the server logs.", node.GivenName()))

		return
	}

	log.Info().
		Str("admin", sessionFrom(r).username).
		Str("node", node.GivenName()).
		Str("user", user.Name).
		Msg("web admin registered node")

	h.redirect(w, r, nodePath(node), success("Registered %s for %s.", node.GivenName(), user.View().Display()))
}

func (h *Handler) approvePending(w http.ResponseWriter, r *http.Request) {
	id, req, ok := h.openAuthRequest(w, r)
	if !ok {
		return
	}

	if !req.IsSSHCheck() {
		h.redirect(w, r, "/pending/"+id.String(), failure("Registrations are approved by assigning them to a user."))

		return
	}

	req.FinishAuth(types.AuthVerdict{})
	h.redirect(w, r, "/pending", success("Approved the SSH check."))
}

func (h *Handler) rejectPending(w http.ResponseWriter, r *http.Request) {
	_, req, ok := h.openAuthRequest(w, r)
	if !ok {
		return
	}

	req.FinishAuth(types.AuthVerdict{Err: errAuthRejected})

	msg := success("Rejected the SSH check.")
	if req.IsRegistration() {
		msg = success("Rejected the registration. If the device tries again, it shows up here as a new request.")
	}

	h.redirect(w, r, "/pending", msg)
}

// Pre-auth keys

func (h *Handler) keys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.state.ListPreAuthKeys()
	if err != nil {
		serverError(w, "listing pre-auth keys", err)

		return
	}

	users, err := h.userChoices()
	if err != nil {
		serverError(w, "listing users", err)

		return
	}

	now := time.Now()
	rows := make([]templates.AdminKeyRow, 0, len(keys))

	for i := range keys {
		rows = append(rows, keyRow(keys[i].View(), now))
	}

	slices.SortFunc(rows, func(a, b templates.AdminKeyRow) int {
		return cmp.Compare(b.ID, a.ID)
	})

	writeHTML(w, http.StatusOK, templates.AdminKeys(h.pageInfo(r), rows, users, keyExpiries).Render())
}

func keyRow(k types.PreAuthKeyView, now time.Time) templates.AdminKeyRow {
	row := templates.AdminKeyRow{
		ID:        k.ID(),
		Prefix:    "hskey-auth-" + k.Prefix() + "-***",
		Tags:      k.Tags().AsSlice(),
		Reusable:  k.Reusable(),
		Ephemeral: k.Ephemeral(),
	}

	if u := k.User(); u.Valid() {
		row.User = u.Username()
	}

	row.CreatedAt, _ = k.CreatedAt().GetOk()
	row.Expiration, _ = k.Expiration().GetOk()

	_, revoked := k.Revoked().GetOk()

	switch {
	case revoked:
		row.Status = "Revoked"
	case !row.Expiration.IsZero() && !row.Expiration.After(now):
		row.Status = "Expired"
	case k.Used() && !k.Reusable():
		row.Status = "Used"
	default:
		row.Status = "Active"
	}

	return row
}

func (h *Handler) createKey(w http.ResponseWriter, r *http.Request) {
	tags, err := parseTags(r.PostForm.Get("tags"))
	if err != nil {
		h.redirect(w, r, "/keys", failure("Could not create the key: %v", err))

		return
	}

	var userID *types.UserID

	if raw := formValue(r, "user_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			h.redirect(w, r, "/keys", failure("Choose a user."))

			return
		}

		user, err := h.state.GetUserByID(types.UserID(id))
		if err != nil {
			h.redirect(w, r, "/keys", failure("That user no longer exists."))

			return
		}

		userID = user.TypedID()
	}

	if userID == nil && len(tags) == 0 {
		h.redirect(w, r, "/keys", failure("Choose a user or enter tags for the new key."))

		return
	}

	lifetime := formValue(r, "expiry")
	if !slices.ContainsFunc(keyExpiries, func(c templates.AdminChoice) bool { return c.Value == lifetime }) {
		h.redirect(w, r, "/keys", failure("Choose how long the key stays valid."))

		return
	}

	d, _ := time.ParseDuration(lifetime) // validated against keyExpiries above
	expiration := time.Now().Add(d)

	key, err := h.state.CreatePreAuthKey(
		userID,
		r.PostForm.Get("reusable") == "on",
		r.PostForm.Get("ephemeral") == "on",
		&expiration,
		tags,
	)
	if err != nil {
		h.redirect(w, r, "/keys", failure("Could not create the key: %v", err))

		return
	}

	log.Info().Str("admin", sessionFrom(r).username).Uint64("key.id", key.ID).Msg("web admin created pre-auth key")

	h.redirect(w, r, "/keys", &templates.AdminFlash{
		Message: "Created a new pre-auth key. Copy it now: it is not shown again.",
		Secret:  key.Key,
	})
}

func (h *Handler) keyFromURL(w http.ResponseWriter, r *http.Request) (templates.AdminKeyRow, bool) {
	id, ok := urlID(r)
	if !ok {
		http.NotFound(w, r)

		return templates.AdminKeyRow{}, false
	}

	keys, err := h.state.ListPreAuthKeys()
	if err != nil {
		serverError(w, "listing pre-auth keys", err)

		return templates.AdminKeyRow{}, false
	}

	for i := range keys {
		if keys[i].ID == id {
			return keyRow(keys[i].View(), time.Now()), true
		}
	}

	http.NotFound(w, r)

	return templates.AdminKeyRow{}, false
}

func (h *Handler) confirmExpireKey(w http.ResponseWriter, r *http.Request) {
	key, ok := h.keyFromURL(w, r)
	if !ok {
		return
	}

	writeHTML(w, http.StatusOK, templates.AdminConfirm(h.pageInfo(r), templates.AdminConfirmInfo{
		Section: "Keys",
		Title:   "Expire pre-auth key",
		Subject: key.Prefix,
		Message: "The key can no longer register devices. Devices it already registered stay connected.",
		Action:  fmt.Sprintf("/keys/%d/expire", key.ID),
		Button:  "Expire key",
		Cancel:  "/keys",
	}).Render())
}

func (h *Handler) expireKey(w http.ResponseWriter, r *http.Request) {
	key, ok := h.keyFromURL(w, r)
	if !ok {
		return
	}

	err := h.state.ExpirePreAuthKey(key.ID)
	if err != nil {
		h.redirect(w, r, "/keys", failure("Could not expire the key: %v", err))

		return
	}

	h.redirect(w, r, "/keys", success("Expired key %s.", key.Prefix))
}

func (h *Handler) routeRows(node types.NodeView) []templates.AdminRouteRow {
	prefixes := append(node.AnnouncedRoutes(), node.ApprovedRoutes().AsSlice()...)
	slices.SortFunc(prefixes, netip.Prefix.Compare)
	prefixes = slices.Compact(prefixes)

	rows := make([]templates.AdminRouteRow, 0, len(prefixes))
	for _, prefix := range prefixes {
		rows = append(rows, templates.AdminRouteRow{
			NodeID: node.ID().Uint64(), Device: node.GivenName(), Prefix: prefix.String(),
			Advertised: slices.Contains(node.AnnouncedRoutes(), prefix),
			Approved:   slices.Contains(node.ApprovedRoutes().AsSlice(), prefix),
			Automatic:  h.state.RouteAutoApproval(node, prefix),
			Exit:       prefix.Bits() == 0,
		})
	}

	return rows
}

func (h *Handler) routeOverview(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, BasePath+"/zones#zone-routes", http.StatusSeeOther)
}

func (h *Handler) setNodeRoute(w http.ResponseWriter, r *http.Request) {
	h.routesMu.Lock()
	defer h.routesMu.Unlock()

	node, ok := h.nodeFromURL(w, r)
	if !ok {
		return
	}

	back := nodePath(node)

	prefix, err := netip.ParsePrefix(formValue(r, "route"))

	action := formValue(r, "action")
	if err != nil || prefix != prefix.Masked() || (action != "approve" && action != "revoke") {
		http.Error(w, "invalid route action", http.StatusBadRequest)
		return
	}

	targets := []netip.Prefix{prefix}
	if prefix.Bits() == 0 {
		targets = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
	}

	approved := node.ApprovedRoutes().AsSlice()
	if action == "approve" {
		// Never grant a route supplied only by a forged or outdated form.
		if !slices.Contains(node.AnnouncedRoutes(), prefix) {
			h.redirect(w, r, back, failure("The device no longer advertises this route. Refresh and review its routes."))
			return
		}

		approved = append(approved, targets...)
	} else {
		approved = slices.DeleteFunc(approved, func(p netip.Prefix) bool { return slices.Contains(targets, p) })
	}

	slices.SortFunc(approved, netip.Prefix.Compare)
	approved = slices.Compact(approved)
	_, c, err := h.state.SetApprovedRoutes(node.ID(), approved)
	h.change(c)

	if err != nil {
		h.redirect(w, r, back, failure("Could not save route approval: %v", err))
		return
	}

	log.Info().Str("admin", sessionFrom(r).username).Uint64("node.id", node.ID().Uint64()).Str("route", prefix.String()).Str("action", action).Msg("web admin route approval")

	message := "Route approval saved. Approval does not prove reachability."
	if action == "revoke" {
		message = "Route approval revoked. Matching autoApprovers may approve it again on a later advertisement or policy reload."
	}

	h.redirect(w, r, back, success("%s", message))
}
