package webui

import (
	"bytes"
	"cmp"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/juanfont/headscale/hscontrol/templates"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

var (
	errSettingsConfig = errors.New("settings require a YAML configuration mapping")
	errSettingsIssuer = errors.New("issuer must be an HTTPS URL, or HTTP on a loopback address, without credentials, query or fragment")
)

type savedOIDCSettings struct {
	Issuer     string `yaml:"issuer"`
	ClientID   string `yaml:"client_id"`
	Secret     string `json:"-"         yaml:"client_secret"`
	SecretPath string `json:"-"         yaml:"client_secret_path"`
}

func (h *Handler) settingsPage(w http.ResponseWriter, r *http.Request) {
	section := chi.URLParam(r, "section")
	if section == "callbacks" {
		http.Redirect(w, r, h.publicPath+"/settings/provider", http.StatusSeeOther)
		return
	}

	if section == "" {
		section = "provider"
	}

	switch section {
	case "provider", "access", "appearance":
	default:
		http.NotFound(w, r)
		return
	}
	h.settingsMu.Lock()
	defer h.settingsMu.Unlock()

	settings := templates.AdminOIDCSettings{
		Section: section,
		Issuer:  h.activeOIDC.Issuer, ClientID: h.activeOIDC.ClientID,
		HasSecret:        h.activeOIDC.ClientSecret != "",
		CallbackURL:      h.serverURL + "/oidc/callback",
		AdminCallbackURL: h.serverURL + "/admin/oidc/callback",
		AdminGroups:      strings.Join(h.adminOIDCGroups, ", "),
		AdminRoles:       strings.Join(h.adminOIDCRoles, ", "),
		RolesClaim:       h.adminOIDCRolesClaim,
		OIDCButtonText:   h.oidcButtonText,
	}

	_, document, saved, err := readOIDCSettings(h.configPath)
	if err == nil {
		settings.Issuer = saved.Issuer
		settings.ClientID = saved.ClientID
		settings.HasSecret = saved.Secret != ""
		settings.Writable = true
		settings.AdminGroups = ""
		settings.AdminRoles = ""
		settings.RolesClaim = "roles"

		if webAdmin := settingsMappingValue(document.Content[0], "web_admin"); webAdmin != nil && webAdmin.Kind == yaml.MappingNode {
			if adminOIDC := settingsMappingValue(webAdmin, "oidc"); adminOIDC != nil && adminOIDC.Kind == yaml.MappingNode {
				if roles := settingsMappingValue(adminOIDC, "allowed_roles"); roles != nil {
					var values []string
					if roles.Decode(&values) == nil {
						settings.AdminRoles = strings.Join(values, ", ")
					}
				}

				if claim := settingsMappingValue(adminOIDC, "roles_claim"); claim != nil && claim.Value != "" {
					settings.RolesClaim = claim.Value
				}

				if groups := settingsMappingValue(adminOIDC, "allowed_groups"); groups != nil {
					var values []string
					if groups.Decode(&values) == nil {
						settings.AdminGroups = strings.Join(values, ", ")
					}
				}
			}
		}
		settings.PendingRestart = saved.Issuer != h.activeOIDC.Issuer ||
			saved.ClientID != h.activeOIDC.ClientID || saved.Secret != h.activeOIDC.ClientSecret ||
			settings.AdminGroups != strings.Join(h.adminOIDCGroups, ", ") ||
			settings.AdminRoles != strings.Join(h.adminOIDCRoles, ", ") || settings.RolesClaim != cmp.Or(h.adminOIDCRolesClaim, "roles")
	}

	writeHTML(w, http.StatusOK, templates.AdminSettings(h.pageInfo(r), settings).Render())
}

func (h *Handler) saveOIDCSettings(w http.ResponseWriter, r *http.Request) {
	issuer := formValue(r, "issuer")

	err := validateSettingsIssuer(issuer)
	if err != nil {
		h.redirect(w, r, "/settings", failure("Ungültige Issuer-URL. Verwende eine HTTPS-Adresse ohne Zugangsdaten, Query oder Fragment."))
		return
	}

	clientID := formValue(r, "client_id")
	if clientID == "" || len(clientID) > 512 {
		h.redirect(w, r, "/settings", failure("Trage eine gültige Client-ID ein."))
		return
	}

	h.settingsMu.Lock()
	defer h.settingsMu.Unlock()

	path, document, saved, err := readOIDCSettings(h.configPath)
	if err != nil {
		h.redirect(w, r, "/settings", failure("Die YAML-Datei konnte nicht gelesen werden. Es wurde nichts gespeichert."))
		return
	}

	secret := r.PostForm.Get("client_secret")
	if secret == "" && saved.Secret == "" {
		h.redirect(w, r, "/settings", failure("Trage das Client-Secret ein."))
		return
	}

	root := document.Content[0]

	oidc := settingsMappingValue(root, "oidc")
	if oidc == nil || oidc.Tag == "!!null" {
		oidc = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setSettingsValue(root, "oidc", oidc)
	}

	setSettingsValue(oidc, "issuer", settingsString(issuer))
	setSettingsValue(oidc, "client_id", settingsString(clientID))

	if secret != "" {
		setSettingsValue(oidc, "client_secret", settingsString(secret))

		for i := 0; i < len(oidc.Content); i += 2 {
			if oidc.Content[i].Value == "client_secret_path" {
				oidc.Content = append(oidc.Content[:i], oidc.Content[i+2:]...)
				break
			}
		}
	}

	if settingsMappingValue(oidc, "scope") == nil {
		scope := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, name := range []string{"openid", "profile", "email"} {
			scope.Content = append(scope.Content, settingsString(name))
		}

		setSettingsValue(oidc, "scope", scope)
	}

	// Omitted fields preserve authorization settings from older clients.
	if r.PostForm.Has("admin_groups") || r.PostForm.Has("admin_roles") || r.PostForm.Has("roles_claim") {
		webAdmin := settingsMappingValue(root, "web_admin")
		if webAdmin == nil || webAdmin.Tag == "!!null" {
			webAdmin = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setSettingsValue(root, "web_admin", webAdmin)
		}

		if webAdmin.Kind != yaml.MappingNode {
			h.redirect(w, r, "/settings", failure("Invalid web_admin configuration."))
			return
		}

		adminOIDC := settingsMappingValue(webAdmin, "oidc")
		if adminOIDC == nil || adminOIDC.Tag == "!!null" {
			adminOIDC = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setSettingsValue(webAdmin, "oidc", adminOIDC)
		}

		if adminOIDC.Kind != yaml.MappingNode {
			h.redirect(w, r, "/settings", failure("Invalid admin OIDC configuration."))
			return
		}

		for formKey, yamlKey := range map[string]string{"admin_groups": "allowed_groups", "admin_roles": "allowed_roles"} {
			if r.PostForm.Has(formKey) {
				values := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}

				for value := range strings.SplitSeq(formValue(r, formKey), ",") {
					if value = strings.TrimSpace(value); value != "" {
						values.Content = append(values.Content, settingsString(value))
					}
				}

				setSettingsValue(adminOIDC, yamlKey, values)
			}
		}

		if r.PostForm.Has("roles_claim") {
			claim := cmp.Or(formValue(r, "roles_claim"), "roles")
			if len(claim) > 128 || strings.ContainsAny(claim, " \t\r\n") {
				h.redirect(w, r, "/settings", failure("Enter a valid roles claim name (maximum 128 characters)."))
				return
			}

			setSettingsValue(adminOIDC, "roles_claim", settingsString(claim))
		}
	}

	err = writeSettingsConfig(path, document)
	if err != nil {
		// Never log submitted values or an encoded config, which contains secrets.
		log.Error().Err(err).Msg("web admin: saving OIDC configuration")
		h.redirect(w, r, "/settings", failure("Speichern fehlgeschlagen. Prüfe die Schreibrechte der YAML-Datei. Die bisherige Datei bleibt erhalten."))

		return
	}

	log.Info().Str("admin", sessionFrom(r).username).Msg("web admin updated OIDC settings; restart required")
	h.redirect(w, r, "/settings", success("Gespeichert. Starte Meshora neu, um die Authentik-Anmeldung zu aktivieren."))
}

