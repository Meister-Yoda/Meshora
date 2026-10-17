package templates

import (
	"html"
	"strconv"
	"strings"

	"github.com/chasefleming/elem-go"
	"github.com/chasefleming/elem-go/attrs"
)

// The admin dashboard pages use their own stylesheet (assets.AdminCSS) and
// classes only, never style attributes, so the dashboard CSP can forbid
// inline styles. elem-go escapes text nodes but NOT attribute values: any
// attribute carrying user input must go through html.EscapeString.

// AdminCSRFField is the form field carrying the CSRF token on every admin
// dashboard form.
const AdminCSRFField = "csrf_token"

// AdminStylesheetPath is the stylesheet's path below the dashboard base.
const AdminStylesheetPath = "/assets/admin.css"

// meshoraMark is the Meshora logo: three linked nodes on a gradient tile.
// Presentation attributes only, so it renders under the strict CSP.
const meshoraMark = `<svg class="brand-mark" viewBox="0 0 32 32" aria-hidden="true">` +
	`<defs><linearGradient id="mg" x1="0" y1="0" x2="1" y2="1">` +
	`<stop offset="0" stop-color="#7c7cf0"/><stop offset="1" stop-color="#4f46e5"/>` +
	`</linearGradient></defs>` +
	`<rect width="32" height="32" rx="9" fill="url(#mg)"/>` +
	`<path d="M10 21 16 10l6 11z" fill="none" stroke="#fff" stroke-opacity=".55" stroke-width="1.6" stroke-linejoin="round"/>` +
	`<circle cx="16" cy="10" r="3.2" fill="#fff"/>` +
	`<circle cx="10" cy="21" r="3.2" fill="#fff"/>` +
	`<circle cx="22" cy="21" r="3.2" fill="#fff"/>` +
	`</svg>`

// AdminLoginInfo is the data rendered by [AdminLogin].
type AdminLoginInfo struct {
	// Base is the dashboard's public base path, e.g. "/admin".
	Base      string
	CSRFToken string
	// Username pre-fills the username field after a failed attempt.
	Username string
	// Error is shown above the form when non-empty.
	Error string
}

// AdminLogin renders the web admin dashboard login page.
func AdminLogin(info AdminLoginInfo) *elem.Element {
	card := []elem.Node{
		brand(),
		elem.H1(nil, elem.Text("Sign in")),
		elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Use your Meshora admin account.")),
	}

	if info.Error != "" {
		card = append(card, elem.Div(
			attrs.Props{attrs.Class: "alert", attrs.Role: "alert"},
			elem.Text(info.Error),
		))
	}

	card = append(card, elem.Form(
		attrs.Props{
			attrs.Class:  "stack",
			attrs.Method: "POST",
			attrs.Action: info.Base + "/login",
		},
		adminCSRFInput(info.CSRFToken),
		adminField("Username", "username", "text", info.Username, "username"),
		adminField("Password", "password", "password", "", "current-password"),
		elem.Button(
			attrs.Props{attrs.Type: "submit", attrs.Class: "btn btn-primary btn-block"},
			elem.Text("Sign in"),
		),
	))

	return adminDocument(info.Base, "Sign in · Meshora", "auth",
		elem.Main(attrs.Props{attrs.Class: "auth-card"}, card...),
		elem.P(attrs.Props{attrs.Class: "auth-footer"}, poweredBy()...),
	)
}

// AdminOverviewInfo is the data rendered by [AdminOverview].
type AdminOverviewInfo struct {
	Base      string
	ServerURL string
	Username  string
	CSRFToken string

	UserCount   int
	NodeCount   int
	OnlineCount int
}

// AdminOverview renders the dashboard start page.
func AdminOverview(info AdminOverviewInfo) *elem.Element {
	onlinePct := 0
	if info.NodeCount > 0 {
		onlinePct = info.OnlineCount * 100 / info.NodeCount
	}

	stats := elem.Section(
		attrs.Props{attrs.Class: "stats"},
		statCard("Users", info.UserCount, elem.Text("Tailnet users")),
		statCard("Nodes", info.NodeCount, elem.Text("Registered devices")),
		elem.Div(
			attrs.Props{attrs.Class: "card"},
			elem.Div(attrs.Props{attrs.Class: "stat-label"}, elem.Text("Online")),
			elem.Div(attrs.Props{attrs.Class: "stat-value"}, elem.Text(strconv.Itoa(info.OnlineCount))),
			elem.Div(
				attrs.Props{attrs.Class: "stat-hint"},
				elem.Span(attrs.Props{attrs.Class: "dot"}),
				elem.Text(strconv.Itoa(onlinePct)+"% of nodes connected"),
			),
			elem.Div(
				attrs.Props{attrs.Class: "meter", attrs.Role: "presentation"},
				elem.Div(attrs.Props{attrs.Class: "meter-fill " + meterWidthClass(onlinePct)}),
			),
		),
	)

	addDevice := elem.Section(
		attrs.Props{attrs.Class: "card"},
		elem.H2(nil, elem.Text("Add a device")),
		elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text(
			"Run this on the device. The user then signs in with their identity provider.",
		)),
		elem.Pre(attrs.Props{attrs.Class: "codeblock"}, elem.Code(nil,
			elem.Text("tailscale up --login-server "+info.ServerURL),
		)),
	)

	return adminShell(info.Base, info.Username, info.CSRFToken, "Overview",
		elem.Div(
			attrs.Props{attrs.Class: "page-head"},
			elem.H1(nil, elem.Text("Overview")),
			elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Your tailnet at a glance.")),
		),
		stats,
		addDevice,
	)
}

