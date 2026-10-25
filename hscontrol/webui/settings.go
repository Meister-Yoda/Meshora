package webui

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/juanfont/headscale/hscontrol/state"
	"github.com/juanfont/headscale/hscontrol/templates"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/rs/zerolog/log"
	"github.com/tailscale/hujson"
	"gopkg.in/yaml.v3"
)

const settingsNullTag = "!!null"

var (
	errSettingsWebAdmin   = errors.New("invalid web_admin configuration")
	errSettingsAdminOIDC  = errors.New("invalid admin OIDC configuration")
	errSettingsRolesClaim = errors.New("invalid roles claim name")

	errPolicyDisplayObject   = errors.New("policy display requires an object")
	errPolicyDisplayKey      = errors.New("policy display key must be a string")
	errPolicyDisplayTrailing = errors.New("policy display has trailing content")
	errSettingsConfig        = errors.New("settings require a YAML configuration mapping")
	errSettingsIssuer        = errors.New("issuer must be an HTTPS URL, or HTTP on a loopback address, without credentials, query or fragment")
)

type savedOIDCSettings struct {
	AllowedGroups []string `yaml:"allowed_groups"`
	Issuer        string   `yaml:"issuer"`
	ClientID      string   `yaml:"client_id"`
	Secret        string   `json:"-"              yaml:"client_secret"`
	SecretPath    string   `json:"-"              yaml:"client_secret_path"`
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
		UserGroups:       strings.Join(h.activeOIDC.AllowedGroups, ", "),
		AdminGroups:      strings.Join(h.adminOIDCGroups, ", "),
		AdminRoles:       strings.Join(h.adminOIDCRoles, ", "),
		RolesClaim:       h.adminOIDCRolesClaim,
		OIDCButtonText:   h.oidcButtonText,
	}

	_, document, saved, err := readOIDCSettings(h.configPath)
	if err == nil {
		settings.UserGroups = strings.Join(saved.AllowedGroups, ", ")
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

		settings.PendingRestart = settings.UserGroups != strings.Join(h.activeOIDC.AllowedGroups, ", ") ||
			saved.Issuer != h.activeOIDC.Issuer ||
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
	if oidc == nil || oidc.Tag == settingsNullTag {
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

	// Provider-only forms omit user_groups and must retain access restrictions.
	if r.PostForm.Has("user_groups") {
		groups := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}

		for group := range strings.SplitSeq(formValue(r, "user_groups"), ",") {
			if group = strings.TrimSpace(group); group != "" {
				groups.Content = append(groups.Content, settingsString(group))
			}
		}

		setSettingsValue(oidc, "allowed_groups", groups)
	}

	// Omitted fields retain the existing access restrictions.
	err = updateAdminOIDCSettings(root, r.PostForm)
	if err != nil {
		switch {
		case errors.Is(err, errSettingsWebAdmin):
			h.redirect(w, r, "/settings", failure("Invalid web_admin configuration."))
		case errors.Is(err, errSettingsAdminOIDC):
			h.redirect(w, r, "/settings", failure("Invalid admin OIDC configuration."))
		default:
			h.redirect(w, r, "/settings", failure("Enter a valid roles claim name (maximum 128 characters)."))
		}

		return
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

	if oidc := settingsMappingValue(document.Content[0], "oidc"); oidc != nil && oidc.Tag != settingsNullTag {
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
	if admin == nil || admin.Tag == settingsNullTag {
		admin = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setSettingsValue(root, "web_admin", admin)
	}

	if admin.Kind != yaml.MappingNode {
		h.redirect(w, r, "/settings", failure("Invalid web_admin configuration."))
		return
	}

	provider := settingsMappingValue(admin, "oidc")
	if provider == nil || provider.Tag == settingsNullTag {
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

// savedDNSSettings mirrors only editable YAML fields. All other nodes remain
// in the syntax tree and are never decoded/re-encoded as part of this struct.
type savedDNSSettings struct {
	MagicDNS         bool   `yaml:"magic_dns"`
	BaseDomain       string `yaml:"base_domain"`
	OverrideLocalDNS bool   `yaml:"override_local_dns"`
	Nameservers      struct {
		Global []string            `yaml:"global"`
		Split  map[string][]string `yaml:"split"`
	} `yaml:"nameservers"`
	SearchDomains []string `yaml:"search_domains"`
}

func dnsEnvironmentOverrides() []string {
	var keys []string

	for _, value := range os.Environ() {
		key, val, _ := strings.Cut(value, "=")
		if strings.HasPrefix(key, "HEADSCALE_DNS_") && val != "" {
			keys = append(keys, key)
		}
	}

	slices.Sort(keys)

	return keys
}

// readDNSSettings rejects ambiguous editable YAML instead of flattening aliases,
// merges, duplicate keys or extra documents and silently losing information.
func readDNSSettings(path string) (string, *yaml.Node, savedDNSSettings, string, error) {
	saved := savedDNSSettings{MagicDNS: true, OverrideLocalDNS: true}
	if path == "" {
		return "", nil, saved, "", errSettingsConfig
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, saved, "", err
	}

	ext := strings.ToLower(filepath.Ext(resolved))
	if ext != ".yaml" && ext != ".yml" {
		return "", nil, saved, "", errSettingsConfig
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", nil, saved, "", err
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))

	var doc yaml.Node

	err = decoder.Decode(&doc)
	if err != nil || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return "", nil, saved, "", errSettingsConfig
	}

	var extra yaml.Node
	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		return "", nil, saved, "", errSettingsConfig
	}
	// Decode validates duplicate keys throughout the document without modifying it.
	var check map[string]any
	if doc.Decode(&check) != nil {
		return "", nil, saved, "", errSettingsConfig
	}

	root := doc.Content[0]
	if settingsMappingValue(root, "<<") != nil {
		return "", nil, saved, "", errSettingsConfig
	}

	dns := settingsMappingValue(root, "dns")
	if dns != nil && dns.Tag != settingsNullTag {
		if dns.Kind != yaml.MappingNode || !dnsEditableYAML(dns) || dns.Decode(&saved) != nil {
			return "", nil, saved, "", errSettingsConfig
		}

		ns := settingsMappingValue(dns, "nameservers")
		if ns != nil && ns.Tag != settingsNullTag && ns.Kind != yaml.MappingNode {
			return "", nil, saved, "", errSettingsConfig
		}
	}

	return resolved, &doc, saved, hashToken(string(data)), nil
}

func dnsEditableYAML(n *yaml.Node) bool {
	if n.Kind == yaml.AliasNode || n.Anchor != "" || n.Tag == "!!merge" {
		return false
	}

	for _, child := range n.Content {
		if !dnsEditableYAML(child) {
			return false
		}
	}

	return true
}

func dnsDisplay(s savedDNSSettings) templates.AdminDNSValues {
	split := "{}"

	if len(s.Nameservers.Split) > 0 {
		data, err := yaml.Marshal(s.Nameservers.Split)
		if err == nil {
			split = strings.TrimSpace(string(data))
		}
	}

	return templates.AdminDNSValues{
		MagicDNS: s.MagicDNS, BaseDomain: s.BaseDomain, OverrideLocalDNS: s.OverrideLocalDNS,
		Global: strings.Join(s.Nameservers.Global, "\n"), Search: strings.Join(s.SearchDomains, "\n"), Split: split,
	}
}

func dnsFromConfig(c types.DNSConfig) savedDNSSettings {
	s := savedDNSSettings{MagicDNS: c.MagicDNS, BaseDomain: c.BaseDomain, OverrideLocalDNS: c.OverrideLocalDNS, SearchDomains: c.SearchDomains}
	s.Nameservers.Global = c.Nameservers.Global
	s.Nameservers.Split = c.Nameservers.Split

	return s
}

func (h *Handler) dnsSettings(w http.ResponseWriter, r *http.Request) {
	h.settingsMu.Lock()
	defer h.settingsMu.Unlock()

	h.renderDNSSettings(w, r, http.StatusOK, nil, "")
}

func (h *Handler) renderDNSSettings(w http.ResponseWriter, r *http.Request, status int, attempted *templates.AdminDNSValues, message string) {
	active := dnsDisplay(dnsFromConfig(h.runtimeConfig.DNSConfig))
	page := templates.AdminDNSSettings{Active: active, Error: message, Overrides: dnsEnvironmentOverrides()}
	// This is the shared config used by the mapper, not a reload of the YAML file.
	effective := h.runtimeConfig.CloneTailcfgDNSConfig()
	if effective != nil {
		page.EffectiveAvailable = true
		page.EffectiveMagic = effective.Proxied
		page.EffectiveSearch = effective.Domains

		page.ExtraRecordCount = len(effective.ExtraRecords)
		for _, resolver := range effective.Resolvers {
			if resolver != nil {
				page.EffectiveResolvers = append(page.EffectiveResolvers, resolver.Addr)
			}
		}

		for _, resolver := range effective.FallbackResolvers {
			if resolver != nil {
				page.EffectiveFallback = append(page.EffectiveFallback, resolver.Addr)
			}
		}

		for domain, resolvers := range effective.Routes {
			var addresses []string

			for _, resolver := range resolvers {
				if resolver != nil {
					addresses = append(addresses, resolver.Addr)
				}
			}

			page.EffectiveRoutes = append(page.EffectiveRoutes, templates.AdminDNSSplit{Domain: domain, Resolvers: strings.Join(addresses, "\n")})
		}

		slices.SortFunc(page.EffectiveRoutes, func(a, b templates.AdminDNSSplit) int { return strings.Compare(a.Domain, b.Domain) })
	}

	_, _, saved, revision, err := readDNSSettings(h.configPath)
	if err == nil {
		page.Saved = dnsDisplay(saved)
		page.Revision = revision
		page.PendingRestart = !reflect.DeepEqual(page.Saved, active)
		page.Writable = len(page.Overrides) == 0
		page.HasSaved = true
	} else {
		page.ReadOnlyReason = "The YAML file is unavailable or cannot be edited safely. Aliases, anchors, merge keys and multiple documents in editable DNS settings require editing the file directly."
	}

	if len(page.Overrides) > 0 {
		page.ReadOnlyReason = "DNS environment overrides take precedence over the YAML file. Remove them and restart Meshora before editing DNS here."
	}

	page.Form = page.Saved
	if attempted != nil {
		page.Form = *attempted
	}

	writeHTML(w, status, templates.AdminDNS(h.pageInfo(r), page).Render())
}

func dnsDomainValid(domain string) bool {
	domain = strings.TrimSuffix(domain, ".")
	if domain == "" || len(domain) > 253 {
		return false
	}

	for label := range strings.SplitSeq(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}

		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}

	return true
}

