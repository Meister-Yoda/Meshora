// Package webui serves public authentication, personal accounts, and administration.
//
// Local and OIDC authentication share immutable user identities. Sessions live
// in memory only, so a restart logs users out. All state-changing requests are POSTs that carry a per-session
// CSRF token, and the session cookie is SameSite=Strict.
package webui

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/juanfont/headscale/hscontrol/assets"
	"github.com/juanfont/headscale/hscontrol/db"
	"github.com/juanfont/headscale/hscontrol/state"
	"github.com/juanfont/headscale/hscontrol/templates"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
	"github.com/rs/zerolog/log"
)

// BasePath is the routed path of the dashboard.
const BasePath = "/admin"

const (
	sessionCookie   = "meshora_admin"
	loginCSRFCookie = "meshora_admin_login"

	maxSessions = 1024

	// Failed logins are counted per client IP and per username; reaching the
	// limit on either blocks further attempts until the window expires.
	maxLoginFailures   = 10
	loginFailureWindow = 15 * time.Minute
	maxFailureKeys     = 4096

	maxFormBytes = 16 << 10

	// adminCSP replaces the server-wide CSP: no scripts, no inline styles and
	// no third-party origins. The only stylesheet is assets.AdminCSS.
	adminCSP = "default-src 'none'; style-src 'self'; img-src 'self'; " +
		"form-action 'self'; base-uri 'none'; frame-ancestors 'none'"
)

var (
	errCSRFMismatch    = errors.New("csrf token mismatch")
	msgInvalidLogin    = "Invalid username or password."
	msgTooManyAttempts = "Too many failed login attempts. Try again later."
)

// session is the server-side state of a logged-in admin.
type session struct {
	managementID       string
	userAgent          string
	endsAt             time.Time
	userID             types.UserID
	accountRevision    uint64
	isAdmin            bool
	mustChangePassword bool
	passkey            bool
	strongAuth         bool
	enrollmentOnly     bool
	validUntil         time.Time
	authenticatedAt    time.Time
	oidcSubject        string
	expires            time.Time
	adminID            uint64
	// passwordFP fingerprints the account's password hash at login. Setting a
	// password always draws a new salt, so a password change ends every
	// session started with the old one.
	passwordFP string
	username   string
	csrf       string

	// flash is shown once on the page after a POST-redirect-GET. It lives
	// here rather than in the URL so nothing the admin typed is reflected
	// from a query string, and a new pre-auth key never lands in a URL.
	mu          sync.Mutex
	flash       *templates.AdminFlash
	policyDraft *policyEditorDraft
}

func (s *session) setFlash(f *templates.AdminFlash) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.flash = f
}

// takeFlash returns the pending flash and clears it.
func (s *session) takeFlash() *templates.AdminFlash {
	s.mu.Lock()
	defer s.mu.Unlock()

	f := s.flash
	s.flash = nil

	return f
}

// Handler serves the dashboard.
type Handler struct {
	mailConfig    types.MailConfig
	emailSender   func(context.Context, types.MailConfig, string, string) error
	state         *state.State
	runtimeConfig *types.Config
	// change distributes state changes to connected nodes, like
	// apiv1.Backend.Change.
	change func(...change.Change)

	// publicPath is BasePath as the browser sees it, including any path
	// prefix of server_url. Cookies and redirects use it.
	publicPath          string
	serverURL           string
	secureCookies       bool
	configPath          string
	activeOIDC          types.OIDCConfig
	adminOIDCGroups     []string
	adminOIDCRoles      []string
	adminOIDCRolesClaim string
	oidcButtonText      string
	oidcFlows           *expirable.LRU[string, adminOIDCFlow]
	oidcFlowsMu         sync.Mutex
	securityFlows       *expirable.LRU[string, securityFlow]
	securityFlowsMu     sync.Mutex
	settingsMu          sync.Mutex
	routesMu            sync.Mutex

	// sessions is keyed by the SHA-256 of the session token, so the tokens
	// themselves are never held in memory after they are handed out.
	sessions *expirable.LRU[string, *session]

	failuresMu sync.Mutex
	failures   *expirable.LRU[string, int]
}

