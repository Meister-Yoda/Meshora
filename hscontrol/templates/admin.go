package templates

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"html"
	"slices"
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

const (
	adminCodeblockClass  = "codeblock"
	adminActionsClass    = "actions"
	adminFlashErrorClass = "flash flash-error"
)

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

// meshoraMark uses the generated logo, with a legible wordmark in either theme.
const meshoraMark = `<picture class="brand-logo">` +
	` <source media="(prefers-color-scheme: dark)" srcset="/assets/meshora-logo-dark.png"/>` +
	`<img src="/assets/meshora-logo.png" alt="Meshora" width="192" height="35"/>` +
	`</picture>`

// meshoraBackdrop uses the same four ambient colours as PicGen.
const meshoraBackdrop = `<div class="meshora-backdrop" aria-hidden="true">` +
	`<span class="ambient ambient-blue"></span><span class="ambient ambient-violet"></span>` +
	`<span class="ambient ambient-peach"></span><span class="ambient ambient-green"></span></div>`

// AdminLoginInfo is the data rendered by [AdminLogin].
type AdminLoginInfo struct {
	OIDCEnabled    bool
	OIDCButtonText string
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
		elem.H1(nil, i18nText("Sign in")),
		elem.P(attrs.Props{attrs.Class: "muted"}, i18nText("Use your Meshora admin account.")),
	}

	if info.Error != "" {
		card = append(card, elem.Div(
			attrs.Props{attrs.Class: "alert", attrs.Role: "alert"},
			i18nText(info.Error),
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
			i18nText("Sign in"),
		),
	))

	if info.OIDCEnabled {
		var buttonLabel elem.Node = elem.Text(info.OIDCButtonText)
		if strings.TrimSpace(info.OIDCButtonText) == "" {
			buttonLabel = i18nText("Sign in with Authentik")
		}

		card = append(card, elem.Form(attrs.Props{
			attrs.Method: "POST", attrs.Action: info.Base + "/login/oidc", attrs.Class: "stack",
		}, adminCSRFInput(info.CSRFToken), elem.Button(attrs.Props{
			attrs.Type: submitType, attrs.Class: "btn btn-ghost btn-block",
		}, buttonLabel)))
	}

	if info.OIDCEnabled {
		card = append(card, elem.P(attrs.Props{attrs.Class: "muted"},
			elem.A(attrs.Props{attrs.Href: info.Base + "/login?local=1"}, i18nText("Use local admin login"))))
	}

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
		statCard(info.Base+"/users", "Users", info.UserCount, i18nText("Tailnet users")),
		statCard(info.Base+"/nodes", "Nodes", info.NodeCount, i18nText("Registered devices")),
		elem.A(
			attrs.Props{attrs.Href: info.Base + "/nodes", attrs.Class: "card card-link"},
			elem.Div(attrs.Props{attrs.Class: "stat-label"}, i18nText("Online")),
			elem.Div(attrs.Props{attrs.Class: "stat-value"}, elem.Text(strconv.Itoa(info.OnlineCount))),
			elem.Div(
				attrs.Props{attrs.Class: "stat-hint"},
				elem.Span(attrs.Props{attrs.Class: "dot"}),
				i18nText(strconv.Itoa(onlinePct)+"% of nodes connected"),
			),
			elem.Div(
				attrs.Props{attrs.Class: "meter", attrs.Role: "presentation"},
				elem.Div(attrs.Props{attrs.Class: "meter-fill " + meterWidthClass(onlinePct)}),
			),
		),
		statCard(info.Base+"/pending", "Pending", info.PendingCount, i18nText("Logins in progress")),
	)

	addDevice := elem.Section(
		attrs.Props{attrs.Class: "card"},
		elem.H2(nil, i18nText("Add a device")),
		elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text(
			"Run this on the device. The user then signs in with their identity provider.",
		)),
		elem.Pre(attrs.Props{attrs.Class: adminCodeblockClass}, elem.Code(nil,
			elem.Text("tailscale up --login-server "+info.ServerURL),
		)),
	)

	return adminShell(info.Base, info.Username, info.CSRFToken, "Overview", nil,
		elem.Div(
			attrs.Props{attrs.Class: "page-head"},
			elem.H1(nil, i18nText("Overview")),
			elem.P(attrs.Props{attrs.Class: "muted"}, i18nText("Your tailnet at a glance.")),
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
		elem.H2(nil, i18nText("Add a user")),
		elem.P(attrs.Props{attrs.Class: "muted"}, i18nText(
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
	Expiry          time.Time
	Expired         bool
	OS              string
	AnnouncedRoutes []string
	ApprovedRoutes  []string
}

const statusClass = "status"

// AdminNodeFilters describes shareable, intersecting device filters.
type AdminNodeFilters struct {
	Query, Status, User, Tag, Expiry string
}

func AdminNodes(info AdminPageInfo, nodes []AdminNodeRow) *elem.Element {
	return AdminFilteredNodes(info, nodes, nodes, AdminNodeFilters{})
}

func AdminFilteredNodes(info AdminPageInfo, nodes, all []AdminNodeRow, filters AdminNodeFilters) *elem.Element {
	var body elem.Node = emptyState("No nodes yet", "Nodes appear here once a device has been registered.")
	if len(nodes) > 0 {
		body = nodeTable(info, nodes)
	} else if len(all) > 0 {
		body = emptyState("No matching devices", "Change or reset the filters to see more devices.")
	}

	users, tags := map[string]bool{}, map[string]bool{}

	for _, node := range all {
		if len(node.Tags) == 0 && node.Owner != "" {
			users[node.Owner] = true
		}

		for _, tag := range node.Tags {
			tags[tag] = true
		}
	}

	choices := func(values map[string]bool, selected string) []AdminChoice {
		keys := make([]string, 0, len(values))
		for value := range values {
			keys = append(keys, value)
		}

		slices.Sort(keys)

		result := make([]AdminChoice, 0, len(keys))
		for _, value := range keys {
			result = append(result, AdminChoice{Value: value, Label: value, Selected: value == selected})
		}

		return result
	}

	return info.shell("Nodes",
		pageHead("Nodes", "Search devices and combine filters."),
		linkButton(strings.TrimSuffix(info.Base, "/admin")+"/connect", "Add device", "btn-primary"),
		elem.Form(attrs.Props{attrs.Class: "card form-grid", attrs.Method: "GET", attrs.Action: info.Base + "/nodes"},
			textField("Search devices", "q", filters.Query, false),
			nodeFilterSelect("Status", statusClass, filters.Status, []string{"online", "offline", "unknown"}, []string{"Online", "Offline", "Unknown"}),
			selectField("User", "user", choices(users, filters.User), "All"),
			selectField("Tags", "tag", choices(tags, filters.Tag), "All"),
			nodeFilterSelect("Expiry", "expiry", filters.Expiry, []string{"expired", "valid", "never"}, []string{"Expired", "Not expired", "Never"}),
			elem.Div(attrs.Props{attrs.Class: "form-actions actions"}, submitButton("Apply filters", "btn-primary"), linkButton(info.Base+"/nodes", "Reset filters", "btn-ghost")),
		),
		elem.P(attrs.Props{"role": statusClass}, elem.Text(strconv.Itoa(len(nodes))+" / "+strconv.Itoa(len(all))+" "), i18nText("devices shown")),
		body,
		cardSection("Understanding device states", "Offline and expired describe different states. Offline does not reveal a cause; check the device, network and Tailscale client. Expired authorization requires signing in again on the device. Open a device for recorded times, route approvals and next steps."),
	)
}

func nodeFilterSelect(label, name, selected string, values, labels []string) elem.Node {
	all := elem.Option(attrs.Props{attrs.Value: ""}, elem.Text("All"))
	all.Children = []elem.Node{i18nText("All")}
	options := make([]elem.Node, 0, 1+len(values))
	options = append(options, all)

	for i, value := range values {
		props := attrs.Props{attrs.Value: value}
		if value == selected {
			props[attrs.Selected] = "true"
		}

		option := elem.Option(props, elem.Text(labels[i]))
		option.Children = []elem.Node{i18nText(labels[i])}
		options = append(options, option)
	}

	return elem.Div(attrs.Props{attrs.Class: "field"}, elem.Label(attrs.Props{attrs.For: "f-" + name}, i18nText(label)), elem.Select(attrs.Props{attrs.ID: "f-" + name, attrs.Name: name}, options...))
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
			kind := elem.Span(attrs.Props{attrs.Class: "pill pill-accent"}, i18nText("Registration"))
			if p.SSHCheck {
				kind = elem.Span(attrs.Props{attrs.Class: "pill"}, i18nText("SSH check"))
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
				}, i18nText("Review"))),
			))
		}

		body = dataTable("Pending",
			[]string{"Type", "Device", "OS", "Machine key", "Started", "Action"}, rows)
	}

	return info.shell("Pending",
		pageHead("Pending", "Logins that have started but not finished yet. They expire on their own."),
		body,
		cardSection("Review pending requests", "An open request is not proof of an error. Review its type, device and intended user. Device registration assigns an owner; an SSH check approves one connection. The reason for waiting is unknown. If a request expires, start sign-in or the SSH connection again on the device."),
	)
}