func dnsResolverValid(value string) bool {
	ip, err := netip.ParseAddr(value)
	if err == nil {
		return ip.Zone() == "" && !ip.IsUnspecified() && !ip.IsMulticast()
	}

	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return false
	}

	if !dnsDomainValid(u.Hostname()) {
		ip, err := netip.ParseAddr(u.Hostname())
		if err != nil || ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() {
			return false
		}
	}

	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}

	return !strings.ContainsAny(value, " \t\r\n")
}

func dnsLines(raw string) []string {
	var lines []string

	for line := range strings.SplitSeq(raw, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}

	return lines
}

func parseDNSForm(form url.Values, serverURL string) (savedDNSSettings, templates.AdminDNSValues, string) {
	values := templates.AdminDNSValues{
		MagicDNS: form.Get("magic_dns") == "true", BaseDomain: strings.TrimSpace(form.Get("base_domain")),
		OverrideLocalDNS: form.Get("override_local_dns") == "true",
		Global:           form.Get("global"), Search: form.Get("search"), Split: form.Get("split"),
	}
	saved := savedDNSSettings{MagicDNS: values.MagicDNS, BaseDomain: values.BaseDomain, OverrideLocalDNS: values.OverrideLocalDNS}

	for _, name := range []string{"magic_dns", "override_local_dns", "base_domain", "global", "search", "split"} {
		if !form.Has(name) || len(form[name]) != 1 {
			return saved, values, "The DNS form is incomplete or contains duplicate fields. Reload the page."
		}
	}

	for _, name := range []string{"magic_dns", "override_local_dns"} {
		if form.Get(name) != "true" && form.Get(name) != "false" {
			return saved, values, "Choose Yes or No for MagicDNS and local DNS override."
		}
	}

	if saved.MagicDNS && saved.BaseDomain == "" || saved.BaseDomain != "" && (!dnsDomainValid(saved.BaseDomain) || strings.HasSuffix(saved.BaseDomain, ".")) {
		return saved, values, "Enter a valid MagicDNS base domain without a trailing dot. A base domain is required when MagicDNS is enabled."
	}

	if saved.BaseDomain != "" {
		server, err := url.Parse(serverURL)
		if err != nil {
			return saved, values, "The server hostname must be outside the MagicDNS base domain."
		}

		host, domain := strings.ToLower(server.Hostname()), strings.ToLower(saved.BaseDomain)
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return saved, values, "The server hostname must be outside the MagicDNS base domain."
		}
	}

	saved.Nameservers.Global = dnsLines(values.Global)
	if saved.OverrideLocalDNS && len(saved.Nameservers.Global) == 0 {
		return saved, values, "At least one global nameserver is required when local DNS is overridden."
	}

	for _, ns := range saved.Nameservers.Global {
		if !dnsResolverValid(ns) {
			return saved, values, "Enter nameservers as IPv4, IPv6 or HTTPS DNS-over-HTTPS URLs, one per line."
		}
	}

	saved.SearchDomains = dnsLines(values.Search)
	for _, domain := range saved.SearchDomains {
		if !dnsDomainValid(domain) {
			return saved, values, "Enter valid search domains, one per line."
		}
	}

	split, validation := parseDNSSplit(values.Split)
	saved.Nameservers.Split = split

	return saved, values, validation
}

