package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/juanfont/headscale/hscontrol/assets"
	"github.com/juanfont/headscale/hscontrol/db"
	"github.com/juanfont/headscale/hscontrol/templates"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

const setupCookie = "meshora_setup"

var (
	errSetupURL  = errors.New("invalid public server URL")
	errSetupOIDC = errors.New("OIDC discovery validation failed")
)

type setupHandler struct {
	cfg       *types.Config
	database  *db.HSDatabase
	token     string
	done      chan struct{}
	mu        sync.Mutex
	completed bool
}

func newSetupHandler(cfg *types.Config, database *db.HSDatabase) *setupHandler {
	return &setupHandler{cfg: cfg, database: database, token: randomToken(), done: make(chan struct{})}
}

// RunInitialSetup waits for browser setup only on an explicitly enabled, empty
// installation. The returned shutdown function keeps the success page available
// while the caller initializes the real server, avoiding a restart or dead link.
//
//nolint:contextcheck,nilnil // The DB initializer has no context API; nil means setup is already complete.
func RunInitialSetup(ctx context.Context, cfg *types.Config) (func() error, error) {
	if !cfg.WebAdmin.Enabled || !cfg.WebAdmin.SetupEnabled {
		return nil, nil
	}

	database, err := db.NewHeadscaleDatabase(cfg)
	if err != nil {
		return nil, err
	}

	available, err := database.InitialSetupAvailable()
	if err != nil || !available {
		_ = database.Close()
		return nil, err
	}

	handler := newSetupHandler(cfg, database)

	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", cfg.Addr)
	if err != nil {
		_ = database.Close()
		return nil, err
	}

	server := &http.Server{Handler: handler.routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}

	stopped := make(chan error, 1)
	go func() { stopped <- server.Serve(listener) }()

	shutdown := func() error {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		return server.Shutdown(shutdownCtx)
	}

	log.Info().Msg("Meshora initial setup is available in the browser at /setup")

	select {
	case <-handler.done:
		err := database.Close()
		if err != nil {
			_ = shutdown()
			return nil, err
		}

		return shutdown, nil
	case <-ctx.Done():
		_ = shutdown()
		_ = database.Close()

		return nil, ctx.Err()
	case serveErr := <-stopped:
		_ = database.Close()
		return nil, serveErr
	}
}