func pageHead(title, subtitle string) *elem.Element {
	return pageHeadNodes(i18nText(title), i18nText(subtitle))
}

func pageHeadNodes(title, subtitle elem.Node) *elem.Element {
	return elem.Div(
		attrs.Props{attrs.Class: "page-head"},
		elem.H1(nil, title),
		elem.P(attrs.Props{attrs.Class: "muted"}, subtitle),
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
		head = append(head, elem.Th(attrs.Props{attrs.Scope: "col"}, i18nText(h)))
	}

	return elem.Div(
		attrs.Props{attrs.Class: "table-wrap", "tabindex": "0", "role": "region", "aria-label": "Scrollable table / Scrollbare Tabelle"},
		elem.Table(
			attrs.Props{attrs.Class: "table"},
			elem.Caption(attrs.Props{attrs.Class: "sr-only"}, i18nText(caption)),
			elem.THead(nil, elem.Tr(nil, head...)),
			elem.TBody(nil, rows...),
		),
	)
}

func emptyState(title, hint string) *elem.Element {
	return elem.Div(
		attrs.Props{attrs.Class: "card empty"},
		elem.H2(nil, i18nText(title)),
		elem.P(attrs.Props{attrs.Class: "muted"}, i18nText(hint)),
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
		return elem.Span(attrs.Props{attrs.Class: "faint"}, i18nText("—"))
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

	return elem.Span(attrs.Props{attrs.Class: statusClass},
		elem.Span(attrs.Props{attrs.Class: dot, "aria-hidden": "true"}),
		i18nText(label),
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
		return elem.Span(attrs.Props{attrs.Class: "faint"}, i18nText("Never"))
	}

	return elem.Time(
		attrs.Props{attrs.Datetime: t.UTC().Format(time.RFC3339), attrs.Title: t.UTC().Format(time.RFC1123)},
		i18nText(relativeTime(t, now)),
	)
}

func expiryCell(t time.Time, expired bool) elem.Node {
	if expired {
		return elem.Span(attrs.Props{attrs.Class: "pill pill-danger"}, i18nText("Expired"))
	}

	if t.IsZero() {
		return elem.Span(attrs.Props{attrs.Class: "faint"}, i18nText("Never"))
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
				elem.Span(attrs.Props{attrs.Class: "brand-tag"}, i18nText("Admin")),
			),
			elem.Div(
				attrs.Props{attrs.Class: "account"},
				languageSwitcher(),
				elem.Span(attrs.Props{attrs.Class: "avatar", "aria-hidden": "true"}, elem.Text(initial)),
				elem.Span(attrs.Props{attrs.Class: "account-name"}, elem.Text(username)),
				elem.Form(
					attrs.Props{attrs.Method: "POST", attrs.Action: base + "/logout"},
					adminCSRFInput(csrf),
					elem.Button(
						attrs.Props{attrs.Type: submitType, attrs.Class: "btn btn-ghost btn-sm"},
						i18nText("Sign out"),
					),
				),
			),
		),
	)

	return adminDocument(base, title+" · Meshora", "app",
		topbar,
		elem.Div(attrs.Props{attrs.Class: "workspace"},
			elem.Aside(attrs.Props{attrs.Class: "sidebar"}, mainNavigation(base, title)),
			elem.Div(attrs.Props{attrs.Class: "workspace-main"},
				elem.Details(attrs.Props{attrs.Class: "mobile-navigation"},
					elem.Summary(nil, i18nText("Navigation"), elem.Span(attrs.Props{"aria-hidden": "true"}, elem.Text("☰"))),
					mainNavigation(base, title)),
				elem.Main(attrs.Props{attrs.Class: "content", attrs.ID: "main-content"}, append([]elem.Node{flashBanner(flash)}, content...)...),
				elem.Footer(attrs.Props{attrs.Class: "app-footer"}, poweredBy()...))),
	)
}

