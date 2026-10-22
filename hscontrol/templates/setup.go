package templates

import (
	"html"

	"github.com/chasefleming/elem-go"
	"github.com/chasefleming/elem-go/attrs"
)

// SetupInfo contains public form values only, never passwords or client secrets.
type SetupInfo struct {
	CSRFToken string
	ServerURL string
	Username  string
	Issuer    string
	ClientID  string
	HasSecret bool
	OIDCOpen  bool
	Error     string
}

// InitialSetup is the first-install form, using the existing Meshora design.
func InitialSetup(info SetupInfo) *elem.Element {
	children := []elem.Node{
		pageHead("Willkommen bei Meshora", "Richte deine eigene Instanz ein. Alle Angaben werden dauerhaft gespeichert."),
	}
	if info.Error != "" {
		children = append(children, elem.Div(attrs.Props{attrs.Class: "alert", attrs.Role: "alert"}, elem.Text(info.Error)))
	}

	oidcHint := "Optional: Alle OIDC-Felder leer lassen, um den Anbieter später einzurichten."
	if info.HasSecret {
		oidcHint = "Ein Secret ist gespeichert. Leer lassen, um es beizubehalten."
	}

	oidcProps := attrs.Props{attrs.Class: "card"}
	if info.OIDCOpen || info.Issuer != "" || info.ClientID != "" || info.HasSecret {
		oidcProps["open"] = "true"
	}

	children = append(children, elem.Form(attrs.Props{attrs.Method: "POST", attrs.Action: "/setup", attrs.Class: "stack"},
		adminCSRFInput(info.CSRFToken),
		cardSection("1 · Server", "Die URL, über die Browser und Tailscale-Clients diese Instanz erreichen. Ohne zusätzlichen Pfad.",
			textField("Öffentliche Server-URL", "server_url", info.ServerURL, true)),
		cardSection("2 · Admin-Konto", "Dein lokales Administratorkonto. Verwende ein eigenes Passwort mit mindestens 12 Zeichen.",
			textField("Benutzername", "username", info.Username, true),
			setupPassword("Passwort", "password", true),
			setupPassword("Passwort wiederholen", "password_confirm", true)),
		elem.Details(oidcProps,
			elem.Summary(nil, elem.Text("3 · OpenID Connect (optional)")),
			elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Zum Beispiel Authentik. Client-ID und Secret stammen aus deinem OIDC-Provider.")),
			textField("Issuer-URL", "issuer", info.Issuer, false),
			textField("Client-ID", "client_id", info.ClientID, false),
			setupPassword("Client-Secret", "client_secret", false),
			elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text(oidcHint)),
			elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Redirect-URI beim Provider: öffentliche Server-URL plus /oidc/callback."))),
		elem.Button(attrs.Props{attrs.Type: submitType, attrs.Class: "btn btn-primary"}, elem.Text("Einrichtung abschließen")),
	))

	return adminDocument("/admin", "Einrichtung · Meshora", "app",
		elem.Header(attrs.Props{attrs.Class: "topbar"},
			elem.Div(attrs.Props{attrs.Class: "topbar-inner"}, brand("Einrichtung"))),
		elem.Main(attrs.Props{attrs.Class: "content"}, children...),
		elem.Footer(attrs.Props{attrs.Class: "app-footer"}, poweredBy()...))
}

func setupPassword(label, name string, required bool) *elem.Element {
	props := attrs.Props{attrs.ID: "setup-" + name, attrs.Name: name, attrs.Type: "password", attrs.Autocomplete: "new-password", "maxlength": "1024"}
	if required {
		props[attrs.Required] = "true"
		props["minlength"] = "12"
	}

	return elem.Div(attrs.Props{attrs.Class: "field"},
		elem.Label(attrs.Props{attrs.For: "setup-" + name}, elem.Text(label)), elem.Input(props))
}

// InitialSetupDone refreshes through the handoff until the normal server is ready.
func InitialSetupDone() *elem.Element {
	page := adminDocument("/admin", "Einrichtung abgeschlossen · Meshora", "auth",
		elem.Main(attrs.Props{attrs.Class: "auth-card"}, brand("Einrichtung"),
			elem.H1(nil, elem.Text("Einrichtung abgeschlossen")),
			elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Meshora startet jetzt mit deinen Einstellungen. Du wirst automatisch zur Anmeldung weitergeleitet.")),
			elem.A(attrs.Props{attrs.Href: html.EscapeString("/setup"), attrs.Class: "btn btn-primary"}, elem.Text("Zur Anmeldung"))))
	// The meta refresh works without JavaScript and under the strict admin CSP.
	head, ok := page.Children[0].(*elem.Element)
	if ok {
		head.Children = append(head.Children, elem.Meta(attrs.Props{"http-equiv": "refresh", attrs.Content: "2;url=/setup"}))
	}

	return page
}