// New builds the dashboard handler for cfg. notify receives the changes of
// every dashboard action, so connected nodes see them.
func New(st *state.State, cfg *types.Config, notify func(...change.Change)) *Handler {
	publicPath := BasePath
	secure := false

	if u, err := url.Parse(cfg.ServerURL); err == nil { //nolint:noinlineerr
		publicPath = strings.TrimSuffix(u.Path, "/") + BasePath
		secure = u.Scheme == "https"
	}

	return &Handler{
		mailConfig:          cfg.Mail,
		emailSender:         sendAccountMail,
		state:               st,
		runtimeConfig:       cfg,
		change:              notify,
		publicPath:          publicPath,
		serverURL:           strings.TrimSuffix(cfg.ServerURL, "/"),
		secureCookies:       secure,
		configPath:          cfg.ConfigFilePath,
		activeOIDC:          cfg.OIDC,
		adminOIDCGroups:     slices.Clone(cfg.WebAdmin.OIDCAllowedGroups),
		adminOIDCRoles:      slices.Clone(cfg.WebAdmin.OIDCAllowedRoles),
		adminOIDCRolesClaim: cmp.Or(cfg.WebAdmin.OIDCRolesClaim, "roles"),
		oidcButtonText:      strings.TrimSpace(cfg.WebAdmin.OIDCButtonText),
		securityFlows:       expirable.NewLRU[string, securityFlow](maxSessions, nil, securityTTL),
		oidcFlows:           expirable.NewLRU[string, adminOIDCFlow](maxSessions, nil, 10*time.Minute),
		sessions:            expirable.NewLRU[string, *session](maxSessions, nil, cfg.WebAdmin.SessionTTL),
		failures:            expirable.NewLRU[string, int](maxFailureKeys, nil, loginFailureWindow),
	}
}

// Close stops the dashboard caches' background TTL sweepers after HTTP shutdown.
func (h *Handler) Close() {
	h.oidcFlows.Close()
	h.securityFlows.Close()
	h.sessions.Close()
	h.failures.Close()
}