func mainNavigation(base, current string) elem.Node {
	items := []elem.Node{
		elem.P(attrs.Props{attrs.Class: "sidebar-label"}, i18nText("Workspace")),
		navLink(base+"/", "Overview", current == "Overview"),
		navLink(base+"/users", "Users", current == "Users"),
		navLink(base+"/nodes", "Nodes", current == "Nodes"),
		navLink(base+"/routes", "Routes", current == "Routes"),
		navLink(base+"/zones", "Zonen", current == "Zonen"),
		navLink(base+"/dns", "DNS", current == "DNS"),
		navLink(base+"/policy", "Policy", current == "Policy"),
		navLink(base+"/pending", "Pending", current == "Pending"),
		navLink(base+"/keys", "Keys", current == "Keys"),
		elem.P(attrs.Props{attrs.Class: "sidebar-label"}, i18nText("Administration")),
		navLink(base+"/settings", "Settings", current == "Settings"),
	}
	if current == "Settings" {
		items = append(items, elem.Div(attrs.Props{attrs.Class: "settings-subnav"},
			elem.A(attrs.Props{attrs.Href: base + "/settings/provider"}, i18nText("Identity provider")),
			elem.A(attrs.Props{attrs.Href: base + "/settings/access"}, i18nText("Access rules")),
			elem.A(attrs.Props{attrs.Href: base + "/settings/appearance"}, i18nText("Appearance"))))
	}

	return elem.Nav(attrs.Props{attrs.Class: "side-nav", "aria-label": "Main / Hauptmenü"}, items...)
}

