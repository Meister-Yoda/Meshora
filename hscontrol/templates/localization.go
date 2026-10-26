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

	body = strings.ReplaceAll(body, `href="?lang=`+lang+`"`, `aria-current="true" href="?lang=`+lang+`"`)

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
	{"Edit policy", "Policy bearbeiten"},
	{"Validate, save and activate are separate actions", "Validieren, Speichern und Aktivieren sind getrennte Aktionen"},
	{"Loaded policy evidence", "Nachweis der geladenen Policy"},
	{"This snapshot records the exact input accepted by the running engine. It does not confirm client delivery, allowed access or reachability.", "Dieser Snapshot erfasst die von der laufenden Engine übernommene Originaleingabe. Er bestätigt weder die Zustellung an Clients noch erlaubten Zugriff oder Erreichbarkeit."},
	{"Loaded revision", "Geladene Revision"},
	{"Loaded SHA-256", "SHA-256 des geladenen Stands"},
	{"Engine acceptance time (UTC)", "Übernahmezeitpunkt der Engine (UTC)"},
	{"Implicit boot policy", "Implizite Policy beim Start"},
	{"Saved source", "Gespeicherte Quelle"},
	{"The source and loaded snapshot are separate evidence. The loaded snapshot records engine acceptance, not client delivery or effective access.", "Quellstand und geladener Snapshot sind getrennte Nachweise. Der geladene Snapshot belegt die Übernahme durch die Engine, nicht die Client-Zustellung oder wirksamen Zugriff."},
	{"Show loaded policy text", "Geladenen Policy-Text anzeigen"},
	{"Engine diagnostics", "Engine-Diagnose"},
	{"Diagnostic text is shown as text, never executed.", "Diagnosetext wird als Text angezeigt und niemals ausgeführt."},
	{"The saved source matches the loaded text.", "Die gespeicherte Quelle entspricht dem geladenen Text."},
	{"The saved source differs from the loaded policy. Saving alone does not activate it.", "Die gespeicherte Quelle weicht von der geladenen Policy ab. Speichern allein aktiviert sie nicht."},
	{"Saved and loaded state", "Gespeicherter und geladener Stand"},
	{"Show current saved source", "Aktuell gespeicherte Quelle anzeigen"},
	{"1. Validate and preview", "1. Validieren und Vorschau"},
	{"2. Save validated draft", "2. Geprüften Entwurf speichern"},
	{"HuJSON policy draft", "HuJSON-Policy-Entwurf"},
	{"Comments and unknown fields are preserved without JSON reformatting. Duplicate keys must be resolved explicitly. An edited draft must be validated again before saving.", "Kommentare und unbekannte Felder bleiben ohne JSON-Neuformatierung erhalten. Doppelte Schlüssel müssen ausdrücklich aufgelöst werden. Ein bearbeiteter Entwurf muss vor dem Speichern erneut validiert werden."},
	{"Policy draft", "Policy-Entwurf"},
	{"Validation uses an isolated instance of the policy engine and its declared tests. It does not replace the active policy. Saving writes the reviewed draft only.", "Die Validierung verwendet eine isolierte Instanz der Policy-Engine und ihre deklarierten Tests. Sie ersetzt die aktive Policy nicht. Speichern schreibt nur den geprüften Entwurf."},
	{"Change preview", "Änderungsvorschau"},
	{"Minus lines belong to the current source; plus lines belong to the draft. The preview does not rewrite the draft. Validation must be repeated if text or runtime context changes.", "Minuszeilen gehören zur aktuellen Quelle, Pluszeilen zum Entwurf. Die Vorschau schreibt den Entwurf nicht um. Bei geändertem Text oder Laufzeitkontext muss erneut validiert werden."},
	{"No source changes.", "Keine Quelländerungen."},
	{"Activate saved policy", "Gespeicherte Policy aktivieren"},
	{"Activation revalidates the saved source against current users and devices. Invalid input or an engine-switch failure keeps the previous loaded policy. A later route-processing failure is reported as new policy active, follow-up work failed; it is not rolled back.", "Die Aktivierung prüft die gespeicherte Quelle erneut anhand aktueller Benutzer und Geräte. Bei ungültiger Eingabe oder fehlgeschlagenem Engine-Wechsel bleibt die bisher geladene Policy erhalten. Ein späterer Fehler der Routenverarbeitung wird als neue Policy aktiv, Folgearbeiten fehlgeschlagen gemeldet; es erfolgt kein Rollback."},
	{"3. Activate saved source", "3. Gespeicherte Quelle aktivieren"},
	{"Source and concurrency", "Quelle und gleichzeitige Änderungen"},
	{"File and database sources are editable. Writes in this process are serialized and stale source or loaded revisions are rejected. Do not write the source simultaneously through an external editor or another server process. Policy activation does not activate saved DNS settings.", "Datei- und Datenbankquellen sind bearbeitbar. Schreibvorgänge dieses Prozesses werden serialisiert und veraltete Quell- oder geladene Revisionen abgewiesen. Schreibe die Quelle nicht gleichzeitig mit einem externen Editor oder einem anderen Serverprozess. Die Policy-Aktivierung aktiviert keine gespeicherten DNS-Einstellungen."},
	{"The policy source is unavailable. No save or activation is possible.", "Die Policy-Quelle ist nicht verfügbar. Speichern und Aktivieren sind nicht möglich."},
	{"The source or loaded policy changed. Reload, review the differences and validate again.", "Die Quelle oder geladene Policy wurde geändert. Lade neu, prüfe die Unterschiede und validiere erneut."},
	{"Duplicate keys are ambiguous. Resolve them explicitly before validating again; nothing was normalized.", "Doppelte Schlüssel sind mehrdeutig. Löse sie vor der erneuten Validierung ausdrücklich auf; es wurde nichts normalisiert."},
	{"Policy validation failed. The active policy and source were not changed. Review the engine diagnostics below.", "Die Policy-Validierung ist fehlgeschlagen. Aktive Policy und Quelle wurden nicht geändert. Prüfe die Engine-Diagnose unten."},
	{"Validate this exact draft before saving. The validation is missing, expired or belongs to different text.", "Validiere genau diesen Entwurf vor dem Speichern. Die Validierung fehlt, ist abgelaufen oder gehört zu anderem Text."},
	{"Saving failed. The previous source and active policy were retained.", "Speichern fehlgeschlagen. Die bisherige Quelle und aktive Policy bleiben erhalten."},
	{"Policy source saved exactly as entered. It has not been activated.", "Policy-Quelle wie eingegeben gespeichert. Sie wurde nicht aktiviert."},
	{"Activation failed before the engine switch. The previously loaded policy is unchanged.", "Die Aktivierung ist vor dem Engine-Wechsel fehlgeschlagen. Die zuvor geladene Policy ist unverändert."},
	{"The new policy is active, but follow-up work failed. This was not rolled back. Review route approvals and retry activation after resolving the underlying error.", "Die neue Policy ist aktiv, aber Folgearbeiten sind fehlgeschlagen. Es erfolgte kein Rollback. Prüfe die Routengenehmigungen und wiederhole die Aktivierung nach Behebung des zugrunde liegenden Fehlers."},
	{"The saved policy was activated. Client delivery and reachability are not confirmed.", "Die gespeicherte Policy wurde aktiviert. Client-Zustellung und Erreichbarkeit sind nicht bestätigt."},
	{"Policy", "Policy"},
	{"Read-only policy source overview", "Nur lesende Übersicht der Policy-Quelle"},
	{"Evidence and limits", "Nachweis und Grenzen"},
	{"This page reads the current configured source. It does not compile policy, evaluate permissions, run policy tests, reload policy or change configuration.", "Diese Seite liest die aktuell konfigurierte Quelle. Sie kompiliert keine Policy, wertet keine Berechtigungen aus, führt keine Policy-Tests aus, lädt keine Policy neu und ändert keine Einstellungen."},
	{"Source mode", "Quellmodus"},
	{"Source reference", "Quellreferenz"},
	{"Current source SHA-256", "SHA-256 des aktuellen Quellstands"},
	{"File", "Datei"},
	{"Database", "Datenbank"},
	{"Loaded policy text and reload time cannot be verified through the current read interface. The source shown here may differ from what the running engine evaluates.", "Geladener Policy-Text und Ladezeitpunkt sind über die aktuelle Leseschnittstelle nicht nachweisbar. Die hier gezeigte Quelle kann von dem Stand abweichen, den die laufende Engine auswertet."},
	{"Configured rules do not prove allowed access or reachability. Actual evaluation depends on device identity, tags, policy context and current state.", "Konfigurierte Regeln belegen weder erlaubten Zugriff noch Erreichbarkeit. Die tatsächliche Auswertung hängt von Geräteidentität, Tags, Policy-Kontext und aktuellem Zustand ab."},
	{"No explicit policy in the source", "Keine ausdrückliche Policy in der Quelle"},
	{"An empty or absent source is not proof of the currently loaded policy or of any device permission.", "Eine leere oder fehlende Quelle belegt weder die aktuell geladene Policy noch Geräteberechtigungen."},
	{"Configured source sections", "Konfigurierte Quellabschnitte"},
	{"Parsed for display only. Recognized field names do not mean that the source passed policy-engine validation. All source fields remain visible.", "Nur für die Anzeige eingelesen. Erkannte Feldnamen bedeuten nicht, dass die Quelle die Prüfung der Policy-Engine bestanden hat. Alle Quellfelder bleiben sichtbar."},
	{"Network rules (ACLs)", "Netzwerkregeln (ACLs)"},
	{"Configured source and destination selectors, protocol and action. These rules have not been evaluated by this page.", "Konfigurierte Quell- und Zielselektoren, Protokoll und Aktion. Diese Seite hat die Regeln nicht ausgewertet."},
	{"Network and capability grants", "Netzwerk- und Capability-Grants"},
	{"Configured selectors, IP rules, application capabilities and via targets are shown completely.", "Konfigurierte Selektoren, IP-Regeln, Anwendungs-Capabilities und Via-Ziele werden vollständig angezeigt."},
	{"Groups", "Gruppen"},
	{"Declared membership only. Group and user selectors are not expanded into effective device permissions.", "Nur deklarierte Mitgliedschaft. Gruppen- und Benutzerselektoren werden nicht zu wirksamen Geräteberechtigungen aufgelöst."},
	{"Tags and tag owners", "Tags und Tag-Eigentümer"},
	{"Declared identities allowed to assign tags. Tagged devices belong to their tags; a recorded creator is not their user owner.", "Deklarierte Identitäten für die Zuweisung von Tags. Getaggte Geräte gehören ihren Tags; ein hinterlegter Ersteller ist nicht ihr Benutzer-Eigentümer."},
	{"Automatic route approvals", "Automatische Routengenehmigungen"},
	{"Declared subnet and exit-node auto-approvers. These entries do not prove an approval, access or route reachability.", "Deklarierte Auto-Genehmiger für Subnetze und Exit Nodes. Diese Einträge belegen weder eine Genehmigung noch Zugriff oder Routenerreichbarkeit."},
	{"Host aliases", "Host-Aliase"},
	{"Configured address aliases. This is not a DNS resolution result.", "Konfigurierte Address-Aliase. Dies ist kein Ergebnis einer DNS-Auflösung."},
	{"SSH rules", "SSH-Regeln"},
	{"Configured SSH selectors, local users, actions and check periods. No SSH authorization check was run.", "Konfigurierte SSH-Selektoren, lokale Benutzer, Aktionen und Prüfintervalle. Es wurde keine SSH-Autorisierungsprüfung ausgeführt."},
	{"Device attributes", "Geräteattribute"},
	{"Configured targets, capabilities and address pools. NextDNS profile and metadata settings may affect individual devices.", "Konfigurierte Ziele, Capabilities und Adresspools. NextDNS-Profil- und Metadateneinstellungen können einzelne Geräte beeinflussen."},
	{"Declared policy tests", "Deklarierte Policy-Tests"},
	{"Test declarations from the source. This page does not execute them or report a pass result.", "Testdeklarationen aus der Quelle. Diese Seite führt sie nicht aus und meldet kein erfolgreiches Testergebnis."},
	{"Declared SSH tests", "Deklarierte SSH-Tests"},
	{"SSH test declarations from the source. This page does not execute them or report a pass result.", "SSH-Testdeklarationen aus der Quelle. Diese Seite führt sie nicht aus und meldet kein erfolgreiches Testergebnis."},
	{"Client port randomization", "Zufällige Client-Ports"},
	{"Configured value from the source, not confirmation of client adoption.", "Konfigurierter Wert aus der Quelle, keine Bestätigung der Übernahme durch Clients."},
	{"Additional policy section", "Zusätzlicher Policy-Abschnitt"},
	{"This source field is not interpreted by this view. Its complete value is retained below; engine support is not asserted.", "Dieses Quellfeld wird von der Ansicht nicht interpretiert. Sein vollständiger Wert bleibt unten erhalten; Unterstützung durch die Engine wird nicht behauptet."},
	{"Not interpreted by this view", "Von dieser Ansicht nicht interpretiert"},
	{"Configured action", "Konfigurierte Aktion"},
	{"Sources", "Quellen"},
	{"Destinations", "Ziele"},
	{"Protocol", "Protokoll"},
	{"IP rules", "IP-Regeln"},
	{"Application capabilities", "Anwendungs-Capabilities"},
	{"Via targets", "Via-Ziele"},
	{"Targets", "Zielselektoren"},
	{"Attributes", "Attribute"},
	{"Address pools", "Adresspools"},
	{"Local SSH users", "Lokale SSH-Benutzer"},
	{"SSH check period", "SSH-Prüfintervall"},
	{"Accepted environment variables", "Akzeptierte Umgebungsvariablen"},
	{"Expected accepts", "Erwartete Freigaben"},
	{"Expected denies", "Erwartete Ablehnungen"},
	{"Other source field", "Weiteres Quellfeld"},
	{"Configured source fields", "Konfigurierte Quellfelder"},
	{"Source field", "Quellfeld"},
	{"Configured value", "Konfigurierter Wert"},
	{"Source entry", "Quelleintrag"},
	{"Recorded tagged devices", "Erfasste getaggte Geräte"},
	{"These tags come from the current device store. They are not proof of authorization under the source shown above. Tagged devices belong to their tags, even when a creator user is recorded.", "Diese Tags stammen aus dem aktuellen Gerätestand. Sie belegen keine Autorisierung durch die oben gezeigte Quelle. Getaggte Geräte gehören ihren Tags, auch wenn ein Ersteller-Benutzer hinterlegt ist."},
	{"Owning tags", "Besitzende Tags"},
	{"Policy and DNS", "Policy und DNS"},
	{"Policy reloads do not activate DNS settings saved for the next Meshora start. NextDNS capabilities can customize DNS per device and must be considered separately from the global DNS baseline.", "Policy-Reloads aktivieren keine DNS-Einstellungen, die für den nächsten Meshora-Start gespeichert wurden. NextDNS-Capabilities können DNS pro Gerät anpassen und sind getrennt von der globalen DNS-Grundlage zu betrachten."},
	{"View DNS baseline", "DNS-Grundlage anzeigen"},
	{"View route approvals", "Routengenehmigungen anzeigen"},
	{"Complete policy source", "Vollständiger Policy-Quelltext"},
	{"Original source, including comments, unknown fields and unsupported content. This is source evidence, not a permission evaluation.", "Originalquelle einschließlich Kommentaren, unbekannten Feldern und nicht unterstützten Inhalten. Dies ist ein Quellnachweis, keine Berechtigungsauswertung."},
	{"Show complete policy source", "Vollständigen Policy-Quelltext anzeigen"},
	{"The policy source could not be read completely. No content was silently truncated.", "Die Policy-Quelle konnte nicht vollständig gelesen werden. Es wurden keine Inhalte stillschweigend abgeschnitten."},
	{"The configured policy source mode is not supported by this view.", "Der konfigurierte Policy-Quellmodus wird von dieser Ansicht nicht unterstützt."},
	{"The policy exceeds the display size limit. Inspect the complete source directly; no partial policy is shown.", "Die Policy überschreitet die Größenbegrenzung der Anzeige. Prüfe die vollständige Quelle direkt; es wird keine unvollständige Policy angezeigt."},
	{"The source cannot be structured as a HuJSON policy object. The complete source remains available below.", "Die Quelle kann nicht als HuJSON-Policy-Objekt gegliedert werden. Der vollständige Quelltext bleibt unten verfügbar."},
	{"Show effective DNS routes", "Wirksame DNS-Routen anzeigen"},
	{"MagicDNS, nameservers, search domains and Split DNS", "MagicDNS, Nameserver, Suchdomains und Split-DNS"},
	{"Configuration source", "Konfigurationsquelle"},
	{"The YAML file is authoritative. The dashboard edits that file; environment overrides take precedence and disable editing. DNS changes require a Meshora restart. SIGHUP reloads policy only.", "Die YAML-Datei ist maßgeblich. Das Dashboard bearbeitet diese Datei; Umgebungsvariablen haben Vorrang und sperren die Bearbeitung. DNS-Änderungen erfordern einen Neustart von Meshora. SIGHUP lädt nur die Policy neu."},
	{"Configuration checks do not test DNS resolution. Client DNS preferences, network access and policy can affect the result.", "Konfigurationsprüfungen testen keine DNS-Auflösung. DNS-Einstellungen des Clients, Netzwerkzugriff und Policy können das Ergebnis beeinflussen."},
	{"Saved DNS settings differ from the running configuration. Restart required; saved values are not active yet.", "Gespeicherte DNS-Einstellungen weichen von der laufenden Konfiguration ab. Neustart erforderlich; die gespeicherten Werte sind noch nicht aktiv."}, //nolint:misspell // German UI translation.
	{"Read-only DNS settings", "Schreibgeschützte DNS-Einstellungen"},
	{"The YAML file is unavailable or cannot be edited safely. Aliases, anchors, merge keys and multiple documents in editable DNS settings require editing the file directly.", "Die YAML-Datei ist nicht verfügbar oder kann nicht sicher bearbeitet werden. Aliase, Anker, Merge-Schlüssel und mehrere Dokumente in bearbeitbaren DNS-Einstellungen müssen direkt in der Datei bearbeitet werden."},
	{"DNS environment overrides take precedence over the YAML file. Remove them and restart Meshora before editing DNS here.", "DNS-Umgebungsvariablen haben Vorrang vor der YAML-Datei. Entferne sie und starte Meshora neu, bevor du DNS hier bearbeitest."},
	{"MagicDNS base domain", "MagicDNS-Basisdomain"},
	{"Override local DNS", "Lokale DNS-Einstellungen überschreiben"},
	{"Global nameservers", "Globale Nameserver"},
	{"Search domains", "Suchdomains"},
	{"Split DNS", "Split-DNS"},
	{"Matches running configuration", "Entspricht der laufenden Konfiguration"}, //nolint:misspell // German UI translation.
	{"Unavailable", "Nicht verfügbar"},
	{"Running and saved DNS settings", "Laufende und gespeicherte DNS-Einstellungen"},
	{"Running values were loaded at startup. Saved values are read from the current YAML file.", "Laufende Werte wurden beim Start geladen. Gespeicherte Werte werden aus der aktuellen YAML-Datei gelesen."},
	{"DNS settings", "DNS-Einstellungen"},
	{"Setting", "Einstellung"},
	{"Running", "Laufend"},
	{"Saved for next start", "Für nächsten Start gespeichert"},
	{"Activation", "Aktivierung"},
	{"Effective client DNS baseline", "Wirksame DNS-Grundkonfiguration für Clients"},
	{"This is the current server-side DNS baseline, including generated MagicDNS reverse zones. Policy may customize NextDNS per device. This does not confirm that a client applied it or resolved a name.", "Dies ist die aktuelle serverseitige DNS-Grundkonfiguration einschließlich erzeugter MagicDNS-Reverse-Zonen. Die Policy kann NextDNS pro Gerät anpassen. Das bestätigt weder die Übernahme durch einen Client noch eine erfolgreiche Namensauflösung."},
	{"Override nameservers", "Vorgegebene Nameserver"},
	{"Fallback nameservers", "Fallback-Nameserver"},
	{"Effective search domains", "Wirksame Suchdomains"},
	{"Active extra records", "Aktive zusätzliche Records"},
	{"Effective DNS routes", "Wirksame DNS-Routen"},
	{"Domain", "Domain"},
	{"Nameservers or local resolution", "Nameserver oder lokale Auflösung"},
	{"Resolved locally by the client", "Lokal durch den Client aufgelöst"},
	{"No client DNS configuration is available from the running server.", "Vom laufenden Server ist keine DNS-Konfiguration für Clients verfügbar."}, //nolint:misspell // German UI translation.
	{"Global nameservers (one per line)", "Globale Nameserver (einer pro Zeile)"},
	{"Search domains (one per line)", "Suchdomains (eine pro Zeile)"},
	{"Split DNS (YAML domain to resolver lists)", "Split-DNS (YAML: Domain zu Resolverlisten)"},
	{"Use IPv4, IPv6 or HTTPS DNS-over-HTTPS URLs for nameservers. Override local DNS requires at least one global nameserver. Split DNS uses lowercase domains without trailing dots; [] means local resolution. Use {} to remove all Split DNS rules.", "Verwende IPv4, IPv6 oder HTTPS-Adressen für DNS-over-HTTPS als Nameserver. Das Überschreiben lokaler DNS-Einstellungen benötigt mindestens einen globalen Nameserver. Split-DNS nutzt kleingeschriebene Domains ohne abschließenden Punkt; [] bedeutet lokale Auflösung. Mit {} entfernst du alle Split-DNS-Regeln."},
	{"Only changed DNS fields are replaced. Extra records, file references, comments and other configuration values are retained. Do not edit the file externally while saving from the dashboard.", "Nur geänderte DNS-Felder werden ersetzt. Zusätzliche Records, Dateiverweise und unveränderte YAML-Knoten mit Kommentaren bleiben erhalten. Bearbeite die Datei nicht gleichzeitig außerhalb des Dashboards."},
	{"Save DNS for next start", "DNS für nächsten Start speichern"},
	{"Edit saved DNS settings", "Gespeicherte DNS-Einstellungen bearbeiten"},
	{"Saving validates configuration and writes the YAML file. It does not activate DNS or test resolution.", "Speichern prüft die Configuration und schreibt die YAML-Datei. Es aktiviert DNS nicht und testet keine Namensauflösung."},
	{"The DNS form is incomplete or contains duplicate fields. Reload the page.", "Das DNS-Formular ist unvollständig oder enthält doppelte Felder. Lade die Seite neu."},
	{"Choose Yes or No for MagicDNS and local DNS override.", "Wähle Ja oder Nein für MagicDNS und das Überschreiben lokaler DNS-Einstellungen."},
	{"Enter a valid MagicDNS base domain without a trailing dot. A base domain is required when MagicDNS is enabled.", "Gib eine gültige MagicDNS-Basisdomain ohne abschließenden Punkt ein. Bei aktiviertem MagicDNS ist eine Basisdomain erforderlich."},
	{"The server hostname must be outside the MagicDNS base domain.", "Der Server-Hostname muss außerhalb der MagicDNS-Basisdomain liegen."},
	{"At least one global nameserver is required when local DNS is overridden.", "Beim Überschreiben lokaler DNS-Einstellungen ist mindestens ein globaler Nameserver erforderlich."},
	{"Enter nameservers as IPv4, IPv6 or HTTPS DNS-over-HTTPS URLs, one per line.", "Gib Nameserver als IPv4, IPv6 oder HTTPS-Adresse für DNS-over-HTTPS ein, einen pro Zeile."},
	{"Enter valid search domains, one per line.", "Gib gültige Suchdomains ein, eine pro Zeile."},
	{"Split DNS must be a YAML mapping from domains to resolver lists. Use [] for local resolution.", "Split-DNS muss eine YAML-Zuordnung von Domains zu Resolverlisten sein. Nutze [] für lokale Auflösung."},
	{"Split DNS needs unique lowercase domains without trailing dots and resolver lists. Use [] for local resolution.", "Split-DNS benötigt eindeutige kleingeschriebene Domains ohne abschließenden Punkt und Resolverlisten. Nutze [] für lokale Auflösung."},
	{"Enter valid IP addresses or HTTPS DNS-over-HTTPS URLs in each Split DNS resolver list.", "Gib gültige IP-Adressen oder HTTPS-Adressen für DNS-over-HTTPS in jeder Split-DNS-Resolverliste ein."},
	{"The DNS configuration cannot be edited safely. Nothing was saved.", "Die DNS-Configuration kann nicht sicher bearbeitet werden. Es wurde nichts gespeichert."},
	{"The configuration file changed since this form was opened. Reload and review the current values before saving.", "Die Konfigurationsdatei wurde seit dem Öffnen dieses Formulars geändert. Lade die Seite neu und prüfe die aktuellen Werte vor dem Speichern."}, //nolint:misspell // German UI translation.
	{"Saving DNS failed. Check file permissions. The previous configuration was retained.", "DNS konnte nicht gespeichert werden. Prüfe die Dateirechte. Die bisherige Configuration bleibt erhalten."},
	{"DNS settings saved. Restart Meshora to activate them. The running DNS configuration has not changed.", "DNS-Einstellungen gespeichert. Starte Meshora zur Aktivierung neu. Die laufende DNS-Configuration wurde nicht geändert."},
	{"Yes", "Ja"},
	{"No", "Nein"},
	{"Routes", "Routen"},
	{"Subnet routes and exit nodes", "Subnetzrouten und Exit Nodes"},
	{"Subnet route", "Subnetzroute"},
	{"Exit node", "Exit Node"},
	{"Advertised", "Beworben"},
	{"Approved", "Genehmigt"},
	{"Available", "Verfügbar"},
	{"Approval policy", "Genehmigungsregel"},
	{"Manual approval required", "Manuelle Genehmigung erforderlich"},
	{"Automatic approval allowed by current policy", "Automatische Genehmigung laut aktueller Policy möglich"},
	{"Not advertised", "Nicht beworben"},
	{"Approve route", "Route genehmigen"},
	{"Revoke approval", "Genehmigung zurücknehmen"},
	{"No routes", "Keine Routen"},
	{"Devices have not advertised any routes and have no saved approvals.", "Geräte bewerben keine Routen und haben keine gespeicherten Genehmigungen."},
	{"Available means advertised and approved. Approval does not prove reachability; connection state, IP forwarding, access policy and client route settings must also be checked.", "Verfügbar bedeutet beworben und genehmigt. Eine Genehmigung belegt keine Erreichbarkeit; Verbindungsstatus, IP-Weiterleitung, Zugriffsregeln und Routeneinstellungen des Clients müssen ebenfalls geprüft werden."},
	{"The original approval source is not recorded. The policy column shows current autoApprovers eligibility, not who granted approval. Revoked routes may be automatically approved again after an advertisement change or policy reload. Exit-node actions apply to both IPv4 and IPv6 default routes.", "Die ursprüngliche Genehmigungsquelle wird nicht gespeichert. Die Policy-Spalte zeigt die aktuelle Berechtigung durch autoApprovers, nicht den Ursprung der Genehmigung. Zurückgenommene Routen können nach einer geänderten Routenankündigung oder dem Neuladen der Policy erneut automatisch genehmigt werden. Exit-Node-Aktionen gelten gemeinsam für IPv4- und IPv6-Standardrouten."},
	{"Verify the intended networks before approving them in route management below. Automatic approval is controlled by the policy's autoApprovers.", "Prüfe die vorgesehenen Netze vor der Genehmigung in der Routenverwaltung unten. Automatische Genehmigung erfolgt über autoApprovers in der Policy."},
	{"Route approval saved. Approval does not prove reachability.", "Routengenehmigung gespeichert. Eine Genehmigung belegt keine Erreichbarkeit."},
	{"Route approval revoked. Matching autoApprovers may approve it again on a later advertisement or policy reload.", "Routengenehmigung zurückgenommen. Passende autoApprovers können sie bei späteren Routenankündigungen oder beim Neuladen der Policy erneut erteilen."},
	{"The device no longer advertises this route. Refresh and review its routes.", "Das Gerät bewirbt diese Route nicht mehr. Lade die Ansicht neu und prüfe die Routen."},
	{"Add device", "Gerät hinzufügen"},
	{"Install the client, connect to this server, then finish registration.", "Installiere den Client, verbinde ihn mit diesem Server und schließe die Registrierung ab."},
	{"1. Choose your platform", "1. Plattform auswählen"},
	{"Platform", "Plattform"},
	{"Choose a platform", "Plattform auswählen"},
	{"Show instructions", "Anleitung anzeigen"},
	{"2. Connect this device", "2. Gerät verbinden"},
	{"Install Tailscale on the device you want to add. Run this command in its terminal; on Windows use PowerShell.", "Installiere Tailscale auf dem Gerät, das du hinzufügen möchtest. Führe diesen Befehl dort im Terminal aus; unter Windows in PowerShell."},
	{"Connection command", "Verbindungsbefehl"},
	{"Server address", "Serveradresse"},
	{"Install the Tailscale app on the device you want to add. Choose a custom coordination server when signing in and enter this server address.", "Installiere die Tailscale-App auf dem Gerät, das du hinzufügen möchtest. Wähle bei der Anmeldung einen eigenen Koordinationsserver und gib diese Serveradresse ein."},
	{"Download Tailscale", "Tailscale herunterladen"},
	{"Copy", "Kopieren"},
	{"You can also select the text and copy it manually.", "Du kannst den Text auch markieren und manuell kopieren."},
	{"Copied.", "Kopiert."},
	{"Copy failed. Select the text and copy it manually.", "Kopieren fehlgeschlagen. Markiere den Text und kopiere ihn manuell."},
	{"Platform instructions", "Anleitung für deine Plattform"},
	{"3. Finish registration", "3. Registrierung abschließen"},
	{"Open the registration link shown by the client. An administrator must review the request, choose its user and approve it under Pending registrations. Keep the client running while you wait.", "Öffne den Registrierungslink, den der Client anzeigt. Ein Administrator muss die Anfrage unter wartenden Registrierungen prüfen, einen Benutzer zuordnen und sie freigeben. Lass den Client während der Wartezeit laufen."},
	{"Open the registration link shown by the client. Sign in with the configured identity provider, review the device and confirm registration. The provider's access rules apply; an admin dashboard login does not register the device.", "Öffne den Registrierungslink, den der Client anzeigt. Melde dich beim konfigurierten Identitätsanbieter an, prüfe das Gerät und bestätige die Registrierung. Die Zugriffsregeln des Anbieters gelten; eine Anmeldung in der Admin-Verwaltung registriert das Gerät nicht."},
	{"4. Check your device", "4. Gerät prüfen"},
	{"After registration, return to the Tailscale app and check its connection state. An administrator can open your device in the device list to check the recorded state and next steps. Registration alone does not prove an active connection or reachability. If the request expired, start sign-in again on the device.", "Kehre nach der Registrierung zur Tailscale-App zurück und prüfe ihren Verbindungsstatus. Ein Administrator kann dein Gerät in der Geräteliste öffnen und den erfassten Status sowie nächste Schritte prüfen. Eine Registrierung allein belegt keine aktive Verbindung oder Erreichbarkeit. Ist die Anfrage abgelaufen, starte die Anmeldung auf dem Gerät erneut."},
	{"Review requests (admin)", "Anfragen prüfen (Admin)"},
	{"Find device (admin)", "Gerät suchen (Admin)"},

	{"Status and next steps", "Zustand und nächste Schritte"},
	{"Connection state unknown. Check the Tailscale client on the device; no cause can be determined from the available data.", "Verbindungszustand unbekannt. Prüfe den Tailscale-Client auf dem Gerät; aus den vorhandenen Daten lässt sich keine Ursache bestimmen."},
	{"Offline: no active connection to the control server is recorded. The cause is unknown. Check whether the device is running, has network access and its Tailscale client is connected.", "Offline: Es ist keine aktive Verbindung zum Kontrollserver erfasst. Die Ursache ist unbekannt. Prüfe, ob das Gerät eingeschaltet ist, Netzwerkzugang hat und sein Tailscale-Client verbunden ist."},
	{"Online: an active connection to the control server is recorded. This does not prove that services or routes are reachable.", "Online: Eine aktive Verbindung zum Kontrollserver ist erfasst. Das belegt nicht die Erreichbarkeit von Diensten oder Routen."},
	{"Device authorization expired. Sign in again through the Tailscale client on this device. Connection state and expiry are separate; the data does not explain why authorization expired.", "Die Geräteautorisierung ist abgelaufen. Melde dich über den Tailscale-Client auf diesem Gerät erneut an. Verbindungszustand und Ablauf sind getrennt; die Daten erklären nicht, warum die Autorisierung abgelaufen ist."},
	{"Routes awaiting approval", "Routen ohne Freigabe"},
	{"These advertised routes are not approved for this device. The reason is unknown. An administrator should verify the intended networks and approve them through route management (CLI or API), or check the policy's autoApprovers. This page does not change route approvals.", "Diese angekündigten Routen sind für dieses Gerät nicht freigegeben. Der Grund ist unbekannt. Ein Administrator sollte die vorgesehenen Netze prüfen und über die Routenverwaltung (CLI oder API) freigeben oder die autoApprovers der Richtlinie prüfen. Diese Seite ändert keine Routenfreigaben."},
	{"Approved routes do not guarantee reachability. If a connection fails, check the router's connection state, route advertisement, IP forwarding, access policy and the client's route settings.", "Genehmigte Routen garantieren keine Erreichbarkeit. Prüfe bei Verbindungsproblemen den Verbindungszustand des Routers, die Routenankündigung, IP-Weiterleitung, Zugriffsrichtlinie und die Routeneinstellungen des Clients."},
	{"Registration pending", "Registrierung wartet"},
	{"A registration request is open. The available data does not show why it has not finished. Verify the device and intended owner before assigning it, or ask the user to complete sign-in. Reject unexpected requests. If the request expires, start sign-in again on the device.", "Eine Registrierungsanfrage ist offen. Die vorhandenen Daten zeigen nicht, warum sie noch nicht abgeschlossen ist. Prüfe vor der Zuordnung das Gerät und den vorgesehenen Eigentümer oder bitte den Benutzer, die Anmeldung abzuschließen. Lehne unerwartete Anfragen ab. Wenn die Anfrage abläuft, starte die Anmeldung auf dem Gerät erneut."},
	{"Understanding device states", "Gerätezustände verstehen"},
	{"Offline and expired describe different states. Offline does not reveal a cause; check the device, network and Tailscale client. Expired authorization requires signing in again on the device. Open a device for recorded times, route approvals and next steps.", "Offline und abgelaufen beschreiben unterschiedliche Zustände. Offline verrät keine Ursache; prüfe Gerät, Netzwerk und Tailscale-Client. Eine abgelaufene Autorisierung erfordert eine erneute Anmeldung auf dem Gerät. Öffne ein Gerät für erfasste Zeitpunkte, Routenfreigaben und nächste Schritte."},
	{"Review pending requests", "Wartende Anfragen prüfen"},
	{"An open request is not proof of an error. Review its type, device and intended user. Device registration assigns an owner; an SSH check approves one connection. The reason for waiting is unknown. If a request expires, start sign-in or the SSH connection again on the device.", "Eine offene Anfrage belegt keinen Fehler. Prüfe Typ, Gerät und vorgesehenen Benutzer. Eine Geräteregistrierung ordnet einen Eigentümer zu; eine SSH-Prüfung gibt eine einzelne Verbindung frei. Der Grund für das Warten ist unbekannt. Wenn eine Anfrage abläuft, starte die Anmeldung oder SSH-Verbindung auf dem Gerät erneut."},
	{"Node", "Gerät"},
	{"Search devices and combine filters.", "Geräte suchen und Filter kombinieren."},
	{"Search devices", "Geräte suchen"},
	{"All", "Alle"},
	{"User", "Benutzer"},
	{"Not expired", "Nicht abgelaufen"},
	{"Apply filters", "Filter anwenden"},
	{"Reset filters", "Filter zurücksetzen"},
	{"devices shown", "Geräte angezeigt"},
	{"No matching devices", "Keine passenden Geräte"},
	{"Change or reset the filters to see more devices.", "Filter ändern oder zurücksetzen, um weitere Geräte zu sehen."},
	{"Last activity (UTC)", "Letzte Aktivität (UTC)"},
	{"Expiry (UTC)", "Ablaufdatum (UTC)"},
	{"Announced routes", "Angekündigte Routen"},
	{"Approved routes", "Genehmigte Routen"},
	{"Not set", "Nicht gesetzt"},
	{"None", "Keine"},
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
	{"Network", "Netzwerk"},
	{"Skip to content", "Zum Inhalt springen"},
	{"Language", "Sprache"},
	{"Administration", "Verwaltung"},
	{"Identity provider", "Identitätsanbieter"},
	{"Access rules", "Zugriffsrechte"},
	{"Appearance", "Darstellung"},
	{"Redirect URLs", "Rückleitungs-URLs"},
	{"01 / Connection", "01 / Verbindung"},
	{"02 / Permissions", "02 / Berechtigungen"},
	{"03 / Appearance", "03 / Darstellung"},
	{"Connect Authentik to handle device and administrator sign-in.", "Verbinde Authentik für die Anmeldung von Geräten und Administratoren."},
	{"Meshora access", "Meshora-Zugriff"},
	{"Authentik groups allowed to use Meshora", "Authentik-Gruppen mit Meshora-Zugriff"},
	{"Enter exact groups claim values, separated by commas. Membership in any listed group allows OIDC sign-in and device registration. Leave empty to allow all users accepted by the OIDC provider. This does not grant admin access.", "Trage die exakten Werte aus dem groups-Claim ein, durch Kommas getrennt. Die Mitgliedschaft in einer der Gruppen erlaubt die OIDC-Anmeldung und Geräteregistrierung. Leer lassen erlaubt alle vom OIDC-Anbieter zugelassenen Benutzer. Dies erteilt keinen Admin-Zugriff."},
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