// Routes separates public login, personal account pages and administrator-only pages.
func (h *Handler) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(adminHeaders)
		r.Get("/", h.loginPage)
		r.Post("/", h.login)
		r.Get("/login/2fa", h.secondFactorPage)
		r.Post("/login/2fa", h.finishSecondFactor)
		r.Post("/login/oidc", h.oidcLogin)
		r.Post("/login/passkeys/begin", h.beginPasskeyLogin)
		r.Post("/login/passkeys/finish", h.finishPasskeyLogin)
		// Keep the registered provider redirect URI and its narrowly scoped flow cookie.
		r.Get("/admin/oidc/callback", h.oidcCallback)
		r.Get("/connect", h.enrollment)
		r.Get("/invite", h.invitationPage)
		r.Post("/invite", h.acceptInvitation)
		r.Get(templates.AdminStylesheetPath, stylesheet)
		r.Get("/assets/security.js", h.securityScript)
		r.Get("/assets/enrollment.js", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write(assets.EnrollmentJS)
		})
		r.Get("/assets/zones.js", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write(assets.ZonesJS)
		})
		r.Get("/setup", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, h.loginPath(), http.StatusSeeOther) })

		//nolint:gosec // G101: public URL aliases, not credentials.
		aliases := map[string]string{
			"/admin/login":                 "/",
			"/admin/login/oidc":            "/login/oidc",
			"/admin/passkeys/login/begin":  "/login/passkeys/begin",
			"/admin/passkeys/login/finish": "/login/passkeys/finish",
			"/admin/logout":                "/logout",
			"/admin/account":               "/account",
		}
		for oldPath, newPath := range aliases {
			r.Handle(oldPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.redirectLegacy(w, r, newPath) }))
		}

		r.Handle("/admin/account/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.redirectLegacy(w, r, strings.TrimPrefix(r.URL.Path, BasePath))
		}))
		r.Handle("/admin/assets/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.redirectLegacy(w, r, strings.TrimPrefix(r.URL.Path, BasePath))
		}))
		r.Group(func(r chi.Router) {
			r.Use(h.requireSession)
			r.Get("/account", h.accountPage)
			r.Get("/account/devices", h.accountDevices)
			r.Get("/account/sessions", h.accountSessions)
			r.Post("/account/sessions/revoke", h.revokeAccountSessions)
			r.Get("/account/email", h.accountEmailPage)
			r.Get("/account/security", h.securityPage)
			r.Post("/account/security/totp/begin", h.beginTOTP)
			r.Get("/account/security/totp/qr", h.totpQR)
			r.Post("/account/security/totp/enable", h.enableTOTP)
			r.Post("/account/security/totp/disable", h.disableTOTP)
			r.Post("/account/security/passkeys/begin", h.beginPasskeyRegistration)
			r.Post("/account/security/passkeys/finish", h.finishPasskeyRegistration)
			r.Post("/account/security/passkeys/delete", h.deletePasskey)
			r.Post("/account/profile", h.saveAccountProfile)
			r.Post("/account/email", h.requestAccountEmail)
			r.Get("/account/email/confirm", h.accountEmailConfirmation)
			r.Post("/account/email/confirm", h.confirmAccountEmail)
			r.Get("/account/password", h.accountPasswordPage)
			r.Post("/account/password", h.saveAccountPassword)
			r.Get("/account/devices/{id}", h.accountDevice)
			r.Post("/logout", h.logout)
		})
		r.Route(BasePath, func(r chi.Router) {
			r.Use(h.requireSession)
			r.Use(h.requireAdmin)
			r.Post("/users/{id}/admin", h.setAccountAdmin)
			r.Post("/users/{id}/auth-policy", h.saveUserAuthPolicy)
			r.Post("/users/{id}/email-policy", h.saveUserEmailPolicy)
			r.Post("/users/{id}/password", h.resetAccountPassword)
			r.Post("/users/{id}/profile", h.saveUserProfile)
			r.Get("/users/{id}/merge", h.mergeAccountPage)
			r.Post("/users/{id}/merge", h.mergeAccount)
			r.Get("/", h.overview)
			r.Get("/users", h.users)
			r.Post("/users", h.createUser)
			r.Get("/users/{id}", h.userDetail)
			r.Post("/users/{id}/rename", h.renameUser)
			r.Post("/users/{id}/zone", h.assignUserZone)
			r.Post("/users/{id}/exit-provider", h.assignUserZone)
			r.Post("/users/{id}/own-exits", h.assignUserZone)
			r.Post("/users/{id}/exit-limit", h.assignUserZone)
			r.Get("/users/{id}/delete", h.confirmDeleteUser)
			r.Post("/users/{id}/delete", h.deleteUser)

			r.Get("/nodes", h.nodes)
			r.Get("/routes", h.routeOverview)
			r.Get("/zones", h.zonesPage)
			r.Post("/zones", h.updateZones)
			r.Post("/zones/activate", h.activateZones)
			r.Get("/dns", h.dnsSettings)
			r.Get("/policy", h.policyPage)
			r.Get("/policy/edit", h.policyEditor)
			r.Post("/policy/validate", h.validatePolicyDraft)
			r.Post("/policy/save", h.savePolicyDraft)
			r.Post("/policy/activate", h.activatePolicySource)
			r.Post("/dns", h.saveDNSSettings)
			r.Post("/nodes/{id}/routes", h.setNodeRoute)
			r.Get("/nodes/{id}", h.nodeDetail)
			r.Post("/nodes/{id}/rename", h.renameNode)
			r.Post("/nodes/{id}/zone", h.assignNodeZone)
			r.Post("/nodes/{id}/exit-provider", h.assignNodeZone)
			r.Post("/nodes/{id}/own-exits", h.assignNodeZone)
			r.Post("/nodes/{id}/exit-limit", h.assignNodeZone)
			r.Post("/nodes/{id}/tags", h.setNodeTags)
			r.Get("/nodes/{id}/expire", h.confirmExpireNode)
			r.Post("/nodes/{id}/expire", h.expireNode)
			r.Get("/nodes/{id}/delete", h.confirmDeleteNode)
			r.Post("/nodes/{id}/delete", h.deleteNode)

			r.Get("/pending", h.pending)
			r.Get("/pending/{authID}", h.pendingReview)
			r.Post("/pending/{authID}/assign", h.assignPending)
			r.Post("/pending/{authID}/approve", h.approvePending)
			r.Post("/pending/{authID}/reject", h.rejectPending)

			r.Get("/keys", h.keys)
			r.Post("/keys", h.createKey)
			r.Get("/keys/{id}/expire", h.confirmExpireKey)
			r.Post("/keys/{id}/expire", h.expireKey)

			r.Get("/settings", h.settingsPage)
			r.Get("/settings/{section}", h.settingsPage)
			r.Post("/settings/oidc", h.saveOIDCSettings)
			r.Post("/settings/oidc-button", h.saveOIDCButtonText)
			r.Get("/invitations", h.invitationsPage)
			r.Post("/invitations", h.createInvitation)
			r.Post("/invitations/revoke", h.revokeInvitation)
			r.Get("/settings/mail", h.mailSettingsPage)
			r.Post("/settings/mail", h.saveMailSettings)
			r.Post("/settings/mail/test", h.testMailSettings)
		})
	})
}

