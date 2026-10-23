package templates

import (
	"bytes"
	"html"
	"net/http"
	"regexp"
	"strings"

	"github.com/chasefleming/elem-go"
	"github.com/chasefleming/elem-go/attrs"
	"golang.org/x/text/language"
)

// Only UI-owned text is marked. Usernames, device names, secrets, code and
// administrator-supplied button labels are deliberately never translated.
func i18nText(text string) elem.Node {
	return elem.Raw("<!--meshora-i18n-->" + html.EscapeString(text) + "<!--/meshora-i18n-->")
}

var documentTitle = regexp.MustCompile(`<title>(.*?)</title>`)

var localizedText = regexp.MustCompile(`(?s)<!--meshora-i18n-->(.*?)<!--/meshora-i18n-->`)

// Localize renders marked text in a request-local language, without global state.
func Localize(body, lang string) string {
	body = localizedText.ReplaceAllStringFunc(body, func(marked string) string {
		value := html.UnescapeString(strings.TrimSuffix(strings.TrimPrefix(marked, "<!--meshora-i18n-->"), "<!--/meshora-i18n-->"))
		return html.EscapeString(translate(value, lang))
	})
	body = documentTitle.ReplaceAllStringFunc(body, func(title string) string {
		value := html.UnescapeString(strings.TrimSuffix(strings.TrimPrefix(title, "<title>"), "</title>"))

		pieces := strings.Split(value, " · ")
		for i := range pieces {
			pieces[i] = translate(pieces[i], lang)
		}

		return "<title>" + html.EscapeString(strings.Join(pieces, " · ")) + "</title>"
	})

	return strings.Replace(body, `<html lang="en">`, `<html lang="`+lang+`">`, 1)
}

func translate(value, lang string) string {
	if after, ok := strings.CutPrefix(value, "← "); ok {
		return "← " + translate(after, lang)
	}

	for _, pair := range translations {
		if value == pair[0] || value == pair[1] {
			if lang == "de" {
				return pair[1]
			}

			return pair[0]
		}
	}

	if lang == "de" {
		for _, rule := range germanPatterns {
			if rule.pattern.MatchString(value) {
				return rule.pattern.ReplaceAllString(value, rule.text)
			}
		}
	}

	return value
}

// RequestLanguage prefers an explicit selection, then the saved preference,
// then Accept-Language. English is the default for clients without a preference.
func RequestLanguage(r *http.Request) string {
	if value := r.URL.Query().Get("lang"); value == "de" || value == "en" {
		return value
	}

	cookie, cookieErr := r.Cookie("meshora_language")
	if cookieErr == nil && (cookie.Value == "de" || cookie.Value == "en") {
		return cookie.Value
	}

	matcher := language.NewMatcher([]language.Tag{language.English, language.German})

	tags, _, err := language.ParseAcceptLanguage(r.Header.Get("Accept-Language"))
	if err == nil {
		_, index, _ := matcher.Match(tags...)
		if index == 1 {
			return "de"
		}
	}

	return "en"
}

func languageSwitcher() elem.Node {
	const (
		germanFlag  = `<svg class="language-flag" viewBox="0 0 24 16" aria-hidden="true"><path fill="#171717" d="M0 0h24v6H0z"/><path fill="#d52b30" d="M0 5.33h24v5.34H0z"/><path fill="#ffce32" d="M0 10.67h24V16H0z"/></svg>`
		englishFlag = `<svg class="language-flag" viewBox="0 0 24 16" aria-hidden="true"><path fill="#173a79" d="M0 0h24v16H0z"/><path stroke="#fff" stroke-width="4" d="m0 0 24 16M24 0 0 16"/><path stroke="#cf243a" stroke-width="1.5" d="m0 0 24 16M24 0 0 16"/><path stroke="#fff" stroke-width="6" d="M12 0v16M0 8h24"/><path stroke="#cf243a" stroke-width="3" d="M12 0v16M0 8h24"/></svg>`
	)

	return elem.Div(attrs.Props{attrs.Class: "language-switcher"},
		elem.Button(attrs.Props{attrs.Type: "button", attrs.Class: "language-trigger", "popovertarget": "language-options", "aria-label": "Sprache / Language"},
			elem.Span(attrs.Props{attrs.Class: "language-current-de"}, elem.Raw(germanFlag), elem.Text("DE")),
			elem.Span(attrs.Props{attrs.Class: "language-current-en"}, elem.Raw(englishFlag), elem.Text("EN")),
			elem.Raw(`<svg class="language-chevron" viewBox="0 0 12 12" aria-hidden="true"><path d="m3 4.5 3 3 3-3" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>`)),
		elem.Div(attrs.Props{attrs.ID: "language-options", attrs.Class: "language-options", "popover": "auto"},
			elem.A(attrs.Props{attrs.Href: "?lang=de", "lang": "de", "hreflang": "de"}, elem.Raw(germanFlag), elem.Text("Deutsch")),
			elem.A(attrs.Props{attrs.Href: "?lang=en", "lang": "en", "hreflang": "en"}, elem.Raw(englishFlag), elem.Text("English"))),
	)
}