func validateSettingsIssuer(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return errSettingsIssuer
	}

	loopback := u.Hostname() == "localhost"
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		loopback = ip.IsLoopback()
	}

	if u.Scheme != "https" && (u.Scheme != "http" || !loopback) {
		return errSettingsIssuer
	}

	return nil
}

// readOIDCSettings reads the current file on every request so unrelated edits
// and advanced OIDC options are preserved. No global Viper state is mutated.
func readOIDCSettings(path string) (string, *yaml.Node, savedOIDCSettings, error) {
	var saved savedOIDCSettings
	if path == "" {
		return "", nil, saved, errSettingsConfig
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, saved, err
	}

	if ext := strings.ToLower(filepath.Ext(resolved)); ext != ".yaml" && ext != ".yml" {
		return "", nil, saved, errSettingsConfig
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", nil, saved, err
	}

	var document yaml.Node

	err = yaml.Unmarshal(data, &document)
	if err != nil || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return "", nil, saved, errSettingsConfig
	}

	if oidc := settingsMappingValue(document.Content[0], "oidc"); oidc != nil && oidc.Tag != "!!null" {
		if oidc.Kind != yaml.MappingNode {
			return "", nil, saved, errSettingsConfig
		}

		err = oidc.Decode(&saved)
		if err != nil {
			return "", nil, savedOIDCSettings{}, errSettingsConfig
		}
	}

	if saved.SecretPath != "" && saved.Secret == "" {
		secretPath := os.ExpandEnv(saved.SecretPath)
		if !filepath.IsAbs(secretPath) {
			secretPath = filepath.Join(filepath.Dir(resolved), secretPath)
		}

		secret, readErr := os.ReadFile(secretPath)
		if readErr == nil {
			saved.Secret = strings.TrimSpace(string(secret))
		}
	}

	return resolved, &document, saved, nil
}