func (h *Handler) redirectLegacy(w http.ResponseWriter, r *http.Request, path string) {
	target := h.rootPath(path)
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}

	status := http.StatusTemporaryRedirect
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		status = http.StatusSeeOther
	}
	//nolint:gosec // G710: only local canonical paths are used; request data is restricted to path suffixes and query values.
	http.Redirect(w, r, target, status)
}

func adminHeaders(next http.Handler) http.Handler {
	return templates.LanguageMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", adminCSP)
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	}))
}

// stylesheet serves assets.AdminCSS. It is public: the login page needs it.
func stylesheet(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")

	_, err := w.Write(assets.AdminCSS)
	if err != nil {
		log.Error().Err(err).Msg("web admin: writing stylesheet")
	}
}

func (h *Handler) loginPage(w http.ResponseWriter, r *http.Request) {
	if sess, ok := h.currentSession(r); ok {
		target := h.rootPath("/account")
		if sess.isAdmin {
			target = h.publicPath + "/"
		}

		if sess.enrollmentOnly {
			target = h.rootPath("/account/security")
		}
		if sess.mustChangePassword {
			target = h.rootPath("/account/password")
		}

		http.Redirect(w, r, target, http.StatusSeeOther)

		return
	}

	if h.adminOIDCEnabled() && r.URL.Query().Get("local") != "1" && r.URL.Query().Get("lang") == "" {
		h.startOIDC(w, r, true)
		return
	}

	h.renderLogin(w, r, http.StatusOK, "", "")
}