func parseDNSSplit(raw string) (map[string][]string, string) {
	var split map[string][]string
	// Split DNS uses a mapping so empty resolver lists remain distinguishable
	// from a missing domain. Empty lists mean local resolution on the client.
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "{}"
	}

	var document yaml.Node

	decoder := yaml.NewDecoder(strings.NewReader(raw))
	if decoder.Decode(&document) != nil || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode || !dnsEditableYAML(&document) {
		return nil, "Split DNS must be a YAML mapping from domains to resolver lists. Use [] for local resolution."
	}

	var extra yaml.Node
	if !errors.Is(decoder.Decode(&extra), io.EOF) || document.Decode(&split) != nil {
		return nil, "Split DNS must be a YAML mapping from domains to resolver lists. Use [] for local resolution."
	}

	seen := map[string]bool{}

	mapping := document.Content[0]
	for i := 0; i < len(mapping.Content); i += 2 {
		domain, resolvers := mapping.Content[i].Value, mapping.Content[i+1]

		key := strings.ToLower(strings.TrimSuffix(domain, "."))
		if !dnsDomainValid(domain) || seen[key] || domain != key || resolvers.Kind != yaml.SequenceNode {
			return nil, "Split DNS needs unique lowercase domains without trailing dots and resolver lists. Use [] for local resolution."
		}

		seen[key] = true

		for _, resolver := range resolvers.Content {
			if resolver.Tag != "!!str" || !dnsResolverValid(resolver.Value) {
				return nil, "Enter valid IP addresses or HTTPS DNS-over-HTTPS URLs in each Split DNS resolver list."
			}
		}
	}

	return split, ""
}