func adminDocument(base, title, bodyClass string, body ...elem.Node) *elem.Element {
	if bodyClass == "auth" && len(body) > 0 {
		body = append([]elem.Node{body[0], languageSwitcher()}, body[1:]...)
	}

	return elem.Html(
		attrs.Props{attrs.Lang: "en"},
		elem.Head(
			nil,
			elem.Meta(attrs.Props{attrs.Charset: "UTF-8"}),
			elem.Meta(attrs.Props{attrs.Name: "viewport", attrs.Content: "width=device-width, initial-scale=1"}),
			elem.Meta(attrs.Props{attrs.Name: "color-scheme", attrs.Content: "light dark"}),
			elem.Meta(attrs.Props{attrs.Name: "robots", attrs.Content: "noindex"}),
			elem.Title(nil, elem.Text(title)),
			elem.Link(attrs.Props{attrs.Rel: "icon", attrs.Href: "/favicon.ico?v=meshora"}),
			elem.Link(attrs.Props{
				attrs.Rel:  "stylesheet",
				attrs.Href: base + AdminStylesheetPath + "?v=" + adminCSSVersion,
			}),
		),
		elem.Body(attrs.Props{attrs.Class: bodyClass, "translate": "no"},
			append([]elem.Node{elem.Raw(meshoraBackdrop)}, body...)...),
	)
}