// renderLogin issues a fresh login CSRF token (double-submit cookie) and
// renders the login form.
func (h *Handler) renderLogin(w http.ResponseWriter, r *http.Request, status int, username, msg string) {
	w.Header().Set("Content-Security-Policy", adminCSP+"; script-src 'self'; connect-src 'self'")
	h.settingsMu.Lock()
	buttonText := h.oidcButtonText
	h.settingsMu.Unlock()

	token := randomToken()
	h.setCookie(w, r, loginCSRFCookie, token, int(loginFailureWindow.Seconds()))

	writeHTML(w, status, templates.AdminLogin(templates.AdminLoginInfo{
		Base:           h.publicPath,
		CSRFToken:      token,
		OIDCEnabled:    h.adminOIDCEnabled(),
		OIDCButtonText: buttonText,
		Username:       username,
		Error:          msg,
	}).Render())
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)

		return
	}

	cookie, err := r.Cookie(loginCSRFCookie)
	if err != nil || !tokensEqual(cookie.Value, r.PostForm.Get(templates.AdminCSRFField)) {
		h.renderLogin(w, r, http.StatusForbidden, "", "Your login form expired. Please try again.")

		return
	}

	username := strings.TrimSpace(r.PostForm.Get("username"))
	password := r.PostForm.Get("password")
	ipKey := "ip:" + clientIP(r)
	userKey := "user:" + strings.ToLower(username)

	if h.blocked(ipKey) || h.blocked(userKey) {
		log.Warn().Str("username", username).Str("remote", r.RemoteAddr).Msg("web admin login rate limited")
		h.renderLogin(w, r, http.StatusTooManyRequests, username, msgTooManyAttempts)

		return
	}

	account, err := h.state.AuthenticateUserAccount(username, password)
	if err != nil {
		if !errors.Is(err, db.ErrWebAdminInvalidCredentials) {
			log.Error().Err(err).Msg("web admin login failed")
		}

		h.recordFailure(ipKey)
		h.recordFailure(userKey)
		log.Info().Str("username", username).Str("remote", r.RemoteAddr).Msg("web admin login rejected")
		h.renderLogin(w, r, http.StatusUnauthorized, username, msgInvalidLogin)

		return
	}

	user, err := h.state.GetUserByID(types.UserID(account.UserID))
	if err != nil {
		h.renderLogin(w, r, http.StatusUnauthorized, username, msgInvalidLogin)
		return
	}

	security, err := h.state.UserSecurity(types.UserID(account.UserID))
	if err != nil {
		h.renderLogin(w, r, http.StatusServiceUnavailable, username, "Kontosicherheit nicht verfügbar.")
		return
	}

	policy, err := h.state.UserAuthPolicy(types.UserID(account.UserID))
	if err != nil {
		h.renderLogin(w, r, http.StatusServiceUnavailable, username, "Sicherheitsrichtlinie nicht verfügbar.")
		return
	}

	if len(security.EncryptedTOTPSecret) > 0 && !policy.TOTPBypassedAt(time.Now()) {
		h.startSecondFactor(w, r, account, nil)
		return
	}

	h.clearFailures(userKey)
	h.issueSession(w, r, account, user, &session{})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil { //nolint:noinlineerr
		h.sessions.Remove(hashToken(cookie.Value))
	}

	h.setCookie(w, r, sessionCookie, "", -1)
	http.Redirect(w, r, h.loginPath()+"?local=1", http.StatusSeeOther)
}

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)

	users, err := h.state.ListAllUsers()
	if err != nil {
		log.Error().Err(err).Msg("web admin: listing users")
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	nodes := h.state.ListNodes()

	online := 0

	for _, node := range nodes.All() {
		if isOnline, ok := node.IsOnline().GetOk(); ok && isOnline {
			online++
		}
	}

	writeHTML(w, http.StatusOK, templates.AdminOverview(templates.AdminOverviewInfo{
		Username:     sess.username,
		Base:         h.publicPath,
		ServerURL:    h.serverURL,
		CSRFToken:    sess.csrf,
		UserCount:    len(users),
		NodeCount:    nodes.Len(),
		OnlineCount:  online,
		PendingCount: len(h.openAuthRequests()),
	}).Render())
}

// pageInfo returns the shared page data and consumes the session's flash.
func (h *Handler) pageInfo(r *http.Request) templates.AdminPageInfo {
	sess := sessionFrom(r)

	section := templates.AccountProfile

	path := strings.TrimPrefix(r.URL.Path, h.rootPath("/account"))
	if path == r.URL.Path {
		path = strings.TrimPrefix(r.URL.Path, "/account")
	}

	switch {
	case strings.HasPrefix(path, "/sessions"):
		section = templates.AccountSessions
	case strings.HasPrefix(path, "/devices"):
		section = templates.AccountDevices
	case strings.HasPrefix(path, "/email"):
		section = templates.AccountEmail
	case strings.HasPrefix(path, "/security"):
		section = templates.AccountSecurity
	case strings.HasPrefix(path, "/password"):
		section = templates.AccountPassword
	}

	return templates.AdminPageInfo{
		IsAdmin:           sess.isAdmin,
		AccountSection:    section,
		AccountRestricted: sess.mustChangePassword || sess.enrollmentOnly,
		Base:              h.publicPath,
		Username:          sess.username,
		CSRFToken:         sess.csrf,
		Flash:             sess.takeFlash(),
	}
}

// openAuthRequests returns the pending auth requests nobody has decided on
// yet. Decided ones stay cached until their client picks up the verdict.
func (h *Handler) openAuthRequests() []state.AuthCacheEntry {
	return slices.DeleteFunc(h.state.ListAuthCacheEntries(), func(e state.AuthCacheEntry) bool {
		return e.Request.IsFinished()
	})
}

