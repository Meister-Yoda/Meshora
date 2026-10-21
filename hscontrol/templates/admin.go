package templates

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"html"
	"strconv"
	"strings"
	"time"

	"github.com/chasefleming/elem-go"
	"github.com/chasefleming/elem-go/attrs"
	"github.com/juanfont/headscale/hscontrol/assets"
)

// The admin dashboard pages use their own stylesheet (assets.AdminCSS) and
// classes only, never style attributes, so the dashboard CSP can forbid
// inline styles. elem-go escapes text nodes but NOT attribute values: any
// attribute carrying user input must go through html.EscapeString.

// AdminCSRFField is the form field carrying the CSRF token on every admin
// dashboard form.
const AdminCSRFField = "csrf_token"

// submitType is the type of every submit button.
const submitType = "submit"

// AdminStylesheetPath is the stylesheet's path below the dashboard base.
const AdminStylesheetPath = "/assets/admin.css"

// adminCSSVersion busts browser caches of the stylesheet, which is served
// with max-age, whenever its content changes.
var adminCSSVersion = func() string {
	sum := sha256.Sum256(assets.AdminCSS)

	return hex.EncodeToString(sum[:4])
}()

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
		brand("Admin"),
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
			attrs.Props{attrs.Type: submitType, attrs.Class: "btn btn-primary btn-block"},
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

	UserCount    int
	NodeCount    int
	OnlineCount  int
	PendingCount int
}

