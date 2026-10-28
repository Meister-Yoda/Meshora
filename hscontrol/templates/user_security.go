package templates

import (
	"strconv"
	"strings"
	"time"

	"github.com/chasefleming/elem-go"
	"github.com/chasefleming/elem-go/attrs"
	"github.com/juanfont/headscale/hscontrol/types"
)

const (
	securityPrimaryButtonClass = "btn btn-primary"
	authPolicyCheckboxType     = "checkbox"
)

func securityProof(totp bool, suffix string) []elem.Node {
	fields := []elem.Node{elem.Div(attrs.Props{attrs.Class: "field"}, elem.Label(attrs.Props{attrs.For: "security-password-" + suffix}, elem.Text("Aktuelles Passwort")), elem.Input(attrs.Props{attrs.Type: "password", attrs.ID: "security-password-" + suffix, attrs.Name: "current_password", attrs.Autocomplete: "current-password", attrs.Required: "true"}))}
	if totp {
		fields = append(fields, securityCodeField("Authenticator- oder Wiederherstellungscode", suffix))
	}

	return fields
}

func securityCodeField(label, suffix string) elem.Node {
	return elem.Div(attrs.Props{attrs.Class: "field"}, elem.Label(attrs.Props{attrs.For: "security-code-" + suffix}, elem.Text(label)), elem.Input(attrs.Props{attrs.ID: "security-code-" + suffix, attrs.Name: "code", attrs.Type: "text", attrs.Autocomplete: "one-time-code", attrs.Required: "true", attrs.Spellcheck: "false"}))
}