func (h *Handler) users(w http.ResponseWriter, r *http.Request) {
	users, err := h.state.ListAllUsers()
	if err != nil {
		log.Error().Err(err).Msg("web admin: listing users")
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	// Tagged nodes belong to their tags, not to the user who created them.
	nodesPerUser := make(map[types.UserID]int)

	for _, node := range h.state.ListNodes().All() {
		if !node.IsTagged() {
			nodesPerUser[node.TypedUserID()]++
		}
	}

	rows := make([]templates.AdminUserRow, 0, len(users))
	for i := range users {
		u := users[i].View()
		rows = append(rows, userRow(u, nodesPerUser[types.UserID(u.ID())]))
	}

	writeHTML(w, http.StatusOK, templates.AdminUsers(h.pageInfo(r), rows).Render())
}

func (h *Handler) nodes(w http.ResponseWriter, r *http.Request) {
	nodes := h.state.ListNodes()
	rows := make([]templates.AdminNodeRow, 0, nodes.Len())

	for _, n := range nodes.All() {
		rows = append(rows, nodeRow(n))
	}

	slices.SortFunc(rows, func(a, b templates.AdminNodeRow) int {
		return cmp.Compare(a.GivenName, b.GivenName)
	})

	query := r.URL.Query()
	filters := templates.AdminNodeFilters{Query: strings.TrimSpace(query.Get("q")), Status: query.Get("status"), User: query.Get("user"), Tag: query.Get("tag"), Expiry: query.Get("expiry")}

	filtered := make([]templates.AdminNodeRow, 0, len(rows))
	for _, row := range rows {
		if matchesNodeFilters(row, filters) {
			filtered = append(filtered, row)
		}
	}

	writeHTML(w, http.StatusOK, templates.AdminFilteredNodes(h.pageInfo(r), filtered, rows, filters).Render())
}

func nodeRow(n types.NodeView) templates.AdminNodeRow {
	row := templates.AdminNodeRow{
		ID:        n.ID().Uint64(),
		GivenName: n.GivenName(),
		Hostname:  n.Hostname(),
		Expired:   n.IsExpired(),
	}

	if n.IsTagged() {
		row.Tags = n.Tags().AsSlice()
	} else {
		row.Owner = n.User().Username()
	}

	if ip, ok := n.IPv4().GetOk(); ok {
		row.IPv4 = ip.String()
	}

	if ip, ok := n.IPv6().GetOk(); ok {
		row.IPv6 = ip.String()
	}

	if online, ok := n.IsOnline().GetOk(); ok {
		row.Online = &online
	}

	row.LastSeen, _ = n.LastSeen().GetOk()

	row.Expiry, _ = n.Expiry().GetOk()
	for _, route := range n.AnnouncedRoutes() {
		row.AnnouncedRoutes = append(row.AnnouncedRoutes, route.String())
	}

	for _, route := range n.ApprovedRoutes().All() {
		row.ApprovedRoutes = append(row.ApprovedRoutes, route.String())
	}

	if hi := n.Hostinfo(); hi.Valid() {
		row.OS = hi.OS()
	}

	return row
}

func (h *Handler) pending(w http.ResponseWriter, r *http.Request) {
	entries := h.openAuthRequests()
	rows := make([]templates.AdminPendingRow, 0, len(entries))

	for _, e := range entries {
		if row, ok := h.pendingRow(e); ok {
			rows = append(rows, row)
		}
	}

	writeHTML(w, http.StatusOK, templates.AdminPending(h.pageInfo(r), rows).Render())
}

// pendingRow describes a pending auth request; ok is false for request kinds
// the dashboard cannot show.
func (h *Handler) pendingRow(e state.AuthCacheEntry) (templates.AdminPendingRow, bool) {
	row := templates.AdminPendingRow{
		AuthID:    e.ID.String(),
		CreatedAt: e.Request.CreatedAt(),
	}

	switch {
	case e.Request.IsRegistration():
		reg := e.Request.RegistrationData()
		row.Hostname = reg.Hostname
		row.MachineKey = shortMachineKey(reg.MachineKey.String())

		if reg.Hostinfo != nil {
			row.OS = reg.Hostinfo.OS
		}
	case e.Request.IsSSHCheck():
		binding := e.Request.SSHCheckBinding()
		row.SSHCheck = true
		row.Hostname = h.nodeName(binding.SrcNodeID) + " → " + h.nodeName(binding.DstNodeID)
	default:
		return row, false
	}

	return row, true
}

func (h *Handler) nodeName(id types.NodeID) string {
	if node, ok := h.state.GetNodeByID(id); ok {
		return cmp.Or(node.GivenName(), node.Hostname())
	}

	return "node " + id.String()
}

// shortMachineKey shortens "mkey:<64 hex>" to "mkey:<8 hex>…".
func shortMachineKey(k string) string {
	const keep = len("mkey:") + 8

	if len(k) <= keep {
		return k
	}

	return k[:keep] + "…"
}

// requireSession rejects requests without a valid session, and POSTs
// without the session's CSRF token.
func (h *Handler) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := h.currentSession(r)
		if !ok {
			http.Redirect(w, r, h.loginPath(), http.StatusSeeOther)

			return
		}

		if sess.mustChangePassword && r.URL.Path != "/account/password" && r.URL.Path != "/logout" {
			http.Redirect(w, r, h.rootPath("/account/password"), http.StatusSeeOther)
			return
		}

		if sess.enrollmentOnly && !enrollmentPath(r.URL.Path) {
			http.Redirect(w, r, h.rootPath("/account/security"), http.StatusSeeOther)
			return
		}

		if r.Method == http.MethodPost {
			limit := int64(maxFormBytes)
			if strings.HasPrefix(r.URL.Path, BasePath+"/policy/") {
				limit = 4 << 20
			}

			r.Body = http.MaxBytesReader(w, r.Body, limit)

			err := r.ParseForm()
			if err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)

				return
			}

			if !tokensEqual(sess.csrf, cmp.Or(r.Header.Get("X-CSRF-Token"), r.PostForm.Get(templates.AdminCSRFField))) {
				log.Warn().Err(errCSRFMismatch).Str("username", sess.username).Msg("web admin request rejected")
				http.Error(w, "forbidden", http.StatusForbidden)

				return
			}
		}

		next.ServeHTTP(w, r.WithContext(withSession(r.Context(), sess)))
	})
}