func (h *Handler) saveDNSSettings(w http.ResponseWriter, r *http.Request) {
	h.settingsMu.Lock()
	defer h.settingsMu.Unlock()

	if len(dnsEnvironmentOverrides()) > 0 {
		h.renderDNSSettings(w, r, http.StatusConflict, nil, "DNS environment overrides take precedence over the YAML file. Remove them and restart Meshora before editing DNS here.")
		return
	}

	path, document, before, revision, err := readDNSSettings(h.configPath)
	if err != nil {
		h.renderDNSSettings(w, r, http.StatusConflict, nil, "The DNS configuration cannot be edited safely. Nothing was saved.")
		return
	}

	if r.PostForm.Get("revision") != revision {
		h.renderDNSSettings(w, r, http.StatusConflict, nil, "The configuration file changed since this form was opened. Reload and review the current values before saving.")
		return
	}

	serverURL := h.serverURL
	if value := settingsMappingValue(document.Content[0], "server_url"); value != nil {
		// Validate against both running and next-start server URLs.
		serverURL = value.Value
	}

	saved, attempted, validation := parseDNSForm(r.PostForm, serverURL)
	if validation == "" {
		_, _, validation = parseDNSForm(r.PostForm, h.serverURL)
	}

	if validation != "" {
		h.renderDNSSettings(w, r, http.StatusUnprocessableEntity, &attempted, validation)
		return
	}

	err = updateDNSDocument(document, before, saved)
	if err == nil {
		// Recheck immediately before the atomic replacement. Dashboard writers are
		// serialized; external editors should use the same operational edit window.
		data, readErr := os.ReadFile(path)
		if readErr != nil || hashToken(string(data)) != revision {
			h.renderDNSSettings(w, r, http.StatusConflict, nil, "The configuration file changed since this form was opened. Reload and review the current values before saving.")
			return
		}

		err = writeSettingsConfig(path, document)
	}

	if err != nil {
		log.Error().Err(err).Msg("web admin: saving DNS configuration")
		h.renderDNSSettings(w, r, http.StatusInternalServerError, &attempted, "Saving DNS failed. Check file permissions. The previous configuration was retained.")

		return
	}

	log.Info().Str("admin", sessionFrom(r).username).Msg("web admin saved DNS configuration; restart required")
	h.redirect(w, r, "/dns", success("DNS settings saved. Restart Meshora to activate them. The running DNS configuration has not changed."))
}