// brand renders the Meshora logo and name, with an optional tag pill such as
// "Admin". Admin and end-user pages share it.
func brand(tag string) *elem.Element {
	children := []elem.Node{
		elem.Raw(meshoraMark),
	}
	if tag != "" {
		children = append(children, elem.Span(attrs.Props{attrs.Class: "brand-tag"}, i18nText(tag)))
	}

	return elem.Div(attrs.Props{attrs.Class: "brand"}, children...)
}

func poweredBy() []elem.Node {
	return []elem.Node{
		i18nText("Meshora · powered by "),
		elem.A(attrs.Props{
			attrs.Href:   "https://github.com/juanfont/headscale",
			attrs.Rel:    "noreferrer noopener",
			attrs.Target: "_blank",
		}, i18nText("Headscale")),
	}
}

func navLink(href, label string, current bool) *elem.Element {
	props := attrs.Props{attrs.Href: href}
	if current {
		props["aria-current"] = "page"
	}

	paths := map[string]string{
		"Overview": "M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z",
		"Users":    "M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2 M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8 M20 21v-2a4 4 0 0 0-3-3.87 M16 3.13a4 4 0 0 1 0 7.75",
		"Nodes":    "M3 4h18v12H3z M8 21h8 M12 16v5",
		"Pending":  "M12 8v4l3 2 M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0",
		"Keys":     "M15 7a5 5 0 1 0 0 10 5 5 0 0 0 0-10 M10 12H2 M5 12v4 M8 12v3",
		"Settings": "M4 6h16 M4 12h16 M4 18h16 M8 3v6 M16 9v6 M10 15v6",
	}
	icon := `<svg class="nav-icon" viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="` + paths[label] + `"/></svg>`

	return elem.A(props, elem.Raw(icon), i18nText(label))
}