// AdminOverview renders the dashboard start page.
func AdminOverview(info AdminOverviewInfo) *elem.Element {
	onlinePct := 0
	if info.NodeCount > 0 {
		onlinePct = info.OnlineCount * 100 / info.NodeCount
	}

	stats := elem.Section(
		attrs.Props{attrs.Class: "stats"},
		statCard(info.Base+"/users", "Users", info.UserCount, elem.Text("Tailnet users")),
		statCard(info.Base+"/nodes", "Nodes", info.NodeCount, elem.Text("Registered devices")),
		elem.A(
			attrs.Props{attrs.Href: info.Base + "/nodes", attrs.Class: "card card-link"},
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
		statCard(info.Base+"/pending", "Pending", info.PendingCount, elem.Text("Logins in progress")),
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

	return adminShell(info.Base, info.Username, info.CSRFToken, "Overview", nil,
		elem.Div(
			attrs.Props{attrs.Class: "page-head"},
			elem.H1(nil, elem.Text("Overview")),
			elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Your tailnet at a glance.")),
		),
		stats,
		addDevice,
	)
}

// AdminFlash is a one-time message shown after an action.
type AdminFlash struct {
	Error   bool
	Message string
	// Secret is shown once in a copyable block, e.g. a new pre-auth key.
	Secret string
}

// AdminChoice is one <option> of a select.
type AdminChoice struct {
	Value    string
	Label    string
	Selected bool
}

// AdminPageInfo is the data every signed-in page needs.
type AdminPageInfo struct {
	Base      string
	Username  string
	CSRFToken string
	Flash     *AdminFlash
	// Now is the reference time for relative times; zero means time.Now.
	Now time.Time
}

func (p AdminPageInfo) now() time.Time {
	if p.Now.IsZero() {
		return time.Now()
	}

	return p.Now
}

func (p AdminPageInfo) shell(title string, content ...elem.Node) *elem.Element {
	return adminShell(p.Base, p.Username, p.CSRFToken, title, p.Flash, content...)
}

// AdminUserRow is one row of [AdminUsers].
type AdminUserRow struct {
	ID uint
	// Name is the login name, DisplayName the full name; either may be empty.
	Name        string
	DisplayName string
	Email       string
	// Provider is "OIDC" or "CLI".
	Provider  string
	NodeCount int
	CreatedAt time.Time
}

// AdminUsers renders the tailnet user list.
func AdminUsers(info AdminPageInfo, users []AdminUserRow) *elem.Element {
	var body elem.Node = emptyState("No users yet",
		"Users appear here once they sign in with your identity provider.")

	if len(users) > 0 {
		rows := make([]elem.Node, 0, len(users))
		for _, u := range users {
			rows = append(rows, elem.Tr(nil,
				elem.Td(nil, twoLine(userHref(info.Base, u.ID), cmp.Or(u.DisplayName, u.Name), u.Name)),
				elem.Td(nil, textOrDash(u.Email)),
				elem.Td(nil, elem.Span(attrs.Props{attrs.Class: "pill"}, elem.Text(u.Provider))),
				elem.Td(attrs.Props{attrs.Class: "num"}, elem.Text(strconv.Itoa(u.NodeCount))),
				elem.Td(nil, dateCell(u.CreatedAt)),
			))
		}

		body = dataTable("Users", []string{"User", "Email", "Provider", "Nodes", "Created"}, rows)
	}

	create := elem.Section(
		attrs.Props{attrs.Class: "card"},
		elem.H2(nil, elem.Text("Add a user")),
		elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text(
			"Users who sign in with your identity provider are created automatically. "+
				"Add one here to register devices for them before they sign in.",
		)),
		elem.Form(
			attrs.Props{attrs.Class: "form-grid", attrs.Method: "POST", attrs.Action: info.Base + "/users"},
			adminCSRFInput(info.CSRFToken),
			textField("Username", "name", "", true),
			textField("Display name", "display_name", "", false),
			textField("Email", "email", "", false),
			elem.Div(attrs.Props{attrs.Class: "form-actions"}, submitButton("Add user", "btn-primary")),
		),
	)

	return info.shell("Users",
		pageHead("Users", countLabel(len(users), "user", "users")+" in this tailnet."),
		body,
		create,
	)
}

func userHref(base string, id uint) string {
	return base + "/users/" + strconv.FormatUint(uint64(id), 10)
}

func nodeHref(base string, id uint64) string {
	return base + "/nodes/" + strconv.FormatUint(id, 10)
}

// AdminNodeRow is one row of [AdminNodes].
type AdminNodeRow struct {
	ID        uint64
	GivenName string
	Hostname  string
	// Tags owns the node when non-empty (tags-as-identity); Owner is the
	// owning user otherwise.
	Tags  []string
	Owner string
	IPv4  string
	IPv6  string
	// Online is nil when the node's connection state is unknown.
	Online   *bool
	LastSeen time.Time
	// Expiry is zero when the node never expires.
	Expiry  time.Time
	Expired bool
	OS      string
}

// AdminNodes renders the node list.
func AdminNodes(info AdminPageInfo, nodes []AdminNodeRow) *elem.Element {
	var body elem.Node = emptyState("No nodes yet",
		"Nodes appear here once a device has been registered.")

	if len(nodes) > 0 {
		body = nodeTable(info, nodes)
	}

	return info.shell("Nodes",
		pageHead("Nodes", countLabel(len(nodes), "node", "nodes")+" registered."),
		body,
	)
}

func nodeTable(info AdminPageInfo, nodes []AdminNodeRow) *elem.Element {
	now := info.now()

	rows := make([]elem.Node, 0, len(nodes))
	for _, n := range nodes {
		rows = append(rows, elem.Tr(nil,
			elem.Td(nil, twoLine(nodeHref(info.Base, n.ID), cmp.Or(n.GivenName, n.Hostname), n.Hostname)),
			elem.Td(nil, ownerCell(n.Tags, n.Owner)),
			elem.Td(attrs.Props{attrs.Class: "mono"}, addressCell(n.IPv4, n.IPv6)),
			elem.Td(nil, statusCell(n.Online)),
			elem.Td(nil, relCell(n.LastSeen, now)),
			elem.Td(nil, expiryCell(n.Expiry, n.Expired)),
			elem.Td(nil, textOrDash(n.OS)),
		))
	}

	return dataTable("Nodes",
		[]string{"Node", "Owner", "Addresses", "Status", "Last seen", "Expiry", "OS"}, rows)
}

// AdminPendingRow is one row of [AdminPending].
type AdminPendingRow struct {
	AuthID string
	// SSHCheck marks an SSH check-mode request; otherwise it is a node
	// registration.
	SSHCheck   bool
	Hostname   string
	OS         string
	MachineKey string
	CreatedAt  time.Time
}

// AdminPending renders the auth requests that are waiting for a login.
func AdminPending(info AdminPageInfo, pending []AdminPendingRow) *elem.Element {
	var body elem.Node = emptyState("Nothing pending",
		"Devices show up here while their user is signing in.")

	if len(pending) > 0 {
		now := info.now()

		rows := make([]elem.Node, 0, len(pending))
		for _, p := range pending {
			kind := elem.Span(attrs.Props{attrs.Class: "pill pill-accent"}, elem.Text("Registration"))
			if p.SSHCheck {
				kind = elem.Span(attrs.Props{attrs.Class: "pill"}, elem.Text("SSH check"))
			}

			rows = append(rows, elem.Tr(nil,
				elem.Td(nil, kind),
				elem.Td(nil, twoLine("", p.Hostname, "")),
				elem.Td(nil, textOrDash(p.OS)),
				elem.Td(attrs.Props{attrs.Class: "mono"}, textOrDash(p.MachineKey)),
				elem.Td(nil, relCell(p.CreatedAt, now)),
				elem.Td(attrs.Props{attrs.Class: "cell-action"}, elem.A(attrs.Props{
					attrs.Href:  pendingHref(info.Base, p.AuthID),
					attrs.Class: "btn btn-primary btn-sm",
				}, elem.Text("Review"))),
			))
		}

		body = dataTable("Pending",
			[]string{"Type", "Device", "OS", "Machine key", "Started", "Action"}, rows)
	}

	return info.shell("Pending",
		pageHead("Pending", "Logins that have started but not finished yet. They expire on their own."),
		body,
	)
}

func pageHead(title, subtitle string) *elem.Element {
	return elem.Div(
		attrs.Props{attrs.Class: "page-head"},
		elem.H1(nil, elem.Text(title)),
		elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text(subtitle)),
	)
}

