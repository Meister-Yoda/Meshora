package templates

import (
	"strconv"

	"github.com/chasefleming/elem-go"
	"github.com/chasefleming/elem-go/attrs"
)

const accountPasswordField = "password"

type MailSettings struct {
	Transport      string
	JMAPSessionURL string
	JMAPAccountID  string
	JMAPIdentityID string
	JMAPUsername   string
	HasJMAPToken   bool
	Host           string
	Port           int
	Username       string
	From           string
	TLSMode        string
	HasPassword    bool
	Writable       bool
	Configured     bool
}

func MailSettingsPage(info AdminPageInfo, cfg MailSettings, proof *AdminUserSecurity) *elem.Element {
	hint := "Noch nicht eingerichtet."
	if cfg.Configured {
		hint = "Mailversand ist konfiguriert. E-Mail-Änderungen werden erst nach Bestätigung wirksam."
	}

	var form elem.Node = elem.P(nil, elem.Text("Zum Speichern wird eine beschreibbare Konfigurationsdatei benötigt."))

	if cfg.Writable {
		hint := "SMTP-Passwort eingeben, falls der Server eine Anmeldung verlangt."
		if cfg.HasPassword {
			hint = "Ein Passwort ist gespeichert. Leer lassen, um es beizubehalten."
		}

		if cfg.Transport == "" {
			cfg.Transport = "smtp"
		}

		jmapHint := "API-Token eingeben. Mit Benutzername wird HTTP Basic verwendet, sonst Bearer."
		if cfg.HasJMAPToken {
			jmapHint = "JMAP-Zugangsdaten sind gespeichert. Leer lassen, um sie beizubehalten. Mit Benutzername wird HTTP Basic verwendet, sonst Bearer."
		}

		fields := []elem.Node{
			zoneSelect("Versandart", "transport", "mail-transport", []AdminChoice{{Value: "smtp", Label: "SMTP"}, {Value: "jmap", Label: "JMAP"}}, []string{cfg.Transport}, false),
			textField("Absenderadresse", "from", cfg.From, true),
			elem.H3(nil, elem.Text("JMAP")),
			elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Nur bei Versandart JMAP erforderlich. Session- und API-Adresse müssen HTTPS und denselben Host einschließlich Port verwenden. Ein beschreibbarer Entwürfe-Ordner und eine passende Absenderidentität werden benötigt.")),
			textField("JMAP-Session-URL", "jmap_session_url", cfg.JMAPSessionURL, false),
			textField("JMAP-Konto-ID (optional)", "jmap_account_id", cfg.JMAPAccountID, false),
			textField("JMAP-Identitäts-ID (optional)", "jmap_identity_id", cfg.JMAPIdentityID, false),
			textField("JMAP-Benutzername (optional)", "jmap_username", cfg.JMAPUsername, false),
			elem.Div(attrs.Props{attrs.Class: "field"}, elem.Label(attrs.Props{attrs.For: "jmap-token"}, elem.Text("JMAP-Token oder Passwort")), elem.Input(attrs.Props{attrs.ID: "jmap-token", attrs.Name: "jmap_token", attrs.Type: accountPasswordField, attrs.Autocomplete: "new-password", "aria-describedby": "jmap-token-hint"}), elem.P(attrs.Props{attrs.ID: "jmap-token-hint", attrs.Class: "muted"}, elem.Text(jmapHint))),
			elem.H3(nil, elem.Text("SMTP")),
			elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Nur bei Versandart SMTP erforderlich.")),
			textField("SMTP-Host", "host", cfg.Host, false),
			textField("SMTP-Port", "port", strconv.Itoa(cfg.Port), false),
			zoneSelect("Verschlüsselung", "tls_mode", "smtp-tls-mode", []AdminChoice{{Value: "starttls", Label: "STARTTLS (meist Port 587)"}, {Value: "tls", Label: "TLS (meist Port 465)"}}, []string{cfg.TLSMode}, false),
			textField("SMTP-Benutzername", "username", cfg.Username, false),
			elem.Div(attrs.Props{attrs.Class: "field"}, elem.Label(attrs.Props{attrs.For: "smtp-password"}, elem.Text("SMTP-Passwort")), elem.Input(attrs.Props{attrs.ID: "smtp-password", attrs.Name: accountPasswordField, attrs.Type: accountPasswordField, attrs.Autocomplete: "new-password", "aria-describedby": "smtp-password-hint"}), elem.P(attrs.Props{attrs.ID: "smtp-password-hint", attrs.Class: "muted"}, i18nText(hint))),
		}
		if proof.NeedPassword {
			fields = append(fields, securityProof(proof.NeedCode, "smtp-settings")...)
		}

		if proof.NeedOIDCLogin {
			fields = append(fields, elem.P(nil, elem.Text("Bitte zuerst erneut über OIDC anmelden.")))
		}

		fields = append(fields, submitButton("Mailversand speichern", "btn-primary"))
		form = postForm(info, "/settings/mail", "action-form", fields...)
	}

	return info.shell("Settings", pageHead("Mailversand", "Bestätigungslinks für E-Mail-Adressen versenden."),
		cardSection("Mailversand", hint, form))
}
