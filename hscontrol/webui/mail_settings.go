package webui

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/juanfont/headscale/hscontrol/templates"
	"gopkg.in/yaml.v3"
)

func (h *Handler) mailSettingsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", adminCSP+"; script-src 'self'")
	h.settingsMu.Lock()
	cfg := h.mailConfig
	_, document, _, err := readOIDCSettings(h.configPath)
	writable := err == nil && document != nil
	h.settingsMu.Unlock()

	proof, errProof := h.adminUserSecurity(r, sessionFrom(r).userID)
	if errProof != nil {
		http.Error(w, "Einstellungen nicht verfügbar", http.StatusInternalServerError)
		return
	}

	data := templates.MailSettings{Host: cfg.Host, Port: cfg.Port, Username: cfg.Username, From: cfg.From, TLSMode: cfg.TLSMode, HasPassword: cfg.Password != "" || cfg.PasswordPath != "", Writable: writable, Configured: validMailConfig(cfg), Transport: cfg.Transport, JMAPSessionURL: cfg.JMAPSessionURL, JMAPAccountID: cfg.JMAPAccountID, JMAPIdentityID: cfg.JMAPIdentityID, JMAPUsername: cfg.JMAPUsername, HasJMAPToken: cfg.JMAPToken != "" || cfg.JMAPTokenPath != ""}
	if data.Port == 0 {
		data.Port = 587
	}

	if data.TLSMode == "" {
		data.TLSMode = "starttls"
	}

	writeHTML(w, http.StatusOK, templates.MailSettingsPage(h.pageInfo(r), data, proof).Render())
}

func (h *Handler) saveMailSettings(w http.ResponseWriter, r *http.Request) {
	proofErr := h.emailAccountProof(r)
	if proofErr != nil {
		h.redirect(w, r, "/settings/mail", failure("Eigene Anmeldung bitte zuerst bestätigen."))
		return
	}

	h.settingsMu.Lock()
	defer h.settingsMu.Unlock()

	cfg := h.mailConfig

	cfg.Transport = r.PostForm.Get("transport")
	if cfg.Transport == "" {
		cfg.Transport = "smtp"
	}

	cfg.JMAPSessionURL = strings.TrimSpace(r.PostForm.Get("jmap_session_url"))
	cfg.JMAPAccountID = strings.TrimSpace(r.PostForm.Get("jmap_account_id"))
	cfg.JMAPIdentityID = strings.TrimSpace(r.PostForm.Get("jmap_identity_id"))

	cfg.JMAPUsername = strings.TrimSpace(r.PostForm.Get("jmap_username"))
	if token := r.PostForm.Get("jmap_token"); token != "" {
		if len(token) > 4096 {
			h.redirect(w, r, "/settings/mail", failure("JMAP-Zugangsdaten sind zu lang."))
			return
		}

		cfg.JMAPToken, cfg.JMAPTokenPath = token, ""
	}

	port, err := strconv.Atoi(r.PostForm.Get("port"))
	if err != nil && cfg.Transport == "jmap" {
		port = 587
		err = nil
	}

	if err != nil {
		h.redirect(w, r, "/settings/mail", failure("Gueltigen SMTP-Port eingeben."))
		return
	}

	cfg.Host = strings.TrimSpace(r.PostForm.Get("host"))
	cfg.Port = port
	cfg.Username = strings.TrimSpace(r.PostForm.Get("username"))
	cfg.From = strings.TrimSpace(r.PostForm.Get("from"))

	cfg.TLSMode = r.PostForm.Get("tls_mode")
	if password := r.PostForm.Get("password"); password != "" {
		if len(password) > 4096 {
			h.redirect(w, r, "/settings/mail", failure("SMTP-Passwort ist zu lang."))
			return
		}

		cfg.Password, cfg.PasswordPath = password, ""
	}

	if !validMailConfig(cfg) {
		h.redirect(w, r, "/settings/mail", failure("Einstellungen der gewählten Versandart und Absender prüfen."))
		return
	}

	path, document, _, err := readOIDCSettings(h.configPath)
	if err != nil {
		h.redirect(w, r, "/settings/mail", failure("Konfigurationsdatei nicht verfügbar."))
		return
	}

	mail := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, pair := range [][2]string{{"transport", cfg.Transport}, {"jmap_session_url", cfg.JMAPSessionURL}, {"jmap_account_id", cfg.JMAPAccountID}, {"jmap_identity_id", cfg.JMAPIdentityID}, {"jmap_username", cfg.JMAPUsername}, {"host", cfg.Host}, {"username", cfg.Username}, {"from", cfg.From}, {"tls_mode", cfg.TLSMode}} {
		setSettingsValue(mail, pair[0], settingsString(pair[1]))
	}

	setSettingsValue(mail, "port", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(cfg.Port)})

	if cfg.PasswordPath != "" {
		setSettingsValue(mail, "password_path", settingsString(cfg.PasswordPath))
	} else if cfg.Password != "" {
		setSettingsValue(mail, "password", settingsString(cfg.Password))
	}

	if cfg.JMAPTokenPath != "" {
		setSettingsValue(mail, "jmap_token_path", settingsString(cfg.JMAPTokenPath))
	} else if cfg.JMAPToken != "" {
		setSettingsValue(mail, "jmap_token", settingsString(cfg.JMAPToken))
	}

	setSettingsValue(document.Content[0], "mail", mail)

	err = writeSettingsConfig(path, document)
	if err != nil {
		h.redirect(w, r, "/settings/mail", failure("Mail-Einstellungen konnten nicht gespeichert werden."))
		return
	}

	h.mailConfig = cfg
	h.redirect(w, r, "/settings/mail", success("Mailversand gespeichert und sofort aktiv."))
}