func countLabel(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}

	return strconv.Itoa(n) + " " + many
}

// dataTable renders a table in a horizontally scrolling card, so wide tables
// stay usable on narrow screens.
func dataTable(caption string, headers []string, rows []elem.Node) *elem.Element {
	head := make([]elem.Node, 0, len(headers))
	for _, h := range headers {
		head = append(head, elem.Th(attrs.Props{attrs.Scope: "col"}, elem.Text(h)))
	}

	return elem.Div(
		attrs.Props{attrs.Class: "table-wrap"},
		elem.Table(
			attrs.Props{attrs.Class: "table"},
			elem.Caption(attrs.Props{attrs.Class: "sr-only"}, elem.Text(caption)),
			elem.THead(nil, elem.Tr(nil, head...)),
			elem.TBody(nil, rows...),
		),
	)
}

func emptyState(title, hint string) *elem.Element {
	return elem.Div(
		attrs.Props{attrs.Class: "card empty"},
		elem.H2(nil, elem.Text(title)),
		elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text(hint)),
	)
}

func pendingHref(base, authID string) string {
	return base + "/pending/" + html.EscapeString(authID)
}

// twoLine renders a primary label, linked to href when set, with an optional
// muted second line. Both are truncated by CSS, so their full values go into
// title attributes (escaped: elem-go does not escape attribute values).
func twoLine(href, primary, secondary string) *elem.Element {
	mainProps := attrs.Props{attrs.Class: "cell-main", attrs.Title: html.EscapeString(primary)}

	var main *elem.Element

	if href != "" {
		mainProps[attrs.Href] = href
		main = elem.A(mainProps, elem.Text(cmp.Or(primary, "—")))
	} else {
		main = elem.Span(mainProps, elem.Text(cmp.Or(primary, "—")))
	}

	children := []elem.Node{main}
	if secondary != "" && secondary != primary {
		children = append(children, elem.Span(
			attrs.Props{attrs.Class: "cell-sub", attrs.Title: html.EscapeString(secondary)},
			elem.Text(secondary),
		))
	}

	return elem.Div(attrs.Props{attrs.Class: "cell-stack"}, children...)
}

func textOrDash(s string) elem.Node {
	if s == "" {
		return elem.Span(attrs.Props{attrs.Class: "faint"}, elem.Text("—"))
	}

	return elem.Text(s)
}

func ownerCell(tags []string, owner string) elem.Node {
	if len(tags) == 0 {
		return textOrDash(owner)
	}

	pills := make([]elem.Node, 0, len(tags))
	for _, tag := range tags {
		pills = append(pills, elem.Span(attrs.Props{attrs.Class: "pill pill-accent"}, elem.Text(tag)))
	}

	return elem.Div(attrs.Props{attrs.Class: "pills"}, pills...)
}

func addressCell(v4, v6 string) elem.Node {
	if v4 == "" && v6 == "" {
		return textOrDash("")
	}

	children := []elem.Node{}

	for _, ip := range []string{v4, v6} {
		if ip != "" {
			children = append(children, elem.Span(nil, elem.Text(ip)))
		}
	}

	return elem.Div(attrs.Props{attrs.Class: "cell-stack"}, children...)
}