func settingsMappingValue(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}

	return nil
}

func settingsString(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func setSettingsValue(node *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			value.HeadComment = node.Content[i+1].HeadComment
			value.LineComment = node.Content[i+1].LineComment
			node.Content[i+1] = value

			return
		}
	}

	node.Content = append(node.Content, settingsString(key), value)
}

// writeSettingsConfig atomically replaces the YAML file with owner-only
// permissions. A failed encode/write leaves the original config untouched.
func writeSettingsConfig(path string, document *yaml.Node) error {
	var buffer bytes.Buffer

	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)

	err := encoder.Encode(document)
	if err != nil {
		return err
	}

	err = encoder.Close()
	if err != nil {
		return err
	}

	file, err := os.CreateTemp(filepath.Dir(path), ".meshora-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()

	_, err = file.Write(buffer.Bytes())
	if err != nil {
		return err
	}

	err = file.Sync()
	if err != nil {
		return err
	}

	err = file.Close()
	if err != nil {
		return err
	}

	return os.Rename(file.Name(), path)
}

// saveOIDCButtonText persists presentation independently of provider credentials.
// requireSession enforces admin authentication and CSRF on this POST.
func (h *Handler) saveOIDCButtonText(w http.ResponseWriter, r *http.Request) {
	text := strings.TrimSpace(r.PostForm.Get("button_text"))
	if len([]rune(text)) > 100 || strings.ContainsAny(text, "\r\n\t") {
		h.redirect(w, r, "/settings", failure("Der Button-Text darf maximal 100 Zeichen und keine Zeilenumbrueche enthalten."))
		return
	}

	h.settingsMu.Lock()
	defer h.settingsMu.Unlock()

	path, document, _, err := readOIDCSettings(h.configPath)
	if err != nil {
		h.redirect(w, r, "/settings", failure("Die YAML-Configuration konnte nicht gelesen werden."))
		return
	}

	root := document.Content[0]

	admin := settingsMappingValue(root, "web_admin")
	if admin == nil || admin.Tag == "!!null" {
		admin = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setSettingsValue(root, "web_admin", admin)
	}

	if admin.Kind != yaml.MappingNode {
		h.redirect(w, r, "/settings", failure("Invalid web_admin configuration."))
		return
	}

	provider := settingsMappingValue(admin, "oidc")
	if provider == nil || provider.Tag == "!!null" {
		provider = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setSettingsValue(admin, "oidc", provider)
	}

	if provider.Kind != yaml.MappingNode {
		h.redirect(w, r, "/settings", failure("Invalid admin OIDC configuration."))
		return
	}

	setSettingsValue(provider, "button_text", settingsString(text))

	err = writeSettingsConfig(path, document)
	if err != nil {
		log.Error().Err(err).Msg("web admin: saving OIDC button text")
		h.redirect(w, r, "/settings", failure("Speichern fehlgeschlagen. Pruefe die Schreibrechte der YAML-Datei."))

		return
	}

	h.oidcButtonText = text
	h.redirect(w, r, "/settings", success("Button-Text gespeichert. Die Aenderung ist sofort aktiv."))
}