// updateDNSDocument patches only DNS nodes; persistence and runtime state are separate.
func updateDNSDocument(document *yaml.Node, before, saved savedDNSSettings) error {
	var err error

	root := document.Content[0]

	dns := settingsMappingValue(root, "dns")
	if dns == nil || dns.Tag == settingsNullTag {
		dns = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setSettingsValue(root, "dns", dns)
	}
	// Patch only changed fields. Unchanged lists, comments, style and unknown
	// keys keep their original YAML nodes, including advanced DNS options.
	patch := func(node *yaml.Node, key string, old, value any) error {
		if reflect.DeepEqual(old, value) {
			return nil
		}

		var encoded yaml.Node

		err := encoded.Encode(value)
		if err != nil {
			return err
		}

		if existing := settingsMappingValue(node, key); existing != nil {
			encoded.FootComment = existing.FootComment
		}

		setSettingsValue(node, key, &encoded)

		return nil
	}
	for _, field := range []struct {
		key        string
		old, value any
	}{
		{"magic_dns", before.MagicDNS, saved.MagicDNS},
		{"base_domain", before.BaseDomain, saved.BaseDomain},
		{"override_local_dns", before.OverrideLocalDNS, saved.OverrideLocalDNS},
		{"search_domains", before.SearchDomains, saved.SearchDomains},
	} {
		err = patch(dns, field.key, field.old, field.value)
		if err != nil {
			break
		}
	}

	ns := settingsMappingValue(dns, "nameservers")
	if ns == nil || ns.Tag == settingsNullTag {
		ns = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setSettingsValue(dns, "nameservers", ns)
	}
	// nil and empty lists both load as empty, but explicit [] must be written
	// for edited lists: null resolver entries are dropped by client config clones.
	global := saved.Nameservers.Global
	if global == nil {
		global = []string{}
	}

	if err == nil && !slices.Equal(before.Nameservers.Global, global) {
		err = patch(ns, "global", nil, global)
	}

	if err == nil && !reflect.DeepEqual(before.Nameservers.Split, saved.Nameservers.Split) {
		split := settingsMappingValue(ns, "split")
		if split == nil || split.Tag == settingsNullTag {
			split = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setSettingsValue(ns, "split", split)
		}
		// Retain comments on unchanged split entries rather than replacing the map.
		for i := len(split.Content) - 2; i >= 0; i -= 2 {
			if _, keep := saved.Nameservers.Split[split.Content[i].Value]; !keep {
				split.Content = append(split.Content[:i], split.Content[i+2:]...)
			}
		}

		keys := make([]string, 0, len(saved.Nameservers.Split))
		for key := range saved.Nameservers.Split {
			keys = append(keys, key)
		}

		slices.Sort(keys)

		for _, key := range keys {
			value := saved.Nameservers.Split[key]
			if value == nil {
				value = []string{}
			}

			if old, exists := before.Nameservers.Split[key]; exists && slices.Equal(old, value) {
				continue
			}

			err = patch(split, key, nil, value)
			if err != nil {
				break
			}
		}
	}

	return err
}

const maxPolicyDisplayBytes = 1 << 20