func UserSecurityPage(info AdminPageInfo, hasPassword, totpEnabled, passkeysAvailable bool, keys []types.UserPasskey, policy *types.UserAuthPolicy, enrollmentOnly bool) *elem.Element {
	if enrollmentOnly {
		info.AccountRestricted = true
	}
	content := []elem.Node{backLink(info.RootPath("/account"), "Mein Konto"), pageHead("Sicherheit", "Schütze deine lokale Anmeldung mit einer Authenticator-App oder Passkeys.")}
	if enrollmentOnly {
		content = content[1:]
		content = append(content, cardSection("Sichere Anmeldung erforderlich", "Dein Administrator verlangt 2FA oder einen Passkey. Für die erstmalige Einrichtung kannst du nur dein Passwort und deine Anmeldesicherheit verwalten."))
	} else if policy.RequireStrongAuth {
		content = append(content, cardSection("Verpflichtende Anmeldung", "Für dieses Konto ist 2FA oder ein Passkey erforderlich."))
	}

	paused := policy.TOTPBypassedAt(time.Now())
	if paused {
		content = append(content, cardSection("Befristete Admin-Ausnahme", "Die 2FA-Prüfung ist bis "+policy.TOTPSuspendedUntil.UTC().Format(time.RFC3339)+" ausgesetzt. Verbinde deine Authenticator-App neu, um die Ausnahme vorzeitig zu beenden."))
	}

	if policy.PasskeysSuspendedAt(time.Now()) {
		content = append(content, cardSection("Passkey-Anmeldung pausiert", "Passkeys können bis "+policy.PasskeysSuspendedUntil.UTC().Format(time.RFC3339)+" nicht zur Anmeldung verwendet werden. Die gespeicherten Schlüssel bleiben erhalten."))
	}

	if !hasPassword {
		content = append(content, cardSection("Lokale Anmeldung", "Lege zuerst ein lokales Passwort fest, um 2FA und Passkeys zu verwalten.", linkButton(info.RootPath("/account/password"), "Passwort festlegen", "btn-primary")))
		return personalPage(info, "Sicherheit", content...)
	}

	totpHint := "Noch nicht aktiviert. Nach dem Passwort bestätigst du die Anmeldung mit einem sechsstelligen Code."
	action, label := "/account/security/totp/begin", "2FA einrichten"

	if totpEnabled {
		totpHint = "Aktiv. Halte deine Wiederherstellungscodes sicher bereit. Zum Ändern brauchst du dein Passwort und einen aktuellen Code."
		action, label = "/account/security/totp/disable", "2FA deaktivieren"
	}

	if paused && totpEnabled {
		totpHint = "Verbinde eine neue Authenticator-App und bestätige einen Code. Damit endet die Admin-Ausnahme sofort."
		action = "/account/security/totp/begin"
		label = "Authenticator-App neu verbinden"
	}

	fields := securityProof(totpEnabled && !paused, "totp")
	fields = append(fields, submitButton(label, "btn-secondary"))
	content = append(content, cardSection("Authenticator-App", totpHint, accountPostForm(info, action, fields...)))

	var rows []elem.Node

	for _, key := range keys {
		fields := securityProof(totpEnabled && !paused, "key-"+key.ID)
		fields = append(fields, elem.Input(attrs.Props{attrs.Type: inputTypeHidden, attrs.Name: "passkey_id", attrs.Value: key.ID}), submitButton("Passkey entfernen", "btn-secondary"))
		rows = append(rows, elem.Tr(nil, elem.Td(nil, elem.Text(key.Name)), elem.Td(nil, accountPostForm(info, "/account/security/passkeys/delete", fields...))))
	}

	passkeyNodes := []elem.Node{elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Passkeys melden dich ohne Passwort an. Dein Gerät bestätigt dich per PIN, Fingerabdruck oder Gesichtserkennung. Der private Schlüssel bleibt bei deinem Gerät oder Passkey-Anbieter."))}
	if len(rows) > 0 {
		passkeyNodes = append(passkeyNodes, dataTable("Gespeicherte Passkeys", []string{"Name", "Verwaltung"}, rows))
	} else {
		passkeyNodes = append(passkeyNodes, elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Noch keine Passkeys gespeichert.")))
	}

	if passkeysAvailable {
		fields := make([]elem.Node, 0, 5)
		fields = append(fields, textField("Name für diesen Passkey", "name", "", true))
		fields = append(fields, securityProof(totpEnabled && !paused, "register")...)
		fields = append(fields, elem.Button(attrs.Props{attrs.Type: submitType, attrs.Class: securityPrimaryButtonClass, attrs.Disabled: "true"}, elem.Text("Passkey hinzufügen")))
		passkeyNodes = append(passkeyNodes, elem.Form(attrs.Props{attrs.Method: "POST", attrs.Action: info.RootPath("/account/security/passkeys/begin"), attrs.Class: "action-form", "data-passkey-register": "", "data-finish": info.RootPath("/account/security/passkeys/finish")}, append([]elem.Node{adminCSRFInput(info.CSRFToken)}, fields...)...))
	} else {
		passkeyNodes = append(passkeyNodes, elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Passkeys benötigen HTTPS. Prüfe die öffentliche Serveradresse.")))
	}

	passkeyNodes = append(passkeyNodes, elem.P(attrs.Props{attrs.ID: "passkey-status", attrs.Class: "muted", attrs.Role: statusClass, "aria-live": "polite"}, elem.Text("Passkeys werden von modernen Browsern auf sicheren Verbindungen unterstützt.")))
	content = append(content, cardSection("Passkeys", "", passkeyNodes...), elem.Script(attrs.Props{attrs.Src: info.RootPath("/assets/security.js"), "defer": "true"}))

	return personalPage(info, "Sicherheit", content...)
}

func UserTOTPSetup(info AdminPageInfo, secret string) *elem.Element {
	return personalPage(info, "2FA einrichten", backLink(info.RootPath("/account/security"), "Sicherheit"), pageHead("Authenticator-App verbinden", "Scanne den QR-Code und bestätige danach den angezeigten Code. Erst dann wird 2FA aktiviert."),
		cardSection("1. In deiner Authenticator-App hinzufügen", "", elem.Img(attrs.Props{attrs.Src: info.RootPath("/account/security/totp/qr"), attrs.Alt: "QR-Code zum Einrichten deiner Authenticator-App", attrs.Width: "240", attrs.Height: "240", attrs.Class: "totp-qr"}), elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Alternativ diesen Einrichtungsschlüssel manuell eingeben:")), elem.Pre(attrs.Props{attrs.Class: "codeblock recovery-codes"}, elem.Code(attrs.Props{attrs.ID: "totp-secret"}, elem.Text(secret)))),
		cardSection("2. Verbindung bestätigen", "", accountPostForm(info, "/account/security/totp/enable", securityCodeField("Sechsstelliger Code", "setup"), submitButton("2FA aktivieren", "btn-primary"))))
}

func UserRecoveryCodes(base, login string, codes []string) *elem.Element {
	return adminDocument(base, "Wiederherstellungscodes · Meshora", "auth", elem.Main(attrs.Props{attrs.Class: "auth-card"}, brand(), elem.H1(nil, elem.Text("2FA ist aktiviert")), elem.P(nil, elem.Text("Speichere diese Wiederherstellungscodes jetzt an einem sicheren Ort. Jeder Code kann einmal statt eines Authenticator-Codes verwendet werden. Die Codes werden nur jetzt angezeigt.")), elem.Pre(attrs.Props{attrs.Class: "codeblock recovery-codes", attrs.ID: "recovery-codes"}, elem.Code(nil, elem.Text(strings.Join(codes, "\n")))), elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Bestehende Sitzungen wurden beendet. Melde dich mit Passwort und einem neuen Authenticator-Code an.")), linkButton(login+"?local=1", "Codes gespeichert – zur Anmeldung", "btn-primary")))
}

func UserSecondFactor(base, login, csrf, message string) *elem.Element {
	fields := []elem.Node{brand(), elem.H1(nil, elem.Text("Anmeldung bestätigen")), elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Gib einen aktuellen Code deiner Authenticator-App oder einen Wiederherstellungscode ein."))}
	if message != "" {
		fields = append(fields, elem.Div(attrs.Props{attrs.Class: "alert", attrs.Role: "alert"}, elem.Text(message)))
	}

	fields = append(fields, elem.Form(attrs.Props{attrs.Method: "POST", attrs.Action: login + "login/2fa", attrs.Class: "stack"}, adminCSRFInput(csrf), securityCodeField("Anmeldecode", "login"), submitButton("Bestätigen", "btn-primary")), linkButton(login+"?local=1", "Zurück zur Anmeldung", "btn-secondary"))

	return adminDocument(base, "Anmeldung bestätigen · Meshora", "auth", elem.Main(attrs.Props{attrs.Class: "auth-card"}, fields...))
}

// AdminUserSecurity contains policy status and the acting administrator's confirmation requirements.
type AdminUserSecurity struct {
	Policy        types.UserAuthPolicy
	TOTPEnabled   bool
	PasskeyCount  int
	NeedPassword  bool
	NeedCode      bool
	NeedOIDCLogin bool
}

func adminPauseStatus(until *time.Time) string {
	if until == nil {
		return "Nicht pausiert"
	}

	if !time.Now().Before(*until) {
		return "Abgelaufen (" + until.UTC().Format(time.RFC3339) + ")"
	}

	return "Pausiert bis " + until.UTC().Format(time.RFC3339)
}

func adminUserAuthentication(info AdminPageInfo, path string, security *AdminUserSecurity) elem.Node {
	if security == nil {
		return elem.Text("")
	}

	props := attrs.Props{attrs.Type: authPolicyCheckboxType, attrs.Name: "require_strong_auth"}
	if security.Policy.RequireStrongAuth {
		props[attrs.Checked] = "true"
	}

	passkeyLabel := " Passkeys"
	if security.PasskeyCount == 1 {
		passkeyLabel = " Passkey"
	}

	status := "Authenticator-App nicht eingerichtet"
	if security.TOTPEnabled {
		status = "Authenticator-App eingerichtet"
	}

	fields := []elem.Node{elem.Label(attrs.Props{attrs.Class: "check"}, elem.Input(props), elem.Span(nil, elem.Text("2FA oder Passkey verpflichtend verlangen")))}
	choices := []AdminChoice{{Value: "keep", Label: "Unverändert"}, {Value: "resume", Label: "Jetzt wieder aktivieren"}, {Value: "15m", Label: "15 Minuten"}, {Value: "1h", Label: "1 Stunde"}, {Value: "4h", Label: "4 Stunden"}, {Value: "24h", Label: "24 Stunden"}}

	fields = append(fields, zoneSelect("2FA-Prüfung vorübergehend aussetzen", "totp_pause", "support-totp-pause", choices, []string{"keep"}, false), zoneSelect("Passkey-Anmeldung vorübergehend sperren", "passkey_pause", "support-passkey-pause", choices, []string{"keep"}, false), textField("Support-Vermerk (optional)", "reason", "", false))
	if security.NeedOIDCLogin {
		fields = append(fields, elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Melde dich vor dieser Änderung erneut über OIDC an.")))
	} else if security.NeedPassword {
		fields = append(fields, elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Bestätige mit deinem eigenen Admin-Passwort und gegebenenfalls deinem eigenen Anmeldecode.")))
		fields = append(fields, securityProof(security.NeedCode, "admin-support")...)
	} else {
		fields = append(fields, elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Deine kürzlich bestätigte Anmeldung wird zur Freigabe verwendet.")))
	}

	fields = append(fields, submitButton("Anmeldesicherheit speichern", "btn-primary"))

	emailProps := attrs.Props{attrs.Type: authPolicyCheckboxType, attrs.Name: "allow_email_change"}
	if !security.Policy.EmailChangeBlocked {
		emailProps[attrs.Checked] = "true"
	}

	emailFields := []elem.Node{elem.Label(attrs.Props{attrs.Class: "check"}, elem.Input(emailProps), elem.Span(nil, elem.Text("E-Mail selbst hinzufügen oder ändern erlauben")))}
	if security.NeedPassword {
		emailFields = append(emailFields, securityProof(security.NeedCode, "email-policy")...)
	}

	if security.NeedOIDCLogin {
		emailFields = append(emailFields, elem.P(nil, elem.Text("Bitte zuerst erneut ueber OIDC anmelden.")))
	}

	emailFields = append(emailFields, submitButton("E-Mail-Freigabe speichern", "btn-secondary"))
	emailForm := postForm(info, path+"/email-policy", "action-form", emailFields...)

	return cardSection("Anmeldesicherheit", "Jede Änderung beendet sofort alle WebUI-Sitzungen dieses Nutzers. Ohne eingerichteten Faktor erlaubt die Pflicht zunächst nur die Einrichtung. Eine 2FA-Ausnahme erlaubt bis zu ihrem Ablauf eine Anmeldung ohne zweiten Faktor; gespeicherte Schlüssel werden nicht gelöscht.",
		detailList(detail("Eingerichtet", elem.Text(status+" · "+strconv.Itoa(security.PasskeyCount)+passkeyLabel)), detail("2FA-Ausnahme", elem.Text(adminPauseStatus(security.Policy.TOTPSuspendedUntil))), detail("Passkey-Anmeldung", elem.Text(adminPauseStatus(security.Policy.PasskeysSuspendedUntil)))),
		postForm(info, path+"/auth-policy", "action-form", fields...), emailForm)
}