func statusCell(online *bool) elem.Node {
	dot, label := "dot dot-unknown", "Unknown"

	if online != nil {
		if *online {
			dot, label = "dot", "Online"
		} else {
			dot, label = "dot dot-off", "Offline"
		}
	}

	return elem.Span(attrs.Props{attrs.Class: "status"},
		elem.Span(attrs.Props{attrs.Class: dot, "aria-hidden": "true"}),
		elem.Text(label),
	)
}

func dateCell(t time.Time) elem.Node {
	if t.IsZero() {
		return textOrDash("")
	}

	return elem.Time(
		attrs.Props{attrs.Datetime: t.UTC().Format(time.RFC3339), attrs.Title: t.UTC().Format(time.RFC1123)},
		elem.Text(t.Format("2006-01-02")),
	)
}

func relCell(t, now time.Time) elem.Node {
	if t.IsZero() {
		return elem.Span(attrs.Props{attrs.Class: "faint"}, elem.Text("Never"))
	}

	return elem.Time(
		attrs.Props{attrs.Datetime: t.UTC().Format(time.RFC3339), attrs.Title: t.UTC().Format(time.RFC1123)},
		elem.Text(relativeTime(t, now)),
	)
}

func expiryCell(t time.Time, expired bool) elem.Node {
	if expired {
		return elem.Span(attrs.Props{attrs.Class: "pill pill-danger"}, elem.Text("Expired"))
	}

	if t.IsZero() {
		return elem.Span(attrs.Props{attrs.Class: "faint"}, elem.Text("Never"))
	}

	return dateCell(t)
}

// relativeTime formats t relative to now, e.g. "5 min ago" or "in 3 days".
func relativeTime(t, now time.Time) string {
	d := now.Sub(t)

	future := d < 0
	if future {
		d = -d
	}

	var s string

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		s = strconv.Itoa(int(d/time.Minute)) + " min"
	case d < 24*time.Hour:
		s = countLabel(int(d/time.Hour), "hour", "hours")
	default:
		s = countLabel(int(d/(24*time.Hour)), "day", "days")
	}

	if future {
		return "in " + s
	}

	return s + " ago"
}

// adminShell wraps a signed-in page in the top bar and footer.
func adminShell(base, username, csrf, title string, flash *AdminFlash, content ...elem.Node) *elem.Element {
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
				navLink(base+"/users", "Users", title == "Users"),
				navLink(base+"/nodes", "Nodes", title == "Nodes"),
				navLink(base+"/pending", "Pending", title == "Pending"),
				navLink(base+"/keys", "Keys", title == "Keys"),
			),
			elem.Div(
				attrs.Props{attrs.Class: "account"},
				elem.Span(attrs.Props{attrs.Class: "avatar", "aria-hidden": "true"}, elem.Text(initial)),
				elem.Span(attrs.Props{attrs.Class: "account-name"}, elem.Text(username)),
				elem.Form(
					attrs.Props{attrs.Method: "POST", attrs.Action: base + "/logout"},
					adminCSRFInput(csrf),
					elem.Button(
						attrs.Props{attrs.Type: submitType, attrs.Class: "btn btn-ghost btn-sm"},
						elem.Text("Sign out"),
					),
				),
			),
		),
	)

	return adminDocument(base, title+" · Meshora", "app",
		topbar,
		elem.Main(attrs.Props{attrs.Class: "content"}, append([]elem.Node{flashBanner(flash)}, content...)...),
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
			elem.Link(attrs.Props{
				attrs.Rel:  "stylesheet",
				attrs.Href: base + AdminStylesheetPath + "?v=" + adminCSSVersion,
			}),
		),
		elem.Body(attrs.Props{attrs.Class: bodyClass, "translate": "no"}, body...),
	)
}

// brand renders the Meshora logo and name, with an optional tag pill such as
// "Admin". Admin and end-user pages share it.
func brand(tag string) *elem.Element {
	children := []elem.Node{
		elem.Raw(meshoraMark),
		elem.Span(nil, elem.Text("Meshora")),
	}
	if tag != "" {
		children = append(children, elem.Span(attrs.Props{attrs.Class: "brand-tag"}, elem.Text(tag)))
	}

	return elem.Div(attrs.Props{attrs.Class: "brand"}, children...)
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

func statCard(href, label string, value int, hint elem.Node) *elem.Element {
	return elem.A(
		attrs.Props{attrs.Href: href, attrs.Class: "card card-link"},
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