func (h *Handler) policyPage(w http.ResponseWriter, r *http.Request) {
	page := templates.AdminPolicyInfo{Loaded: h.state.PolicySnapshot()}
	cfg := h.runtimeConfig

	var data []byte

	switch cfg.Policy.Mode {
	case types.PolicyModeFile:
		page.Source = "File"

		page.Reference = cfg.Policy.Path
		if cfg.Policy.Path != "" {
			path := cfg.Policy.Path
			if !filepath.IsAbs(path) {
				path = filepath.Join(filepath.Dir(cfg.ConfigFilePath), path)
			}

			page.Reference = path

			var err error

			data, err = readPolicyDisplayFile(path)
			if err != nil {
				page.Error = "The policy source could not be read completely. No content was silently truncated."
			}
		}
	case types.PolicyModeDB:
		page.Source = "Database"

		stored, err := h.state.GetPolicy()
		switch {
		case errors.Is(err, types.ErrPolicyNotFound):
		case err != nil:
			page.Error = "The policy source could not be read completely. No content was silently truncated."
		default:
			page.Reference = strconv.FormatUint(uint64(stored.ID), 10)
			data = []byte(stored.Data)
		}
	default:
		page.Source = "Unknown"
		page.Error = "The configured policy source mode is not supported by this view."
	}

	if len(data) > maxPolicyDisplayBytes {
		data = nil
		page.Error = "The policy exceeds the display size limit. Inspect the complete source directly; no partial policy is shown."
	}

	if page.Error == "" {
		page.Raw = string(data)
		page.Fingerprint = hashToken(string(data))

		page.Empty = len(bytes.TrimSpace(data)) == 0
		if !page.Empty {
			sections, err := policyDisplaySections(data)
			if err != nil {
				page.Error = "The source cannot be structured as a HuJSON policy object. The complete source remains available below."
			} else {
				page.Sections = sections
			}
		}
	}

	for _, node := range h.state.ListNodes().All() {
		if node.IsTagged() {
			page.Tagged = append(page.Tagged, templates.AdminPolicyTagged{ID: node.ID().Uint64(), Name: node.GivenName(), Tags: node.Tags().AsSlice()})
		}
	}

	slices.SortFunc(page.Tagged, func(a, b templates.AdminPolicyTagged) int { return cmp.Compare(a.Name, b.Name) })
	writeHTML(w, http.StatusOK, templates.AdminPolicy(h.pageInfo(r), page).Render())
}

func readPolicyDisplayFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return io.ReadAll(io.LimitReader(file, maxPolicyDisplayBytes+1))
}

// policyObjectFields preserves member order and duplicate keys. This is a display
// parser, not policy-engine validation; it never compiles or applies a policy.
func policyObjectFields(data []byte) ([]templates.AdminPolicyField, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))

	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}

	if token != json.Delim('{') {
		return nil, errPolicyDisplayObject
	}

	var fields []templates.AdminPolicyField

	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, err
		}

		key, ok := token.(string)
		if !ok {
			return nil, errPolicyDisplayKey
		}

		var raw json.RawMessage

		err = decoder.Decode(&raw)
		if err != nil {
			return nil, err
		}

		var pretty bytes.Buffer

		err = json.Indent(&pretty, raw, "", "  ")
		if err != nil {
			return nil, err
		}

		fields = append(fields, templates.AdminPolicyField{Key: key, Value: pretty.String()})
	}

	_, err = decoder.Token()
	if err != nil {
		return nil, err
	}

	_, err = decoder.Token()
	if !errors.Is(err, io.EOF) {
		return nil, errPolicyDisplayTrailing
	}

	return fields, nil
}

func policyDisplaySections(data []byte) ([]templates.AdminPolicySection, error) {
	standardized, err := hujson.Standardize(data)
	if err != nil {
		return nil, err
	}

	fields, err := policyObjectFields(standardized)
	if err != nil {
		return nil, err
	}

	sections := make([]templates.AdminPolicySection, 0, len(fields))
	for _, field := range fields {
		section := templates.AdminPolicySection{Key: field.Key, Value: field.Value}
		if strings.HasPrefix(field.Value, "{") {
			section.Fields, err = policyObjectFields([]byte(field.Value))
			if err != nil {
				return nil, err
			}
		} else if strings.HasPrefix(field.Value, "[") {
			var entries []json.RawMessage

			err = json.Unmarshal([]byte(field.Value), &entries)
			if err != nil {
				return nil, err
			}

			for _, entry := range entries {
				rule := templates.AdminPolicyEntry{}
				if bytes.HasPrefix(bytes.TrimSpace(entry), []byte("{")) {
					rule.Fields, err = policyObjectFields(entry)
					if err != nil {
						return nil, err
					}
				} else {
					var pretty bytes.Buffer

					err = json.Indent(&pretty, entry, "", "  ")
					if err != nil {
						return nil, err
					}

					rule.Value = pretty.String()
				}

				section.Entries = append(section.Entries, rule)
			}
		}

		sections = append(sections, section)
	}

	return sections, nil
}