// requireAdmin is applied exclusively to the administration route group.
func (h *Handler) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !sessionFrom(r).isAdmin {
			if strings.TrimSuffix(r.URL.Path, "/") == BasePath {
				http.Redirect(w, r, h.rootPath("/account"), http.StatusSeeOther)
			} else {
				http.Error(w, "Admin-Zugang erforderlich", http.StatusForbidden)
			}

			return
		}

		next.ServeHTTP(w, r)
	})
}

// currentSession returns the session named by the request's cookie, if it is
// live and its account still exists unchanged.
func (h *Handler) currentSession(r *http.Request) (*session, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return nil, false
	}

	key := hashToken(cookie.Value)

	sess, ok := h.sessions.Get(key)
	if !ok {
		return nil, false
	}

	if !sess.validUntil.IsZero() && !time.Now().Before(sess.validUntil) {
		h.sessions.Remove(key)
		return nil, false
	}

	account, err := h.state.UserAccount(sess.userID)
	if err != nil || account.Revision != sess.accountRevision {
		h.sessions.Remove(key)
		return nil, false
	}

	err = h.requireVerifiedFactor(sess)
	if err != nil {
		h.sessions.Remove(key)
		return nil, false
	}
	_, err = h.state.GetUserByID(sess.userID)
	if err != nil {
		h.sessions.Remove(key)
		return nil, false
	}

	if sess.oidcSubject != "" && (!h.adminOIDCEnabled() || !time.Now().Before(sess.expires)) {
		h.sessions.Remove(key)
		return nil, false
	}
	return sess, true
}

func (h *Handler) blocked(key string) bool {
	h.failuresMu.Lock()
	defer h.failuresMu.Unlock()

	n, _ := h.failures.Peek(key)

	return n >= maxLoginFailures
}

func (h *Handler) recordFailure(key string) {
	h.failuresMu.Lock()
	defer h.failuresMu.Unlock()

	// Add resets the entry's TTL, so a block lifts loginFailureWindow after
	// the last failed attempt, not the first.
	n, _ := h.failures.Peek(key)
	h.failures.Add(key, n+1)
}

