// Package webui serves the Meshora web admin dashboard under /admin.
//
// Admins log in with local accounts ([types.WebAdmin]), which are separate
// from tailnet users. Sessions live in memory only, so a restart logs every
// admin out. All state-changing requests are POSTs that carry a per-session
// CSRF token, and the session cookie is SameSite=Strict.
package webui

import (
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
	adminID uint64
	// passwordFP fingerprints the account's password hash at login. Setting a
	// password always draws a new salt, so a password change ends every
	// session started with the old one.
	passwordFP string
	username   string
	csrf       string
}

// Handler serves the dashboard.
type Handler struct {
	state *state.State

	// publicPath is BasePath as the browser sees it, including any path
	// prefix of server_url. Cookies and redirects use it.
	publicPath    string
	serverURL     string
	secureCookies bool

	// sessions is keyed by the SHA-256 of the session token, so the tokens
	// themselves are never held in memory after they are handed out.
	sessions *expirable.LRU[string, *session]

	failuresMu sync.Mutex
	failures   *expirable.LRU[string, int]
}

// New builds the dashboard handler for cfg.
func New(st *state.State, cfg *types.Config) *Handler {
	publicPath := BasePath
	secure := false

	if u, err := url.Parse(cfg.ServerURL); err == nil { //nolint:noinlineerr
		publicPath = strings.TrimSuffix(u.Path, "/") + BasePath
		secure = u.Scheme == "https"
	}

	return &Handler{
		state:         st,
		publicPath:    publicPath,
		serverURL:     strings.TrimSuffix(cfg.ServerURL, "/"),
		secureCookies: secure,
		sessions:      expirable.NewLRU[string, *session](maxSessions, nil, cfg.WebAdmin.SessionTTL),
		failures:      expirable.NewLRU[string, int](maxFailureKeys, nil, loginFailureWindow),
	}
}

// Routes mounts the dashboard on r under [BasePath].
func (h *Handler) Routes(r chi.Router) {
	r.Route(BasePath, func(r chi.Router) {
		r.Use(adminHeaders)
		r.Get(templates.AdminStylesheetPath, stylesheet)
		r.Get("/login", h.loginPage)
		r.Post("/login", h.login)

		r.Group(func(r chi.Router) {
			r.Use(h.requireSession)
			r.Get("/", h.overview)
			r.Post("/logout", h.logout)
		})
	})
}

func adminHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", adminCSP)
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
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
	if _, ok := h.currentSession(r); ok {
		http.Redirect(w, r, h.publicPath+"/", http.StatusSeeOther)

		return
	}

	h.renderLogin(w, r, http.StatusOK, "", "")
}

// renderLogin issues a fresh login CSRF token (double-submit cookie) and
// renders the login form.
func (h *Handler) renderLogin(w http.ResponseWriter, r *http.Request, status int, username, msg string) {
	token := randomToken()
	h.setCookie(w, r, loginCSRFCookie, token, int(loginFailureWindow.Seconds()))

	writeHTML(w, status, templates.AdminLogin(templates.AdminLoginInfo{
		Base:      h.publicPath,
		CSRFToken: token,
		Username:  username,
		Error:     msg,
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

	admin, err := h.state.AuthenticateWebAdmin(username, password)
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

	h.clearFailures(userKey)

	token := randomToken()
	h.sessions.Add(hashToken(token), &session{
		adminID:    admin.ID,
		passwordFP: hashToken(string(admin.PasswordHash)),
		username:   admin.Username,
		csrf:       randomToken(),
	})

	h.setCookie(w, r, loginCSRFCookie, "", -1)
	h.setCookie(w, r, sessionCookie, token, 0)
	log.Info().Str("username", admin.Username).Str("remote", r.RemoteAddr).Msg("web admin logged in")

	http.Redirect(w, r, h.publicPath+"/", http.StatusSeeOther)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil { //nolint:noinlineerr
		h.sessions.Remove(hashToken(cookie.Value))
	}

	h.setCookie(w, r, sessionCookie, "", -1)
	http.Redirect(w, r, h.publicPath+"/login", http.StatusSeeOther)
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
		Username:    sess.username,
		Base:        h.publicPath,
		ServerURL:   h.serverURL,
		CSRFToken:   sess.csrf,
		UserCount:   len(users),
		NodeCount:   nodes.Len(),
		OnlineCount: online,
	}).Render())
}

// requireSession rejects requests without a valid session, and POSTs
// without the session's CSRF token.
func (h *Handler) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := h.currentSession(r)
		if !ok {
			http.Redirect(w, r, h.publicPath+"/login", http.StatusSeeOther)

			return
		}

		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)

			err := r.ParseForm()
			if err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)

				return
			}

			if !tokensEqual(sess.csrf, r.PostForm.Get(templates.AdminCSRFField)) {
				log.Warn().Err(errCSRFMismatch).Str("username", sess.username).Msg("web admin request rejected")
				http.Error(w, "forbidden", http.StatusForbidden)

				return
			}
		}

		next.ServeHTTP(w, r.WithContext(withSession(r.Context(), sess)))
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

	admin, err := h.state.GetWebAdminByID(sess.adminID)
	if err != nil || hashToken(string(admin.PasswordHash)) != sess.passwordFP {
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

// setCookie sets an HttpOnly, SameSite=Strict cookie scoped to the
// dashboard. maxAge 0 makes a session cookie, negative deletes it.
func (h *Handler) setCookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge int) {
	//nolint:gosec // G124: Secure from server_url scheme or req.TLS; HttpOnly + SameSite set
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     h.publicPath,
		MaxAge:   maxAge,
		Secure:   h.secureCookies || r.TLS != nil,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func writeHTML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

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