type languageResponse struct {
	http.ResponseWriter

	body   bytes.Buffer
	status int
}

func (w *languageResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *languageResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}

	return w.body.Write(data)
}

// LanguageMiddleware only buffers browser documents. It is not installed on
// Noise, API, DERP or other streaming protocol routes.
func LanguageMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lang := RequestLanguage(r)
		if selection := r.URL.Query().Get("lang"); selection == "de" || selection == "en" {
			// This non-sensitive preference is shared across public/setup/admin pages.
			//nolint:gosec // G124: non-sensitive language preference supports local HTTP; HttpOnly and SameSite are set.
			http.SetCookie(w, &http.Cookie{Name: "meshora_language", Value: lang, Path: "/", MaxAge: 31536000, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode})
		}

		response := &languageResponse{ResponseWriter: w}
		next.ServeHTTP(response, r)

		body := response.body.String()
		if strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			body = Localize(body, lang)
			w.Header().Set("Content-Language", lang)
			w.Header().Add("Vary", "Accept-Language, Cookie")
			w.Header().Del("Content-Length")
		}

		if response.status == 0 {
			response.status = http.StatusOK
		}

		w.WriteHeader(response.status)
		//nolint:gosec // G705: only escaped template-owned text is translated; arbitrary record data stays escaped.
		_, _ = w.Write([]byte(body))
	})
}