type policyEditorDraft struct {
	Text, SourceRevision, Token string
	LoadedRevision              uint64
	ValidatedAt                 time.Time
}

func (h *Handler) policyEditor(w http.ResponseWriter, r *http.Request) {
	h.renderPolicyEditor(w, r, http.StatusOK, "", "", nil)
}

func (h *Handler) renderPolicyEditor(w http.ResponseWriter, r *http.Request, status int, text, message string, draft *policyEditorDraft, diagnostics ...string) {
	source, loaded, err := h.state.PolicyEditorState()

	page := templates.AdminPolicyEditorInfo{
		Source: source.Reference, SourceRevision: source.Revision, SourceText: source.Text,
		LoadedText: loaded.Text, LoadedHash: loaded.SHA256, LoadedRevision: loaded.Revision,
		LoadedAt: loaded.LoadedAt, Implicit: loaded.Implicit, Error: message,
		Text: text, Writable: err == nil, Pending: source.Text != loaded.Text,
	}
	if len(diagnostics) > 0 {
		page.Diagnostics = diagnostics[0]
	}

	if err != nil {
		page.Error = "The policy source is unavailable. No save or activation is possible."
	}

	if r.Method == http.MethodGet || strings.HasSuffix(r.URL.Path, "/activate") {
		page.Text = source.Text
		if page.Text == "" {
			page.Text = "{}"
		}
	}

	if draft != nil {
		page.Validated = true
		page.Token = draft.Token
		page.Preview = policyEditorPreview(source.Text, draft.Text)
	}

	writeHTML(w, status, templates.AdminPolicyEditor(h.pageInfo(r), page).Render())
}

func policyEditorPreview(before, after string) string {
	if before == after {
		return ""
	}

	oldLines, newLines := strings.Split(before, "\n"), strings.Split(after, "\n")

	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}

	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix && oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}

	var result strings.Builder
	result.WriteString("--- current source\n+++ draft\n")

	for _, line := range oldLines[:prefix] {
		result.WriteString(" " + line + "\n")
	}

	for _, line := range oldLines[prefix : len(oldLines)-suffix] {
		result.WriteString("-" + line + "\n")
	}

	for _, line := range newLines[prefix : len(newLines)-suffix] {
		result.WriteString("+" + line + "\n")
	}

	for _, line := range oldLines[len(oldLines)-suffix:] {
		result.WriteString(" " + line + "\n")
	}

	return result.String()
}

func (h *Handler) validatePolicyDraft(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	sess.mu.Lock()
	sess.policyDraft = nil
	sess.mu.Unlock()

	text := r.PostForm.Get("policy_text")

	revision, err := strconv.ParseUint(r.PostForm.Get("loaded_revision"), 10, 64)
	if err == nil {
		err = h.state.ValidatePolicyDraft(text, r.PostForm.Get("source_revision"), revision)
	}

	if err != nil {
		h.renderPolicyEditor(w, r, policyEditorErrorStatus(err), text, policyEditorErrorMessage(err), nil, err.Error())
		return
	}

	draft := &policyEditorDraft{Text: text, SourceRevision: r.PostForm.Get("source_revision"), LoadedRevision: revision, Token: randomToken(), ValidatedAt: time.Now()}

	sess.mu.Lock()
	sess.policyDraft = draft
	sess.mu.Unlock()
	h.renderPolicyEditor(w, r, http.StatusOK, text, "", draft)
}

func policyEditorErrorStatus(err error) int {
	if errors.Is(err, state.ErrPolicyEditorWrite) {
		return http.StatusInternalServerError
	}

	if errors.Is(err, state.ErrPolicyEditorConflict) {
		return http.StatusConflict
	}

	return http.StatusUnprocessableEntity
}

func policyEditorErrorMessage(err error) string {
	if errors.Is(err, state.ErrPolicyEditorConflict) {
		return "The source or loaded policy changed. Reload, review the differences and validate again."
	}

	if errors.Is(err, state.ErrPolicyEditorDuplicate) {
		return "Duplicate keys are ambiguous. Resolve them explicitly before validating again; nothing was normalized."
	}

	return "Policy validation failed. The active policy and source were not changed. Review the engine diagnostics below."
}