func (h *setupHandler) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"pass","setup_required":true}`)
	})
	mux.HandleFunc("GET /admin"+templates.AdminStylesheetPath, stylesheet)

	for path, data := range map[string][]byte{
		"/assets/meshora-logo.png":      assets.Logo,
		"/assets/meshora-logo-dark.png": assets.LogoDark,
		"/favicon.ico":                  assets.Favicon,
	} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			http.ServeContent(w, r, path, time.Unix(0, 0), bytes.NewReader(data))
		})
	}

	mux.HandleFunc("GET /setup", h.page)
	mux.HandleFunc("POST /setup", h.save)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/admin", "/admin/", "/admin/login":
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
		default:
			http.NotFound(w, r)
		}
	})

	return adminHeaders(mux)
}

func (h *setupHandler) page(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.completed {
		writeHTML(w, http.StatusOK, templates.InitialSetupDone().Render())
		return
	}

	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}

	info := templates.SetupInfo{ServerURL: scheme + "://" + r.Host}

	_, _, saved, err := readOIDCSettings(h.cfg.ConfigFilePath)
	if err == nil {
		info.Issuer = saved.Issuer
		info.ClientID = saved.ClientID
		info.HasSecret = saved.Secret != ""
	}

	h.render(w, r, http.StatusOK, info)
}

func (h *setupHandler) render(w http.ResponseWriter, r *http.Request, status int, info templates.SetupInfo) {
	info.CSRFToken = h.token
	//nolint:gosec // G124: HTTP LAN setup permits non-Secure cookies; TLS/proxy HTTPS requests set Secure.
	http.SetCookie(w, &http.Cookie{Name: setupCookie, Value: h.token, Path: "/setup", HttpOnly: true, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteStrictMode, MaxAge: 3600})
	writeHTML(w, status, templates.InitialSetup(info).Render())
}

func (h *setupHandler) save(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.completed {
		http.Error(w, "Einrichtung bereits abgeschlossen.", http.StatusGone)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)

	parseErr := r.ParseForm()
	if parseErr != nil {
		http.Error(w, "Ungültiges Formular.", http.StatusBadRequest)
		return
	}

	cookie, err := r.Cookie(setupCookie)
	if err != nil || !tokensEqual(cookie.Value, h.token) || !tokensEqual(r.PostForm.Get(templates.AdminCSRFField), h.token) {
		http.Error(w, "Das Einrichtungsformular ist abgelaufen.", http.StatusForbidden)
		return
	}

	if origin := r.Header.Get("Origin"); origin != "" {
		parsed, parseErr := url.Parse(origin)
		if parseErr != nil || !strings.EqualFold(parsed.Host, r.Host) || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			http.Error(w, "Ungültiger Ursprung.", http.StatusForbidden)
			return
		}
	}

	info := templates.SetupInfo{ServerURL: strings.TrimSpace(r.PostForm.Get("server_url")), Username: strings.TrimSpace(r.PostForm.Get("username")), Issuer: strings.TrimSpace(r.PostForm.Get("issuer")), ClientID: strings.TrimSpace(r.PostForm.Get("client_id"))}
	fail := func(message string) { info.Error = message; h.render(w, r, http.StatusBadRequest, info) }

	password := r.PostForm.Get("password")
	secret := r.PostForm.Get("client_secret")
	info.OIDCOpen = info.Issuer != "" || info.ClientID != "" || secret != ""

	path, document, saved, err := readOIDCSettings(h.cfg.ConfigFilePath)
	if err != nil {
		//nolint:misspell // German user-facing text.
		fail("Die Konfiguration kann nicht gelesen werden. Es wurde kein Admin-Konto angelegt.")
		return
	}

	info.HasSecret = saved.Secret != ""

	publicURL, message := validateSetupInput(r.Context(), info, password, r.PostForm.Get("password_confirm"), secret, saved, h.cfg.BaseDomain)
	if message != "" {
		fail(message)
		return
	}

	info.ServerURL = publicURL
	root := document.Content[0]
	setSettingsValue(root, "server_url", settingsString(publicURL))

	admin := settingsMappingValue(root, "web_admin")
	if admin == nil || admin.Kind != yaml.MappingNode {
		//nolint:misspell // German user-facing text.
		fail("Die Web-Admin-Konfiguration fehlt oder ist ungültig.")
		return
	}

	setSettingsValue(admin, "enabled", settingsBool(true))
	setSettingsValue(admin, "setup_enabled", settingsBool(true))

	if info.Issuer != "" {
		oidc := settingsMappingValue(root, "oidc")
		if oidc == nil || oidc.Tag == "!!null" {
			oidc = settingsMapping()
			setSettingsValue(root, "oidc", oidc)
		}

		if oidc.Kind != yaml.MappingNode {
			//nolint:misspell // German user-facing text.
			fail("Ungültige OIDC-Konfiguration.")
			return
		}

		setSettingsValue(oidc, "issuer", settingsString(info.Issuer))
		setSettingsValue(oidc, "client_id", settingsString(info.ClientID))

		if secret != "" {
			setSettingsValue(oidc, "client_secret", settingsString(secret))
			removeSetupValue(oidc, "client_secret_path")
		}
	} else {
		removeSetupValue(root, "oidc")
	}

	err = h.database.CreateInitialWebAdmin(info.Username, password, func() error { return writeSettingsConfig(path, document) })
	if err != nil {
		if errors.Is(err, db.ErrInitialSetupClosed) {
			http.Error(w, "Einrichtung bereits geschlossen.", http.StatusGone)
			return
		}

		if errors.Is(err, db.ErrWebAdminUsernameInvalid) {
			fail("Wähle einen gültigen Admin-Benutzernamen.")
			return
		}

		log.Error().Err(err).Msg("initial setup could not commit configuration and admin")
		fail("Speichern fehlgeschlagen. Es wurde kein Admin-Konto angelegt; du kannst es erneut versuchen.")

		return
	}
	// The account is now committed. Failure of this final marker cannot lock out
	// the owner: any existing admin independently prevents setup on the next boot.
	setSettingsValue(admin, "setup_enabled", settingsBool(false))

	markerErr := writeSettingsConfig(path, document)
	if markerErr != nil {
		log.Error().Err(markerErr).Msg("initial setup completion marker could not be written")
	}

	h.completed = true

	//nolint:gosec // G124: Cookie removal must also work for the HTTP LAN setup.
	http.SetCookie(w, &http.Cookie{Name: setupCookie, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", Path: "/setup", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	writeHTML(w, http.StatusOK, templates.InitialSetupDone().Render())
	close(h.done)
}

func validateSetupServerURL(raw, baseDomain string) (string, error) {
	if len(raw) > 2048 {
		return "", errSetupURL
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return "", errSetupURL
	}

	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", errSetupURL
		}
	}

	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))

	baseDomain = strings.ToLower(strings.TrimSuffix(baseDomain, "."))
	if baseDomain != "" && (host == baseDomain || strings.HasSuffix(host, "."+baseDomain)) {
		return "", errSetupURL
	}

	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		return "", errSetupURL
	}

	parsed.Path = ""

	return parsed.String(), nil
}

func checkSetupOIDC(ctx context.Context, issuer string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(issuer, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return errSetupOIDC
		}

		return validateSettingsIssuer(r.URL.String())
	}}

	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return errSetupOIDC
	}

	var metadata struct {
		Issuer        string `json:"issuer"`
		Authorization string `json:"authorization_endpoint"`
		Token         string `json:"token_endpoint"`
		Keys          string `json:"jwks_uri"`
	}

	err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&metadata)
	if err != nil {
		return err
	}

	if metadata.Issuer != issuer {
		return errSetupOIDC
	}

	for _, endpoint := range []string{metadata.Authorization, metadata.Token, metadata.Keys} {
		if validateSettingsIssuer(endpoint) != nil {
			return errSetupOIDC
		}
	}

	return nil
}

func settingsBool(value bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(value)}
}

func settingsMapping() *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

func removeSetupValue(mapping *yaml.Node, key string) {
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			mapping.Content = append(mapping.Content[:index], mapping.Content[index+2:]...)
			return
		}
	}
}

func validateSetupInput(ctx context.Context, info templates.SetupInfo, password, confirm, secret string, saved savedOIDCSettings, baseDomain string) (string, string) {
	publicURL, err := validateSetupServerURL(info.ServerURL, baseDomain)
	if err != nil {
		return "", "Trage eine gültige HTTP- oder HTTPS-Server-URL ohne Zugangsdaten, Pfad, Query oder Fragment ein. Der Host darf nicht innerhalb der MagicDNS-Domain liegen."
	}

	if password != confirm || len([]rune(password)) < db.WebAdminMinPasswordLength || len(password) > 1024 {
		return "", "Die Passwörter müssen übereinstimmen und mindestens 12 Zeichen lang sein."
	}

	if info.Issuer == "" {
		if info.ClientID != "" || secret != "" {
			return "", "Für OIDC werden Issuer-URL, Client-ID und Client-Secret gemeinsam benötigt."
		}

		return publicURL, ""
	}

	if validateSettingsIssuer(info.Issuer) != nil || info.ClientID == "" || len(info.ClientID) > 512 || (secret == "" && saved.Secret == "") {
		return "", "Gib eine gültige Issuer-URL, Client-ID und ein Client-Secret ein oder lasse alle OIDC-Felder leer."
	}

	err = checkSetupOIDC(ctx, info.Issuer)
	if err != nil {
		//nolint:misspell // German user-facing text.
		return "", "Der OIDC-Anbieter ist nicht erreichbar oder liefert keine passende OpenID-Konfiguration. Prüfe die Issuer-URL oder richte OIDC später ein."
	}

	return publicURL, ""
}