// PublicLanguageMiddleware explicitly excludes protocol/streaming paths and
// the dashboard, which installs the same middleware itself (including setup).
func PublicLanguageMiddleware(next http.Handler) http.Handler {
	localized := LanguageMiddleware(next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" || path == "/windows" || path == "/apple" || strings.HasPrefix(path, "/apple/") || strings.HasPrefix(path, "/register/") || strings.HasPrefix(path, "/auth/") || path == "/oidc/callback" {
			localized.ServeHTTP(w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}

var germanPatterns = []struct {
	pattern *regexp.Regexp
	text    string
}{
	{regexp.MustCompile(`^(\d+)% of nodes connected$`), `${1}% der Geräte verbunden`},
	{regexp.MustCompile(`^(\d+) users? in this tailnet\.$`), `${1} Benutzer in diesem Tailnet.`},
	{regexp.MustCompile(`^(\d+) nodes? registered\.$`), `${1} Geräte registriert.`},
	{regexp.MustCompile(`^(\d+) keys?\. Keys are shown in full only once\.$`), `${1} Schlüssel. Vollständige Schlüssel werden nur einmal angezeigt.`},
	{regexp.MustCompile(`^(\d+) min ago$`), `vor ${1} Min.`},
	{regexp.MustCompile(`^in (\d+) min$`), `in ${1} Min.`},
	{regexp.MustCompile(`^(\d+) hours? ago$`), `vor ${1} Std.`},
	{regexp.MustCompile(`^in (\d+) hours?$`), `in ${1} Std.`},
	{regexp.MustCompile(`^(\d+) days? ago$`), `vor ${1} Tagen`},
	{regexp.MustCompile(`^in (\d+) days?$`), `in ${1} Tagen`},
	{regexp.MustCompile(`^Created user (.*)\.$`), `Benutzer ${1} angelegt.`},
	{regexp.MustCompile(`^Renamed the user to (.*)\.$`), `Benutzer in ${1} umbenannt.`},
	{regexp.MustCompile(`^Deleted user (.*)\.$`), `Benutzer ${1} gelöscht.`},
	{regexp.MustCompile(`^Renamed the node to (.*)\.$`), `Gerät in ${1} umbenannt.`},
	{regexp.MustCompile(`^Set tags to (.*)\.$`), `Tags auf ${1} gesetzt.`},
	{regexp.MustCompile(`^Expired (.*)\.$`), `${1} abgemeldet.`},
	{regexp.MustCompile(`^Deleted (.*)\.$`), `${1} gelöscht.`},
	{regexp.MustCompile(`^Expired key (.*)\.$`), `Schlüssel ${1} deaktiviert.`},
	{regexp.MustCompile(`^Could not (.*): (.*)$`), `Aktion fehlgeschlagen: ${2}`},
}

var translations = [][2]string{
	{"Sign in", "Anmelden"},
	{"Use your Meshora admin account.", "Melde dich mit deinem Meshora-Admin-Konto an."},
	{"Username", "Benutzername"},
	{"Password", "Passwort"},
	{"Sign in with Authentik", "Mit Authentik anmelden"},
	{"Use local admin login", "Lokal als Admin anmelden"},
	{"Overview", "Übersicht"},
	{"Your tailnet at a glance.", "Dein Tailnet auf einen Blick."},
	{"Users", "Benutzer"},
	{"Nodes", "Geräte"},
	{"Devices", "Geräte"},
	{"Device", "Gerät"},
	{"Pending", "Ausstehend"},
	{"Keys", "Schlüssel"},
	{"Settings", "Einstellungen"},
	{"Tailnet users", "Tailnet-Benutzer"},
	{"Registered devices", "Registrierte Geräte"},
	{"Online", "Online"},
	{"Offline", "Offline"},
	{"Unknown", "Unbekannt"},
	{"Logins in progress", "Laufende Anmeldungen"},
	{"Add a device", "Gerät hinzufügen"},
	{"Run this on the device. The user then signs in with their identity provider.", "Führe diesen Befehl auf dem Gerät aus. Der Benutzer meldet sich anschließend beim Identitätsanbieter an."},
	{"No users yet", "Noch keine Benutzer"},
	{"Add a user", "Benutzer hinzufügen"},
	{"Add user", "Benutzer hinzufügen"},
	{"Display name", "Anzeigename"},
	{"Email", "E-Mail"},
	{"No nodes yet", "Noch keine Geräte"},
	{"Nothing pending", "Nichts ausstehend"},
	{"Logins that have started but not finished yet. They expire on their own.", "Begonnene, noch nicht abgeschlossene Anmeldungen. Sie laufen automatisch ab."},
	{"Registration", "Registrierung"},
	{"SSH check", "SSH-Prüfung"},
	{"Review", "Prüfen"},
	{"Never", "Nie"},
	{"Expired", "Abgelaufen"},
	{"Sign out", "Abmelden"},
	{"Name", "Name"},
	{"Owner", "Besitzer"},
	{"Status", "Status"},
	{"Addresses", "Adressen"},
	{"Last seen", "Zuletzt gesehen"},
	{"Expiry", "Ablauf"},
	{"Created", "Erstellt"},
	{"Registered", "Registriert"},
	{"Registered via", "Registriert über"},
	{"Machine key", "Geräteschlüssel"},
	{"Started", "Gestartet"},
	{"Type", "Typ"},
	{"Action", "Aktion"},
	{"Actions", "Aktionen"},
	{"No devices yet.", "Noch keine Geräte."},
	{"Rename", "Umbenennen"},
	{"Changes the username used in ACL policies.", "Ändert den Benutzernamen in den ACL-Regeln."},
	{"New username", "Neuer Benutzername"},
	{"Delete user", "Benutzer löschen"},
	{"Delete user…", "Benutzer löschen…"},
	{"Only possible once the user owns no devices.", "Erst möglich, wenn der Benutzer keine Geräte mehr besitzt."},
	{"Sets the name the node has in MagicDNS.", "Legt den Gerätenamen in MagicDNS fest."},
	{"New name", "Neuer Name"},
	{"Tags, comma separated", "Tags, durch Kommas getrennt"},
	{"Set tags", "Tags setzen"},
	{"Tagged nodes always keep at least one tag.", "Getaggte Geräte behalten immer mindestens einen Tag."},
	{"Tagging hands the node over to its tags: it no longer belongs to a user. Tags must be allowed by tagOwners in the policy.", "Tags übernehmen den Besitz des Geräts; es gehört dann keinem Benutzer mehr. Die Tags müssen in der Richtlinie unter tagOwners erlaubt sein."},
	{"Expire or delete", "Abmelden oder löschen"},
	{"Expire node…", "Gerät abmelden…"},
	{"Delete node…", "Gerät löschen…"},
	{"Expire node", "Gerät abmelden"},
	{"Delete node", "Gerät löschen"},
	{"Device registration", "Geräteregistrierung"},
	{"Reject", "Ablehnen"},
	{"Approve", "Freigeben"},
	{"Decide", "Entscheiden"},
	{"Approving lets this SSH connection through once. The client is waiting for the answer.", "Die Freigabe erlaubt diese SSH-Verbindung einmalig. Der Client wartet auf die Antwort."},
	{"There are no users yet. Add one on the Users page first.", "Noch keine Benutzer vorhanden. Lege zuerst einen unter Benutzer an."},
	{"User", "Benutzer"},
	{"Register device", "Gerät registrieren"},
	{"Assign to a user", "Einem Benutzer zuweisen"},
	{"Registers the device for this user. It connects as soon as the client notices.", "Registriert das Gerät für diesen Benutzer. Es verbindet sich, sobald der Client die Änderung erkennt."},
	{"The client is told the login failed. If it tries again, it shows up here as a new request.", "Der Client erhält eine Fehlermeldung. Bei einem neuen Versuch erscheint hier eine neue Anfrage."},
	{"No pre-auth keys", "Keine Vorab-Authentifizierungsschlüssel"},
	{"New pre-auth key", "Neuer Vorab-Authentifizierungsschlüssel"},
	{"A key with tags registers tagged devices; otherwise devices belong to the chosen user.", "Ein Schlüssel mit Tags registriert getaggte Geräte. Andernfalls gehören die Geräte dem gewählten Benutzer."},
	{"No user (tags only)", "Kein Benutzer (nur Tags)"},
	{"Valid for", "Gültig für"},
	{"Reusable", "Wiederverwendbar"},
	{"Ephemeral devices", "Temporäre Geräte"},
	{"Create key", "Schlüssel erstellen"},
	{"Pre-auth keys", "Vorab-Authentifizierungsschlüssel"},
	{"Active", "Aktiv"},
	{"Used", "Verwendet"},
	{"Revoked", "Widerrufen"},
	{"reusable", "wiederverwendbar"},
	{"ephemeral", "temporär"},
	{"Expire…", "Deaktivieren…"},
	{"Expire key", "Schlüssel deaktivieren"},
	{"Expire pre-auth key", "Vorab-Authentifizierungsschlüssel deaktivieren"},
	{"Cancel", "Abbrechen"},
	{"The device is disconnected from the tailnet until its user signs in again.", "Das Gerät wird vom Tailnet getrennt, bis sich sein Benutzer erneut anmeldet."},
	{"The device is removed from the tailnet. It has to be registered again to reconnect.", "Das Gerät wird aus dem Tailnet entfernt. Für eine neue Verbindung muss es erneut registriert werden."},
	{"The key can no longer register devices. Devices it already registered stay connected.", "Der Schlüssel kann keine Geräte mehr registrieren. Bereits registrierte Geräte bleiben verbunden."},
	{"1 hour", "1 Stunde"},
	{"1 day", "1 Tag"},
	{"7 days", "7 Tage"},
	{"30 days", "30 Tage"},
	{"90 days", "90 Tage"},
	{"just now", "gerade eben"},
	{"Not configured yet.", "Noch nicht eingerichtet."},
	{"Settings saved.", "Einstellungen gespeichert."},
	{"Restart required: saved settings will take effect at the next server start.", "Neustart erforderlich: Die gespeicherten Einstellungen werden beim nächsten Serverstart übernommen."},
	{"Enter the client secret from your Authentik provider.", "Trage das Client-Secret aus deinem Authentik-Provider ein."},
	{"A secret is saved. Leave blank to keep it. It will not be shown again.", "Ein Secret ist gespeichert. Leer lassen, um es beizubehalten. Es wird nicht wieder angezeigt."},
	{"This instance needs a readable YAML configuration file to save settings here.", "Diese Instanz benötigt eine lesbare YAML-Konfigurationsdatei, um Einstellungen hier zu speichern."},
	{"Issuer URL", "Issuer-URL"},
	{"Client ID", "Client-ID"},
	{"Client secret", "Client-Secret"},
	{"Authentik groups with admin access", "Authentik-Gruppen mit Admin-Zugriff"},
	{"Comma-separated groups, e.g. meshora-admins. Device OIDC groups never grant admin access.", "Kommagetrennte Gruppen, z. B. meshora-admins. OIDC-Gerätegruppen vergeben keine Admin-Rechte."},
	{"Admin roles", "Admin-Rollen"},
	{"Roles claim", "Rollen-Claim"},
	{"Comma-separated admin roles from a signed ID-token claim (default: roles). If both groups and roles are configured, both restrictions must match. Empty groups and roles disable admin OIDC.", "Kommagetrennte Admin-Rollen aus einem signierten ID-Token-Claim (Standard: roles). Wenn Gruppen und Rollen konfiguriert sind, müssen beide Bedingungen erfüllt sein. Leere Gruppen- und Rollenlisten deaktivieren Admin-OIDC."},
	{"Saving changes sign-in after a restart. Existing access restrictions and OIDC options are preserved.", "Speichern ändert die Anmeldung nach einem Neustart. Bestehende Zugriffsbeschränkungen und OIDC-Optionen bleiben erhalten."},
	{"Save", "Speichern"},
	{"Saving requires a writable YAML configuration.", "Zum Speichern wird eine beschreibbare YAML-Configuration benoetigt."},
	{"OIDC login button text", "Text des OIDC-Login-Buttons"},
	{"Leave blank for the default: Sign in with Authentik. Maximum 100 characters. Changes take effect immediately without restarting.", "Leer lassen fuer den Standard: Mit Authentik anmelden. Maximal 100 Zeichen. Die Aenderung gilt sofort, ohne Neustart."},
	{"Save button text", "Button-Text speichern"},
	{"Authentik / OpenID Connect for device and admin sign-in.", "Authentik / OpenID Connect für die Anmeldung deiner Geräte."},
	{"Sign-in status", "Anmeldestatus"},
	{"OIDC login button", "OIDC-Login-Button"},
	{"Customize the button label on the admin sign-in page.", "Passe die Beschriftung auf der Admin-Anmeldeseite an."},
	{"Authentik provider", "Authentik-Provider"},
	{"Find the client credentials in your Authentik OAuth2/OpenID Connect provider.", "Die Client-Zugangsdaten findest du im OAuth2/OpenID-Connect-Provider von Authentik."},
	{"Redirect URL", "Rückleitungs-URL"},
	{"Add this URL as a strict redirect URI in Authentik.", "Diese URL in Authentik als Redirect-URI mit Strict-Abgleich eintragen."},
	{"Admin redirect URI", "Admin-Redirect-URI"},
	{"Also add this as a strict redirect URI in Authentik. Signed ID tokens must contain the configured groups and roles claims.", "In Authentik ebenfalls mit Strict-Abgleich eintragen. Der signierte ID-Token muss den groups-Claim mit den freigegebenen Gruppennamen enthalten."},
	{"Welcome to Meshora", "Willkommen bei Meshora"},
	{"Set up your own instance. All settings will be saved permanently.", "Richte deine eigene Instanz ein. Alle Angaben werden dauerhaft gespeichert."},
	{"1 · Server", "1 · Server"},
	{"The URL browsers and Tailscale clients use to reach this instance. Without an additional path.", "Die URL, über die Browser und Tailscale-Clients diese Instanz erreichen. Ohne zusätzlichen Pfad."},
	{"Public server URL", "Öffentliche Server-URL"},
	{"2 · Admin account", "2 · Admin-Konto"},
	{"Your local administrator account. Use a unique password with at least 12 characters.", "Dein lokales Administratorkonto. Verwende ein eigenes Passwort mit mindestens 12 Zeichen."},
	{"Repeat password", "Passwort wiederholen"},
	{"3 · OpenID Connect (optional)", "3 · OpenID Connect (optional)"},
	{"For example Authentik. Client ID and secret come from your OIDC provider.", "Zum Beispiel Authentik. Client-ID und Secret stammen aus deinem OIDC-Provider."},
	{"Optional: leave all OIDC fields blank to configure the provider later.", "Optional: Alle OIDC-Felder leer lassen, um den Anbieter später einzurichten."},
	{"A secret is saved. Leave blank to keep it.", "Ein Secret ist gespeichert. Leer lassen, um es beizubehalten."},
	{"Provider redirect URI: public server URL plus /oidc/callback.", "Redirect-URI beim Provider: öffentliche Server-URL plus /oidc/callback."},
	{"Complete setup", "Einrichtung abschließen"},
	{"Setup complete", "Einrichtung abgeschlossen"},
	{"Setup", "Einrichtung"},
	{"Meshora is starting with your settings. You will be redirected to sign-in automatically.", "Meshora startet jetzt mit deinen Einstellungen. Du wirst automatisch zur Anmeldung weitergeleitet."},
	{"Go to sign-in", "Zur Anmeldung"},
	{"Invalid username or password.", "Ungültiger Benutzername oder ungültiges Passwort."},
	{"Too many failed login attempts. Try again later.", "Zu viele Anmeldeversuche. Bitte versuche es später erneut."},
	{"Your login form expired. Please try again.", "Dein Anmeldeformular ist abgelaufen. Bitte versuche es erneut."},
	{"Authentik is unavailable. Use your local admin account or try again later.", "Authentik ist nicht erreichbar. Verwende dein lokales Admin-Konto oder versuche es später erneut."},
	{"Invalid or expired admin login. Start again from the login page.", "Ungültige oder abgelaufene Admin-Anmeldung. Beginne erneut auf der Login-Seite."},
	{"Authentik sign-in was cancelled or rejected.", "Die Authentik-Anmeldung wurde abgebrochen oder abgelehnt. Melde dich lokal an oder versuche es erneut."},
	{"Your Authentik account does not have an authorized admin role.", "Deinem Authentik-Konto fehlt eine freigegebene Admin-Rolle."},
	{"Your Authentik account does not satisfy the configured admin groups and roles.", "Dein Authentik-Konto erfüllt die konfigurierten Admin-Gruppen und Rollen nicht."},
	{"Authentik sign-in could not be verified. Please start again.", "Die Authentik-Anmeldung konnte nicht bestätigt werden. Bitte versuche es erneut oder melde dich lokal an."},
	{"Enter a username.", "Gib einen Benutzernamen ein."},
	{"Enter the new username.", "Gib den neuen Benutzernamen ein."},
	{"Enter the new name.", "Gib den neuen Namen ein."},
	{"Choose a user.", "Wähle einen Benutzer."},
	{"That user no longer exists.", "Dieser Benutzer existiert nicht mehr."},
	{"That request is no longer pending.", "Diese Anfrage ist nicht mehr ausstehend."},
	{"Only device registrations can be assigned to a user.", "Nur Geräteregistrierungen können einem Benutzer zugewiesen werden."},
	{"Registrations are approved by assigning them to a user.", "Registrierungen werden durch Zuweisung zu einem Benutzer freigegeben."},
	{"Approved the SSH check.", "SSH-Prüfung freigegeben."},
	{"Rejected the SSH check.", "SSH-Prüfung abgelehnt."},
	{"Rejected the registration. If the device tries again, it shows up here as a new request.", "Registrierung abgelehnt. Bei einem neuen Versuch erscheint hier eine neue Anfrage."},
	{"Choose a user or enter tags for the new key.", "Wähle einen Benutzer oder gib Tags für den neuen Schlüssel ein."},
	{"Choose how long the key stays valid.", "Wähle die Gültigkeitsdauer des Schlüssels."},
	{"Created a new pre-auth key. Copy it now: it is not shown again.", "Vorab-Authentifizierungsschlüssel erstellt. Kopiere ihn jetzt; er wird nicht erneut angezeigt."},
	{"Settings saved. Restart Meshora to activate Authentik sign-in.", "Gespeichert. Starte Meshora neu, um die Authentik-Anmeldung zu aktivieren."},
	{"Button text saved. The change is active immediately.", "Button-Text gespeichert. Die Aenderung ist sofort aktiv."},
	{"The button text must have at most 100 characters and no line breaks.", "Der Button-Text darf maximal 100 Zeichen und keine Zeilenumbrueche enthalten."},
	{"The YAML configuration could not be read.", "Die YAML-Configuration konnte nicht gelesen werden."},
	{"Saving failed. Check the YAML file permissions.", "Speichern fehlgeschlagen. Pruefe die Schreibrechte der YAML-Datei."},
	{"Enter a valid roles claim name (maximum 128 characters).", "Gib einen gültigen Rollen-Claim-Namen mit maximal 128 Zeichen ein."},
	{"Invalid web_admin configuration.", "Ungültige web_admin-Configuration."},
	{"Invalid admin OIDC configuration.", "Ungültige Admin-OIDC-Configuration."},
	{"Getting started", "Erste Schritte"},
	{"Check out the documentation to learn more about headscale and Tailscale:", "Mehr über Headscale und Tailscale erfährst du in der Dokumentation:"},
	{"Headscale documentation", "Headscale-Dokumentation"},
	{"Tailscale docs", "Tailscale-Dokumentation"},
	{"Windows configuration", "Windows-Configuration"},
	{"Node registration", "Geräteregistrierung"},
	{"Registered to", "Registriert für"},
	{"Confirm registration", "Registrierung bestätigen"},
	{"Forbidden", "Zugriff verweigert"},
	{"Node registered", "Gerät registriert"},
	{"You can now close this window.", "Du kannst dieses Fenster jetzt schließen."},
	{"iOS configuration", "iOS-Configuration"},
	{"GUI", "Grafische Oberfläche"},
	{"Install the official Tailscale iOS client from the ", "Installiere den offiziellen Tailscale-iOS-Client aus dem "},
	{"Open the ", "Öffne die "},
	{" app", " App"},
	{"Click the account icon in the top-right corner and select ", "Klicke oben rechts auf das Kontosymbol und wähle "},
	{"Log in…", "Anmelden…"},
	{"Tap the top-right options menu button and select ", "Tippe oben rechts auf das Optionsmenü und wähle "},
	{"Use custom coordination server", "Eigenen Koordinationsserver verwenden"},
	{"Enter your instance URL: ", "Gib die URL deiner Instanz ein: "},
	{"macOS configuration", "macOS-Configuration"},
	{"Command line", "Kommandozeile"},
	{"Use Tailscale's login command to add your profile:", "Füge dein Profil mit dem Tailscale-Anmeldebefehl hinzu:"},
	{"Option + Click the ", "Halte die Wahltaste gedrückt und klicke auf das "},
	{" icon in the menu and hover over the ", " Symbol im Menü und bewege den Mauszeiger über das "},
	{"Debug", "Debug"},
	{" menu", " Menü"},
	{"Under ", "Unter "},
	{"Custom Login Server", "Eigener Anmeldeserver"},
	{", select ", " wähle "},
	{"Add Account...", "Konto hinzufügen..."},
	{"Enter ", "Gib "},
	{" of the headscale instance and press ", " der Headscale-Instanz ein und wähle "},
	{"Add Account", "Konto hinzufügen"},
	{"Follow the login procedure in the browser", "Folge dem Anmeldeablauf im Browser"},
	{"Profiles", "Profile"},
	{"Open ", "Öffne "},
	{"System Preferences", "Systemeinstellungen"},
	{" and go to ", " und gehe zu "},
	{"Find and install the ", "Suche und installiere das "},
	{" profile", " Profil"},
	{"Restart ", "Starte "},
	{" and log in", " neu und melde dich an"},
	{"For app store client:", "Für den App-Store-Client:"},
	{"For standalone client:", "Für den eigenständigen Client:"},
	{" and log in.", " neu und melde dich an."},
	{"tvOS configuration", "tvOS-Configuration"},
	{"Install the official Tailscale tvOS client from the ", "Installiere den offiziellen Tailscale-tvOS-Client aus dem "},
	{"Select ", "Wähle "},
	{"Install VPN configuration", "VPN-Configuration installieren"},
	{"Allow", "Erlauben"},
	{", but don't sign in yet", ", melde dich aber noch nicht an"},
	{" (the Apple tvOS settings) > ", " (die Apple-tvOS-Einstellungen) > "},
	{"Apps", "Apps"},
	{" under ", " unter "},
	{"Return to the tvOS ", "Gehe zum tvOS-"},
	{"Home", "Startbildschirm"},
	{" screen and open ", " zurück und öffne "},
	{"Scan the QR code and follow the login procedure", "Scanne den QR-Code und folge dem Anmeldeablauf"},
	{"Headscale should now be working on your tvOS device", "Headscale sollte jetzt auf deinem tvOS-Gerät funktionieren"},
	{"Download ", "Lade "},
	{" and install it.", " herunter und installiere es."},
	{"Open a Command Prompt or PowerShell and use Tailscale's login command to connect with headscale:", "Öffne eine Eingabeaufforderung oder PowerShell und verbinde dich über den Tailscale-Anmeldebefehl mit Headscale:"},
	{"Ping Node", "Gerät anpingen"},
	{"Check if a connected node responds to a PingRequest.", "Prüfe, ob ein verbundenes Gerät auf eine Ping-Anfrage antwortet."},
	{"Ping", "Ping"},
	{"Connected Nodes", "Verbundene Geräte"},
	{"or", "oder"},
	{"Confirm node registration", "Geräteregistrierung bestätigen"},
	{"The configuration cannot be read. No admin account was created.", "Die Configuration kann nicht gelesen werden. Es wurde kein Admin-Konto angelegt."},
	{"The web admin configuration is missing or invalid.", "Die Web-Admin-Configuration fehlt oder ist ungültig."},
	{"Invalid OIDC configuration.", "Ungültige OIDC-Configuration."},
	{"Choose a valid admin username.", "Wähle einen gültigen Admin-Benutzernamen."},
	{"Saving failed. No admin account was created; you can try again.", "Speichern fehlgeschlagen. Es wurde kein Admin-Konto angelegt; du kannst es erneut versuchen."},
	{"Enter a valid HTTP or HTTPS server URL without credentials, path, query or fragment. Its host must be outside the MagicDNS domain.", "Trage eine gültige HTTP- oder HTTPS-Server-URL ohne Zugangsdaten, Pfad, Query oder Fragment ein. Der Host darf nicht innerhalb der MagicDNS-Domain liegen."},
	{"Passwords must match and be at least 12 characters long.", "Die Passwörter müssen übereinstimmen und mindestens 12 Zeichen lang sein."},
	{"OIDC requires the issuer URL, client ID and client secret together.", "Für OIDC werden Issuer-URL, Client-ID und Client-Secret gemeinsam benötigt."},
	{"Enter a valid issuer URL, client ID and client secret, or leave all OIDC fields blank.", "Gib eine gültige Issuer-URL, Client-ID und ein Client-Secret ein oder lasse alle OIDC-Felder leer."},
	{"The OIDC provider is unavailable or returned an incompatible OpenID configuration. Check the issuer URL or configure OIDC later.", "Der OIDC-Anbieter ist nicht erreichbar oder liefert keine passende OpenID-Configuration. Prüfe die Issuer-URL oder richte OIDC später ein."},
	{"Invalid issuer URL. Use HTTPS without credentials, query or fragment.", "Ungültige Issuer-URL. Verwende eine HTTPS-Adresse ohne Zugangsdaten, Query oder Fragment."},
	{"Enter a valid client ID.", "Trage eine gültige Client-ID ein."},
	{"The YAML file could not be read. Nothing was saved.", "Die YAML-Datei konnte nicht gelesen werden. Es wurde nichts gespeichert."},
	{"Enter the client secret.", "Trage das Client-Secret ein."},
	{"Saving failed. Check the YAML file permissions. The previous file is preserved.", "Speichern fehlgeschlagen. Prüfe die Schreibrechte der YAML-Datei. Die bisherige Datei bleibt erhalten."},
	{"This deletes the user and their pre-auth keys. It cannot be undone. Devices owned by this user must be deleted first.", "Dies löscht den Benutzer und seine Vorab-Authentifizierungsschlüssel unwiderruflich. Seine Geräte müssen zuerst gelöscht werden."},
	{"Users appear here once they sign in with your identity provider.", "Benutzer erscheinen hier nach der Anmeldung bei deinem Identitätsanbieter."},
	{"Users who sign in with your identity provider are created automatically. Add one here to register devices for them before they sign in.", "Benutzer werden bei der Anmeldung beim Identitätsanbieter automatisch angelegt. Lege sie hier vorab an, um schon vorher Geräte für sie zu registrieren."},
	{"Nodes appear here once a device has been registered.", "Geräte erscheinen hier nach ihrer Registrierung."},
	{"Devices show up here while their user is signing in.", "Geräte erscheinen hier, während sich ihr Benutzer anmeldet."},
	{"Pre-auth keys register devices without an interactive login, e.g. servers.", "Vorab-Authentifizierungsschlüssel registrieren Geräte ohne interactive Anmeldung, zum Beispiel Server."},
	{"A device is asking to be added to your tailnet. Please review the details below and confirm that this device is yours.", "Ein Gerät möchte deinem Tailnet beitreten. Prüfe die Angaben und bestätige, dass dieses Gerät dir gehört."},
	{"If you do not recognise this device, close this window. The registration request will expire automatically.", "Wenn du dieses Gerät nicht kennst, schließe dieses Fenster. Die Registrierungsanfrage läuft automatisch ab."},
	{"Headscale - Confirm node registration", "Headscale – Geräteregistrierung bestätigen"},
	{"Registered as ", "Registriert als "},
	{"Reauthenticated as ", "Erneut angemeldet als "},
	{"Authorized as ", "Autorisiert als "},
	{"Navigation", "Navigation"},
	{"Workspace", "Arbeitsbereich"},
	{"Administration", "Verwaltung"},
	{"Identity provider", "Identitätsanbieter"},
	{"Access rules", "Zugriffsrechte"},
	{"Appearance", "Darstellung"},
	{"Redirect URLs", "Rückleitungs-URLs"},
	{"01 / Connection", "01 / Verbindung"},
	{"02 / Permissions", "02 / Berechtigungen"},
	{"03 / Appearance", "03 / Darstellung"},
	{"Connect Authentik to handle device and administrator sign-in.", "Verbinde Authentik für die Anmeldung von Geräten und Administratoren."},
	{"Admin access", "Admin-Zugriff"},
	{"Choose which Authentik identities can administer this instance.", "Lege fest, welche Authentik-Benutzer diese Instanz verwalten dürfen."},
	{"Use comma-separated names. If both groups and roles are set, both must match. Leave both empty to disable admin OIDC.", "Trenne Namen durch Kommas. Sind Gruppen und Rollen gesetzt, müssen beide passen. Lasse beide Listen leer, um Admin-OIDC zu deaktivieren."},
	{"Advanced claim mapping", "Erweiterte Claim-Zuordnung"},
	{"Provider and access changes require a restart.", "Änderungen an Anbieter und Zugriffsrechten benötigen einen Neustart."},
	{"Save sign-in settings", "Anmeldeeinstellungen speichern"},
	{"Manage sign-in, permissions and appearance.", "Verwalte Anmeldung, Zugriffsrechte und Darstellung."},
	{"Add these exact URLs to Authentik using strict matching.", "Trage diese URLs in Authentik mit Strict-Abgleich ein."},
	{"Administrators", "Administratoren"},
	{"Local access", "Lokaler Zugang"},
	{"Keep a local admin account available for recovery.", "Behalte ein lokales Admin-Konto für den Notfall."},
}