func statCard(href, label string, value int, hint elem.Node) *elem.Element {
	return elem.A(
		attrs.Props{attrs.Href: href, attrs.Class: "card card-link"},
		elem.Div(attrs.Props{attrs.Class: "stat-label"}, i18nText(label)),
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
		elem.Label(attrs.Props{attrs.For: name}, i18nText(label)),
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

// AdminEnrollment guides an interactive device sign-in without issuing secrets.
func AdminEnrollment(info AdminPageInfo, serverURL, platform string, oidc bool) *elem.Element {
	shell := func(content ...elem.Node) *elem.Element {
		return adminDocument(info.Base, "Add device · Meshora", "app",
			elem.Header(attrs.Props{attrs.Class: "topbar"}, elem.Div(attrs.Props{attrs.Class: "topbar-inner"}, brand(""), languageSwitcher())),
			elem.Main(attrs.Props{attrs.Class: "content enrollment", attrs.ID: "main-content"}, content...),
			elem.Footer(attrs.Props{attrs.Class: "app-footer"}, poweredBy()...))
	}

	choices := make([]AdminChoice, 0, 5)
	for _, pair := range [][2]string{{"windows", "Windows"}, {"macos", "macOS"}, {"linux", "Linux"}, {"android", "Android"}, {"ios", "iOS"}} {
		choices = append(choices, AdminChoice{Value: pair[0], Label: pair[1], Selected: platform == pair[0]})
	}

	body := make([]elem.Node, 0, 6)

	body = append(body,
		pageHead("Add device", "Install the client, connect to this server, then finish registration."),
		elem.Form(attrs.Props{attrs.Class: "card form-grid", attrs.Method: "GET", attrs.Action: strings.TrimSuffix(info.Base, "/admin") + "/connect"},
			elem.H2(nil, i18nText("1. Choose your platform")),
			selectField("Platform", "platform", choices, "Choose a platform"),
			submitButton("Show instructions", "btn-primary")),
	)
	if platform == "" {
		return shell(body...)
	}

	mobile := platform == "android" || platform == "ios"

	command := "tailscale login --login-server='" + strings.ReplaceAll(serverURL, "'", "'\"'\"'") + "'"
	if platform == "windows" {
		command = "tailscale login --login-server='" + strings.ReplaceAll(serverURL, "'", "''") + "'"
	}

	if platform == "linux" {
		command = "sudo " + command
	}

	heading, instruction, label := "2. Connect this device", "Install Tailscale on the device you want to add. Run this command in its terminal; on Windows use PowerShell.", "Connection command"

	if mobile {
		command = serverURL
		label = "Server address"
		instruction = "Install the Tailscale app on the device you want to add. Choose a custom coordination server when signing in and enter this server address."
	}

	download := externalLink("https://tailscale.com/download", "Download Tailscale")
	download.Children = []elem.Node{i18nText("Download Tailscale")}
	instructions := externalLink("https://tailscale.com/docs/how-to/set-up-custom-control-server", "Platform instructions")
	instructions.Children = []elem.Node{i18nText("Platform instructions")}
	copyField := elem.Input(attrs.Props{attrs.ID: "enrollment-value", attrs.Type: "text", attrs.Readonly: "true", attrs.Value: html.EscapeString(command), attrs.Class: "mono", "spellcheck": "false"})
	body = append(body, elem.Section(attrs.Props{attrs.Class: "card"},
		elem.H2(nil, i18nText(heading)), elem.P(nil, i18nText(instruction)),
		download,
		elem.Div(attrs.Props{attrs.Class: "field"}, elem.Label(attrs.Props{attrs.For: "enrollment-value"}, i18nText(label)), copyField),
		elem.Button(attrs.Props{attrs.Type: "button", attrs.Class: "btn btn-ghost", attrs.ID: "enrollment-copy", attrs.Disabled: "true"}, i18nText("Copy")),
		elem.P(attrs.Props{attrs.ID: "enrollment-copy-status", "role": statusClass, "aria-live": "polite"}, i18nText("You can also select the text and copy it manually.")),
		elem.Span(attrs.Props{attrs.ID: "enrollment-copied", "hidden": "true"}, i18nText("Copied.")),
		elem.Span(attrs.Props{attrs.ID: "enrollment-copy-failed", "hidden": "true"}, i18nText("Copy failed. Select the text and copy it manually.")),
		instructions,
	))

	registration := "Open the registration link shown by the client. An administrator must review the request, choose its user and approve it under Pending registrations. Keep the client running while you wait."
	if oidc {
		registration = "Open the registration link shown by the client. Sign in with the configured identity provider, review the device and confirm registration. The provider's access rules apply; an admin dashboard login does not register the device."
	}

	body = append(body,
		cardSection("3. Finish registration", registration),
		elem.Section(attrs.Props{attrs.Class: "card"}, elem.H2(nil, i18nText("4. Check your device")),
			elem.P(nil, i18nText("After registration, return to the Tailscale app and check its connection state. An administrator can open your device in the device list to check the recorded state and next steps. Registration alone does not prove an active connection or reachability. If the request expired, start sign-in again on the device.")),
			elem.Div(attrs.Props{attrs.Class: adminActionsClass}, linkButton(info.Base+"/pending", "Review requests (admin)", "btn-secondary"), linkButton(info.Base+"/nodes", "Find device (admin)", "btn-primary"))),
		elem.Script(attrs.Props{attrs.Src: html.EscapeString(info.Base + "/assets/enrollment.js"), "defer": "true"}),
	)

	return shell(body...)
}