func (h *Handler) clearFailures(key string) {
	h.failuresMu.Lock()
	defer h.failuresMu.Unlock()

	h.failures.Remove(key)
}

// rootPath keeps canonical public URLs inside the configured reverse-proxy prefix.
func (h *Handler) rootPath(path string) string {
	return strings.TrimSuffix(h.publicPath, BasePath) + path
}

// loginPath is the public application root, including any reverse-proxy prefix.
func (h *Handler) loginPath() string {
	return h.rootPath("/")
}

// setCookie sets an HttpOnly, SameSite=Strict cookie scoped to the application. maxAge 0 makes a session cookie, negative deletes it.
func (h *Handler) setCookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge int) {
	// Remove cookies from the previous /admin scope before setting the root
	// cookie, so browsers cannot send two competing session values.
	//nolint:gosec // G124: deletion cookie retains Secure from the configured HTTPS URL or req.TLS, HttpOnly and Strict SameSite.
	http.SetCookie(w, &http.Cookie{Name: name, Path: h.publicPath, MaxAge: -1, HttpOnly: true, Secure: h.secureCookies || r.TLS != nil, SameSite: http.SameSiteStrictMode})
	//nolint:gosec // G124: Secure from server_url scheme or req.TLS; HttpOnly + SameSite set
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     h.loginPath(),
		MaxAge:   maxAge,
		Secure:   h.secureCookies || r.TLS != nil,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func writeHTML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	//nolint:gosec // G705: body comes from elem-go templates, which escape text and attribute values carrying user input
	_, err := w.Write([]byte(body))
	if err != nil {
		log.Error().Err(err).Msg("web admin: writing response")
	}
}

// randomToken returns 256 bits of crypto/rand entropy, URL-safe encoded.
func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand.Read never fails

	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}

func tokensEqual(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// clientIP returns the host part of r.RemoteAddr, which the server's
// trusted-proxy middleware has already resolved to the real client.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}

type sessionKey struct{}

func withSession(ctx context.Context, s *session) context.Context {
	return context.WithValue(ctx, sessionKey{}, s)
}

// sessionFrom returns the session [Handler.requireSession] attached to the
// request; handlers behind it can rely on it being set.
func sessionFrom(r *http.Request) *session {
	s, _ := r.Context().Value(sessionKey{}).(*session)

	return s
}

func matchesNodeFilters(node templates.AdminNodeRow, filters templates.AdminNodeFilters) bool {
	status := "unknown"

	if node.Online != nil {
		if *node.Online {
			status = "online"
		} else {
			status = "offline"
		}
	}

	if filters.Status != "" && filters.Status != status {
		return false
	}

	if filters.User != "" && (len(node.Tags) > 0 || node.Owner != filters.User) {
		return false
	}

	if filters.Tag != "" && !slices.Contains(node.Tags, filters.Tag) {
		return false
	}

	switch filters.Expiry {
	case "expired":
		if !node.Expired {
			return false
		}
	case "valid":
		if node.Expired {
			return false
		}
	case "never":
		if !node.Expiry.IsZero() {
			return false
		}
	case "":
	default:
		return false
	}

	search := strings.Join([]string{node.GivenName, node.Hostname, node.IPv4, node.IPv6, node.Owner, strings.Join(node.Tags, " ")}, " ")

	return strings.Contains(strings.ToLower(search), strings.ToLower(filters.Query))
}

// enrollment is public and exposes only the configured server address and sign-in instructions.
func (h *Handler) enrollment(w http.ResponseWriter, r *http.Request) {
	platform := r.URL.Query().Get("platform")
	switch platform {
	case "", "windows", "macos", "linux", "android", "ios":
	default:
		http.Error(w, "Unknown platform", http.StatusBadRequest)
		return
	}
	// Other dashboard pages retain the script-free policy.
	w.Header().Set("Content-Security-Policy", adminCSP+"; script-src 'self'")
	writeHTML(w, http.StatusOK, templates.AdminEnrollment(templates.AdminPageInfo{Base: h.publicPath}, h.serverURL, platform, h.activeOIDC.Issuer != "").Render())
}
