package webui

import (
	"bytes"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

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
	h.settingsMu.Lock()
	defer h.settingsMu.Unlock()

	settings := templates.AdminOIDCSettings{
		Issuer: h.activeOIDC.Issuer, ClientID: h.activeOIDC.ClientID,
		HasSecret:   h.activeOIDC.ClientSecret != "",
		CallbackURL: h.serverURL + "/oidc/callback",
	}

	_, _, saved, err := readOIDCSettings(h.configPath)
	if err == nil {
		settings.Issuer = saved.Issuer
		settings.ClientID = saved.ClientID
		settings.HasSecret = saved.Secret != ""
		settings.Writable = true
		settings.PendingRestart = saved.Issuer != h.activeOIDC.Issuer ||
			saved.ClientID != h.activeOIDC.ClientID || saved.Secret != h.activeOIDC.ClientSecret
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