func (h *Handler) savePolicyDraft(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	sess.mu.Lock()
	draft := sess.policyDraft
	sess.mu.Unlock()

	text := r.PostForm.Get("policy_text")
	if draft == nil || !tokensEqual(draft.Token, r.PostForm.Get("validation_token")) || draft.Text != text || time.Since(draft.ValidatedAt) > 15*time.Minute {
		h.renderPolicyEditor(w, r, http.StatusConflict, text, "Validate this exact draft before saving. The validation is missing, expired or belongs to different text.", nil)
		return
	}

	_, err := h.state.SavePolicyDraft(draft.Text, draft.SourceRevision, draft.LoadedRevision)
	if err != nil {
		message := "Saving failed. The previous source and active policy were retained."
		if errors.Is(err, state.ErrPolicyEditorConflict) {
			message = policyEditorErrorMessage(err)
		}

		h.renderPolicyEditor(w, r, policyEditorErrorStatus(err), text, message, nil, err.Error())

		return
	}

	sess.mu.Lock()
	sess.policyDraft = nil
	sess.mu.Unlock()
	h.redirect(w, r, "/policy/edit", success("Policy source saved exactly as entered. It has not been activated."))
}

func (h *Handler) activatePolicySource(w http.ResponseWriter, r *http.Request) {
	revision, err := strconv.ParseUint(r.PostForm.Get("loaded_revision"), 10, 64)
	if err != nil {
		h.renderPolicyEditor(w, r, http.StatusConflict, "", "The source or loaded policy changed. Reload, review the differences and validate again.", nil)
		return
	}

	result, err := h.state.ActivatePolicySource(r.PostForm.Get("source_revision"), revision)
	// Follow-up failures can occur after the engine swap. Publish every returned
	// change even then; do not leave connected clients with an obsolete policy.
	h.change(result.Changes...)

	if err != nil {
		message := "Activation failed before the engine switch. The previously loaded policy is unchanged."
		if result.Applied {
			message = "The new policy is active, but follow-up work failed. This was not rolled back. Review route approvals and retry activation after resolving the underlying error."
		}

		if errors.Is(err, state.ErrPolicyEditorConflict) {
			message = policyEditorErrorMessage(err)
		}

		h.renderPolicyEditor(w, r, http.StatusConflict, "", message, nil, err.Error())

		return
	}

	h.redirect(w, r, "/policy/edit", success("The saved policy was activated. Client delivery and reachability are not confirmed."))
}

// updateAdminOIDCSettings updates only authorization fields present in the form.
func updateAdminOIDCSettings(root *yaml.Node, form url.Values) error {
	if form.Has("admin_groups") || form.Has("admin_roles") || form.Has("roles_claim") {
		webAdmin := settingsMappingValue(root, "web_admin")
		if webAdmin == nil || webAdmin.Tag == settingsNullTag {
			webAdmin = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setSettingsValue(root, "web_admin", webAdmin)
		}

		if webAdmin.Kind != yaml.MappingNode {
			return errSettingsWebAdmin
		}

		adminOIDC := settingsMappingValue(webAdmin, "oidc")
		if adminOIDC == nil || adminOIDC.Tag == settingsNullTag {
			adminOIDC = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setSettingsValue(webAdmin, "oidc", adminOIDC)
		}

		if adminOIDC.Kind != yaml.MappingNode {
			return errSettingsAdminOIDC
		}

		for formKey, yamlKey := range map[string]string{"admin_groups": "allowed_groups", "admin_roles": "allowed_roles"} {
			if form.Has(formKey) {
				values := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}

				for value := range strings.SplitSeq(strings.TrimSpace(form.Get(formKey)), ",") {
					if value = strings.TrimSpace(value); value != "" {
						values.Content = append(values.Content, settingsString(value))
					}
				}

				setSettingsValue(adminOIDC, yamlKey, values)
			}
		}

		if form.Has("roles_claim") {
			claim := cmp.Or(strings.TrimSpace(form.Get("roles_claim")), "roles")
			if len(claim) > 128 || strings.ContainsAny(claim, " \t\r\n") {
				return errSettingsRolesClaim
			}

			setSettingsValue(adminOIDC, "roles_claim", settingsString(claim))
		}
	}

	return nil
}