// adminShell wraps a signed-in page in the top bar and footer.
func adminShell(base, username, csrf, title string, content ...elem.Node) *elem.Element {
	initial := ""
	if r := []rune(username); len(r) > 0 {
		initial = string(r[0])
	}

	topbar := elem.Header(
		attrs.Props{attrs.Class: "topbar"},
		elem.Div(
			attrs.Props{attrs.Class: "topbar-inner"},
			elem.A(attrs.Props{attrs.Href: base + "/", attrs.Class: "brand"},
				elem.Raw(meshoraMark),
				elem.Span(nil, elem.Text("Meshora")),
				elem.Span(attrs.Props{attrs.Class: "brand-tag"}, elem.Text("Admin")),
			),
			elem.Nav(
				attrs.Props{attrs.Class: "nav", "aria-label": "Main"},
				navLink(base+"/", "Overview", title == "Overview"),
			),
			elem.Div(
				attrs.Props{attrs.Class: "account"},
				elem.Span(attrs.Props{attrs.Class: "avatar", "aria-hidden": "true"}, elem.Text(initial)),
				elem.Span(attrs.Props{attrs.Class: "account-name"}, elem.Text(username)),
				elem.Form(
					attrs.Props{attrs.Method: "POST", attrs.Action: base + "/logout"},
					adminCSRFInput(csrf),
					elem.Button(
						attrs.Props{attrs.Type: "submit", attrs.Class: "btn btn-ghost btn-sm"},
						elem.Text("Sign out"),
					),
				),
			),
		),
	)

	return adminDocument(base, title+" · Meshora", "app",
		topbar,
		elem.Main(attrs.Props{attrs.Class: "content"}, content...),
		elem.Footer(attrs.Props{attrs.Class: "app-footer"}, poweredBy()...),
	)
}

func adminDocument(base, title, bodyClass string, body ...elem.Node) *elem.Element {
	return elem.Html(
		attrs.Props{attrs.Lang: "en"},
		elem.Head(
			nil,
			elem.Meta(attrs.Props{attrs.Charset: "UTF-8"}),
			elem.Meta(attrs.Props{attrs.Name: "viewport", attrs.Content: "width=device-width, initial-scale=1"}),
			elem.Meta(attrs.Props{attrs.Name: "color-scheme", attrs.Content: "light dark"}),
			elem.Meta(attrs.Props{attrs.Name: "robots", attrs.Content: "noindex"}),
			elem.Title(nil, elem.Text(title)),
			elem.Link(attrs.Props{attrs.Rel: "icon", attrs.Href: "/favicon.ico"}),
			elem.Link(attrs.Props{attrs.Rel: "stylesheet", attrs.Href: base + AdminStylesheetPath}),
		),
		elem.Body(attrs.Props{attrs.Class: bodyClass, "translate": "no"}, body...),
	)
}

func brand() *elem.Element {
	return elem.Div(
		attrs.Props{attrs.Class: "brand"},
		elem.Raw(meshoraMark),
		elem.Span(nil, elem.Text("Meshora")),
		elem.Span(attrs.Props{attrs.Class: "brand-tag"}, elem.Text("Admin")),
	)
}

func poweredBy() []elem.Node {
	return []elem.Node{
		elem.Text("Meshora · powered by "),
		elem.A(attrs.Props{
			attrs.Href:   "https://github.com/juanfont/headscale",
			attrs.Rel:    "noreferrer noopener",
			attrs.Target: "_blank",
		}, elem.Text("Headscale")),
	}
}

func navLink(href, label string, current bool) *elem.Element {
	props := attrs.Props{attrs.Href: href}
	if current {
		props["aria-current"] = "page"
	}

	return elem.A(props, elem.Text(label))
}

func statCard(label string, value int, hint elem.Node) *elem.Element {
	return elem.Div(
		attrs.Props{attrs.Class: "card"},
		elem.Div(attrs.Props{attrs.Class: "stat-label"}, elem.Text(label)),
		elem.Div(attrs.Props{attrs.Class: "stat-value"}, elem.Text(strconv.Itoa(value))),
		elem.Div(attrs.Props{attrs.Class: "stat-hint"}, hint),
	)
}

// meterWidthClass maps a percentage to the nearest .w-N step in admin.css.
func meterWidthClass(pct int) string {
	step := min(max((pct+5)/10*10, 0), 100)

	return "w-" + strconv.Itoa(step)
}

func adminCSRFInput(token string) *elem.Element {
	return elem.Input(attrs.Props{
		attrs.Type:  "hidden",
		attrs.Name:  AdminCSRFField,
		attrs.Value: html.EscapeString(token),
	})
}

func adminField(label, name, inputType, value, autocomplete string) *elem.Element {
	return elem.Div(
		attrs.Props{attrs.Class: "field"},
		elem.Label(attrs.Props{attrs.For: name}, elem.Text(label)),
		elem.Input(attrs.Props{
			attrs.ID:           name,
			attrs.Name:         name,
			attrs.Type:         inputType,
			attrs.Value:        html.EscapeString(strings.TrimSpace(value)),
			attrs.Required:     "true",
			attrs.Autocomplete: autocomplete,
		}),
	)
}
