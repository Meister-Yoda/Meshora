package templates

import (
	"html"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/chasefleming/elem-go"
	"github.com/chasefleming/elem-go/attrs"
	"github.com/juanfont/headscale/hscontrol/types"
)

const inputTypeHidden = "hidden"

// Pages and form helpers for the dashboard actions. The same rules as in
// admin.go apply: classes only, and every attribute value carrying user
// input goes through html.EscapeString. Form actions and links are passed as
// paths below the dashboard base and prefixed here.

// flashBanner renders the one-time message of an action, or nothing.
func flashBanner(f *AdminFlash) elem.Node {
	if f == nil {
		return i18nText("")
	}

	class, role := "flash flash-success", "status"
	if f.Error {
		class, role = adminFlashErrorClass, "alert"
	}

	children := []elem.Node{elem.P(nil, i18nText(f.Message))}
	if f.Secret != "" {
		children = append(children, elem.Pre(
			attrs.Props{attrs.Class: "codeblock secret"},
			elem.Code(nil, elem.Text(f.Secret)),
		))
	}

	return elem.Div(attrs.Props{attrs.Class: class, attrs.Role: role}, children...)
}

func textField(label, name, value string, required bool) *elem.Element {
	props := attrs.Props{
		attrs.ID:           "f-" + name,
		attrs.Name:         name,
		attrs.Type:         "text",
		attrs.Value:        html.EscapeString(value),
		attrs.Autocomplete: "off",
	}
	if required {
		props[attrs.Required] = "true"
	}

	return elem.Div(
		attrs.Props{attrs.Class: "field"},
		elem.Label(attrs.Props{attrs.For: "f-" + name}, i18nText(label)),
		elem.Input(props),
	)
}

func selectField(label, name string, choices []AdminChoice, placeholder string) *elem.Element {
	options := make([]elem.Node, 0, len(choices)+1)

	if placeholder != "" {
		option := elem.Option(attrs.Props{attrs.Value: ""}, elem.Text(placeholder))
		option.Children = []elem.Node{i18nText(placeholder)}
		options = append(options, option)
	}

	for _, c := range choices {
		props := attrs.Props{attrs.Value: html.EscapeString(c.Value)}
		if c.Selected {
			props[attrs.Selected] = "true"
		}

		var label elem.Node = elem.Text(c.Label)
		if name == "expiry" || name == "magic_dns" || name == "override_local_dns" {
			label = i18nText(c.Label)
		}

		option := elem.Option(props, elem.Text(c.Label))
		option.Children = []elem.Node{label}
		options = append(options, option)
	}

	return elem.Div(
		attrs.Props{attrs.Class: "field"},
		elem.Label(attrs.Props{attrs.For: "f-" + name}, i18nText(label)),
		elem.Select(attrs.Props{attrs.ID: "f-" + name, attrs.Name: name}, options...),
	)
}

func checkboxField(label, name string) *elem.Element {
	return elem.Label(
		attrs.Props{attrs.Class: "check"},
		elem.Input(attrs.Props{attrs.Type: "checkbox", attrs.Name: name}),
		elem.Span(nil, i18nText(label)),
	)
}

func submitButton(label, variant string) *elem.Element {
	return elem.Button(attrs.Props{attrs.Type: submitType, attrs.Class: "btn " + variant}, i18nText(label))
}

// postForm is a form that POSTs to path with the session's CSRF token.
func postForm(info AdminPageInfo, path, class string, children ...elem.Node) *elem.Element {
	return elem.Form(
		attrs.Props{attrs.Class: class, attrs.Method: "POST", attrs.Action: info.Base + path},
		append([]elem.Node{adminCSRFInput(info.CSRFToken)}, children...)...,
	)
}

func linkButton(href, label, variant string) *elem.Element {
	return elem.A(attrs.Props{attrs.Href: href, attrs.Class: "btn " + variant}, i18nText(label))
}

func backLink(href, label string) *elem.Element {
	return elem.A(attrs.Props{attrs.Href: href, attrs.Class: "back"}, i18nText("← "+label))
}

// detailList renders label/value pairs.
func detailList(pairs ...elem.Node) *elem.Element {
	return elem.Dl(attrs.Props{attrs.Class: "details"}, pairs...)
}

func detail(label string, value elem.Node) elem.Node {
	return elem.Div(nil, elem.Dt(nil, i18nText(label)), elem.Dd(nil, value))
}

func cardSection(title, hint string, children ...elem.Node) *elem.Element {
	head := []elem.Node{elem.H2(nil, i18nText(title))}
	if hint != "" {
		head = append(head, elem.P(attrs.Props{attrs.Class: "muted"}, i18nText(hint)))
	}

	return elem.Section(attrs.Props{attrs.Class: "card"}, append(head, children...)...)
}

// AdminUserDetail renders one user with their nodes and actions.
func AdminUserDetail(info AdminPageInfo, user AdminUserRow, nodes []AdminNodeRow, assignments ...*AdminZoneAssignment) *elem.Element {
	path := "/users/" + strconv.FormatUint(uint64(user.ID), 10)

	var nodeList elem.Node = elem.P(attrs.Props{attrs.Class: "muted"}, i18nText("No devices yet."))
	if len(nodes) > 0 {
		nodeList = nodeTable(info, nodes)
	}

	var assignment *AdminZoneAssignment
	if len(assignments) > 0 {
		assignment = assignments[0]
	}

	adminProps := attrs.Props{attrs.Type: "checkbox", attrs.Name: "is_admin"}
	if user.IsAdmin {
		adminProps[attrs.Checked] = "true"
	}
	return info.shell("Users",
		backLink(info.Base+"/users", "Users"),
		pageHeadNodes(elem.Text(user.displayName()), elem.Text(user.Provider+" user")),
		elem.Section(attrs.Props{attrs.Class: "card"}, detailList(
			detail("UID", elem.Text(strconv.FormatUint(uint64(user.ID), 10))),
			detail("Username", textOrDash(user.Name)),
			detail("Display name", textOrDash(user.DisplayName)),
			detail("Email", textOrDash(user.Email)),
			detail("Created", dateCell(user.CreatedAt)),
		)),
		elem.H2(attrs.Props{attrs.Class: "section-title"}, i18nText("Devices")),
		nodeList,
		adminZoneAssignment(info, path, assignment),
		cardSection("Benutzerprofil", "E-Mail und Anzeigename ändern die UID nicht.",
			postForm(info, path+"/profile", "action-form", textField("Email", "email", user.Email, true), textField("Display name", "display_name", user.DisplayName, false), submitButton("Profil speichern", "btn-primary"))),
		cardSection("Admin-Zugang", "Nur ausdrücklich freigegebene Benutzer dürfen die gesamte Instanz verwalten.",
			postForm(info, path+"/admin", "action-form", elem.Label(attrs.Props{attrs.Class: "check"}, elem.Input(adminProps), elem.Span(nil, elem.Text("Admin-Zugang erlauben"))), submitButton("Admin-Zugang speichern", "btn-primary"))),
		cardSection("Lokale Anmeldung", "Ein zufälliges Startpasswort wird nur einmal angezeigt. Der Benutzer muss es beim ersten Login ändern. OIDC bleibt mit derselben UID verknüpft.",
			postForm(info, path+"/password", "action-form", submitButton("Zufälliges Startpasswort erzeugen", "btn-secondary"))),
		cardSection("Doppelte Konten", "Vorhandene lokale und OIDC-Konten ausdrücklich mit einer gemeinsamen UID verbinden.", linkButton(info.Base+path+"/merge", "Mit einem anderen Konto zusammenführen", "btn-secondary")),
		cardSection("Delete user", "Only possible once the user owns no devices.",
			linkButton(info.Base+path+"/delete", "Delete user…", "btn-danger"),
		),
	)
}

func (u AdminUserRow) displayName() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}

	return u.Name
}

// AdminNodeExtra holds node details only shown on [AdminNodeDetail].
type AdminNodeExtra struct {
	Zone           *AdminZoneAssignment
	Routes         []AdminRouteRow
	MachineKey     string
	RegisterMethod string
	CreatedAt      time.Time
}

// AdminNodeDetail renders one node with its actions.
func AdminNodeDetail(info AdminPageInfo, node AdminNodeRow, extra AdminNodeExtra) *elem.Element {
	path := "/nodes/" + strconv.FormatUint(node.ID, 10)
	now := info.now()

	tagsHint := "Tagging hands the node over to its tags: it no longer belongs to a user. " +
		"Tags must be allowed by tagOwners in the policy."
	if len(node.Tags) > 0 {
		tagsHint = "Tagged nodes always keep at least one tag."
	}

	return info.shell("Nodes",
		backLink(info.Base+"/nodes", "Nodes"),
		pageHeadNodes(elem.Text(node.GivenName), elem.Text(node.Hostname)),
		nodeDiagnostics(node),
		adminRouteSection(info, extra.Routes),
		adminZoneAssignment(info, path, extra.Zone),
		elem.Section(attrs.Props{attrs.Class: "card"}, detailList(
			detail("Owner", ownerCell(node.Tags, node.Owner)),
			detail("Status", statusCell(node.Online)),
			detail("Addresses", elem.Span(attrs.Props{attrs.Class: "mono"}, addressCell(node.IPv4, node.IPv6))),
			detail("Last seen", relCell(node.LastSeen, now)),
			detail("Expiry", expiryCell(node.Expiry, node.Expired)),
			detail("Last activity (UTC)", exactNodeTime(node.LastSeen)),
			detail("Expiry (UTC)", exactNodeTime(node.Expiry)),
			detail("Announced routes", routeList(node.AnnouncedRoutes)),
			detail("Approved routes", routeList(node.ApprovedRoutes)),
			detail("OS", textOrDash(node.OS)),
			detail("Machine key", elem.Span(attrs.Props{attrs.Class: "mono"}, textOrDash(extra.MachineKey))),
			detail("Registered", dateCell(extra.CreatedAt)),
			detail("Registered via", textOrDash(extra.RegisterMethod)),
		)),
		cardSection("Rename", "Sets the name the node has in MagicDNS.",
			postForm(info, path+"/rename", "form-inline",
				textField("New name", "name", node.GivenName, true),
				submitButton("Rename", "btn-primary"),
			),
		),
		cardSection("Tags", tagsHint,
			postForm(info, path+"/tags", "form-inline",
				textField("Tags, comma separated", "tags", strings.Join(node.Tags, ", "), true),
				submitButton("Set tags", "btn-primary"),
			),
		),
		cardSection("Expire or delete", "",
			elem.Div(attrs.Props{attrs.Class: adminActionsClass},
				linkButton(info.Base+path+"/expire", "Expire node…", "btn-ghost"),
				linkButton(info.Base+path+"/delete", "Delete node…", "btn-danger"),
			),
		),
	)
}

// nodeDiagnostics describes recorded state without inferring a failure cause.
func nodeDiagnostics(node AdminNodeRow) elem.Node {
	var connection string

	switch {
	case node.Online == nil:
		connection = "Connection state unknown. Check the Tailscale client on the device; no cause can be determined from the available data."
	case !*node.Online:
		connection = "Offline: no active connection to the control server is recorded. The cause is unknown. Check whether the device is running, has network access and its Tailscale client is connected."
	default:
		connection = "Online: an active connection to the control server is recorded. This does not prove that services or routes are reachable."
	}

	items := []elem.Node{elem.P(nil, i18nText(connection))}
	if node.Expired {
		items = append(items, elem.P(nil, i18nText("Device authorization expired. Sign in again through the Tailscale client on this device. Connection state and expiry are separate; the data does not explain why authorization expired.")))
	}

	var unapproved []string

	for _, route := range node.AnnouncedRoutes {
		if !slices.Contains(node.ApprovedRoutes, route) {
			unapproved = append(unapproved, route)
		}
	}

	if len(unapproved) > 0 {
		items = append(items,
			elem.H3(nil, i18nText("Routes awaiting approval")),
			routeList(unapproved),
			elem.P(nil, i18nText("Verify the intended networks before approving them in route management below. Automatic approval is controlled by the policy's autoApprovers.")),
		)
	}

	if len(node.AnnouncedRoutes) > 0 || len(node.ApprovedRoutes) > 0 {
		items = append(items, elem.P(nil, i18nText("Approved routes do not guarantee reachability. If a connection fails, check the router's connection state, route advertisement, IP forwarding, access policy and the client's route settings.")))
	}

	return cardSection("Status and next steps", "", items...)
}

// AdminPendingReview renders one pending auth request with its decisions.
func AdminPendingReview(info AdminPageInfo, req AdminPendingRow, users []AdminChoice) *elem.Element {
	path := "/pending/" + html.EscapeString(req.AuthID)
	now := info.now()

	kind := "Device registration"
	if req.SSHCheck {
		kind = "SSH check"
	}

	sections := []elem.Node{
		backLink(info.Base+"/pending", "Pending"),
		pageHeadNodes(elem.Text(req.Hostname), i18nText(kind)),
		elem.Section(attrs.Props{attrs.Class: "card"}, detailList(
			detail("Type", i18nText(kind)),
			detail("Device", textOrDash(req.Hostname)),
			detail("OS", textOrDash(req.OS)),
			detail("Machine key", elem.Span(attrs.Props{attrs.Class: "mono"}, textOrDash(req.MachineKey))),
			detail("Started", relCell(req.CreatedAt, now)),
		)),
	}

	reject := postForm(info, path+"/reject", "form-plain", submitButton("Reject", "btn-danger"))

	if req.SSHCheck {
		sections = append(sections, cardSection("Decide",
			"Approving lets this SSH connection through once. The client is waiting for the answer.",
			elem.Div(attrs.Props{attrs.Class: adminActionsClass},
				postForm(info, path+"/approve", "form-plain", submitButton("Approve", "btn-primary")),
				reject,
			),
		))

		return info.shell("Pending", sections...)
	}

	var assign elem.Node = elem.P(attrs.Props{attrs.Class: "muted"},
		i18nText("There are no users yet. Add one on the Users page first."))
	if len(users) > 0 {
		assign = postForm(info, path+"/assign", "form-inline",
			selectField("User", "user_id", users, ""),
			submitButton("Register device", "btn-primary"),
		)
	}

	sections = append(sections,
		cardSection("Registration pending", "A registration request is open. The available data does not show why it has not finished. Verify the device and intended owner before assigning it, or ask the user to complete sign-in. Reject unexpected requests. If the request expires, start sign-in again on the device."),
		cardSection("Assign to a user",
			"Registers the device for this user. It connects as soon as the client notices.",
			assign,
		),
		cardSection("Reject",
			"The client is told the login failed. If it tries again, it shows up here as a new request.",
			reject,
		),
	)

	return info.shell("Pending", sections...)
}

// AdminKeyRow is one pre-auth key of [AdminKeys]. The key itself is never
// stored, only its public prefix.
type AdminKeyRow struct {
	ID     uint64
	Prefix string
	// User is the owner of a user key, or the creator of a tagged key.
	User       string
	Tags       []string
	Reusable   bool
	Ephemeral  bool
	CreatedAt  time.Time
	Expiration time.Time
	// Status is "Active", "Used", "Expired" or "Revoked".
	Status string
}

// AdminKeys renders the pre-auth key list and the form for a new key.
func AdminKeys(info AdminPageInfo, keys []AdminKeyRow, users, expiries []AdminChoice) *elem.Element {
	var body elem.Node = emptyState("No pre-auth keys",
		"Pre-auth keys register devices without an interactive login, e.g. servers.")

	if len(keys) > 0 {
		rows := make([]elem.Node, 0, len(keys))
		for _, k := range keys {
			flags := []elem.Node{}
			if k.Reusable {
				flags = append(flags, elem.Span(attrs.Props{attrs.Class: "pill"}, i18nText("reusable")))
			}

			if k.Ephemeral {
				flags = append(flags, elem.Span(attrs.Props{attrs.Class: "pill"}, i18nText("ephemeral")))
			}

			action := i18nText("")
			if k.Status == "Active" {
				action = linkButton(info.Base+"/keys/"+strconv.FormatUint(k.ID, 10)+"/expire", "Expire…", "btn-ghost btn-sm")
			}

			rows = append(rows, elem.Tr(nil,
				elem.Td(attrs.Props{attrs.Class: "mono"}, elem.Text(k.Prefix)),
				elem.Td(nil, ownerCell(k.Tags, k.User)),
				elem.Td(nil, elem.Div(attrs.Props{attrs.Class: "pills"}, flags...)),
				elem.Td(nil, keyStatus(k.Status)),
				elem.Td(nil, dateCell(k.Expiration)),
				elem.Td(attrs.Props{attrs.Class: "cell-action"}, action),
			))
		}

		body = dataTable("Pre-auth keys",
			[]string{"Key", "Owner", "Options", "Status", "Expires", "Action"}, rows)
	}

	create := cardSection("New pre-auth key",
		"A key with tags registers tagged devices; otherwise devices belong to the chosen user.",
		postForm(info, "/keys", "form-grid",
			selectField("User", "user_id", users, "No user (tags only)"),
			textField("Tags, comma separated", "tags", "", false),
			selectField("Valid for", "expiry", expiries, ""),
			elem.Div(attrs.Props{attrs.Class: "field checks"},
				checkboxField("Reusable", "reusable"),
				checkboxField("Ephemeral devices", "ephemeral"),
			),
			elem.Div(attrs.Props{attrs.Class: "form-actions"}, submitButton("Create key", "btn-primary")),
		),
	)

	return info.shell("Keys",
		pageHead("Pre-auth keys", countLabel(len(keys), "key", "keys")+". Keys are shown in full only once."),
		body,
		create,
	)
}

func keyStatus(status string) elem.Node {
	class := "pill"

	switch status {
	case "Active":
		class = "pill pill-accent"
	case "Expired", "Revoked":
		class = "pill pill-danger"
	}

	return elem.Span(attrs.Props{attrs.Class: class}, i18nText(status))
}

// AdminConfirmInfo describes a destructive action awaiting confirmation.
type AdminConfirmInfo struct {
	// Section is the navigation entry the page belongs to.
	Section string
	Title   string
	// Subject names the affected object, e.g. a node name.
	Subject string
	Message string
	// Action is the POST path, Cancel the path to go back to.
	Action string
	Button string
	Cancel string
}

// AdminConfirm renders the confirmation page of a destructive action.
func AdminConfirm(info AdminPageInfo, c AdminConfirmInfo) *elem.Element {
	return info.shell(c.Section,
		elem.Section(
			attrs.Props{attrs.Class: "card confirm"},
			elem.H1(nil, i18nText(c.Title)),
			elem.P(attrs.Props{attrs.Class: "confirm-subject"}, elem.Text(c.Subject)),
			elem.P(attrs.Props{attrs.Class: "muted"}, i18nText(c.Message)),
			elem.Div(attrs.Props{attrs.Class: adminActionsClass},
				postForm(info, c.Action, "form-plain", submitButton(c.Button, "btn-danger")),
				linkButton(info.Base+c.Cancel, "Cancel", "btn-ghost"),
			),
		),
	)
}

// AdminOIDCSettings contains public settings only; secrets never enter templates.
type AdminOIDCSettings struct {
	UserGroups       string
	Section          string
	AdminGroups      string
	AdminRoles       string
	RolesClaim       string
	OIDCButtonText   string
	AdminCallbackURL string
	Issuer           string
	ClientID         string
	CallbackURL      string
	HasSecret        bool
	Writable         bool
	PendingRestart   bool
}

// AdminSettings renders the identity-provider form in the admin dashboard.
func AdminSettings(info AdminPageInfo, settings AdminOIDCSettings) *elem.Element {
	status := "Noch nicht eingerichtet."
	if settings.Issuer != "" {
		status = "Einstellungen gespeichert."
	}

	if settings.PendingRestart {
		status = "Neustart erforderlich: Die gespeicherten Einstellungen werden beim nächsten Serverstart übernommen."
	}

	hint := "Trage das Client-Secret aus deinem Authentik-Provider ein."
	secretProps := attrs.Props{
		attrs.ID: "f-client_secret", attrs.Name: "client_secret", attrs.Type: "password",
		attrs.Autocomplete: "new-password", "aria-describedby": "secret-hint",
	}

	if settings.HasSecret {
		hint = "Ein Secret ist gespeichert. Leer lassen, um es beizubehalten. Es wird nicht wieder angezeigt."
	} else {
		secretProps[attrs.Required] = "true"
	}

	var form elem.Node = elem.P(attrs.Props{attrs.Class: "muted"}, i18nText("This instance needs a readable YAML configuration file to save settings here."))
	if settings.Writable {
		form = postForm(info, "/settings/oidc", "settings-provider-form",
			elem.Section(attrs.Props{attrs.Class: "card settings-section", attrs.ID: "provider"},
				elem.P(attrs.Props{attrs.Class: "settings-eyebrow"}, i18nText("01 / Connection")),
				elem.H2(nil, i18nText("Identity provider")),
				elem.P(attrs.Props{attrs.Class: "muted"}, i18nText("Connect Authentik to handle device and administrator sign-in.")),
				elem.Div(attrs.Props{attrs.Class: "settings-fields"},
					textField("Issuer URL", "issuer", settings.Issuer, true),
					textField("Client ID", "client_id", settings.ClientID, true)),
				elem.Div(attrs.Props{attrs.Class: "field"},
					elem.Label(attrs.Props{attrs.For: "f-client_secret"}, i18nText("Client secret")),
					elem.Input(secretProps),
					elem.P(attrs.Props{attrs.ID: "secret-hint", attrs.Class: "muted"}, i18nText(hint)))),
			elem.Section(attrs.Props{attrs.Class: "card settings-section", attrs.ID: "access"},
				elem.P(attrs.Props{attrs.Class: "settings-eyebrow"}, i18nText("02 / Permissions")),
				elem.H2(nil, i18nText("Meshora access")),
				textField("Authentik groups allowed to use Meshora", "user_groups", settings.UserGroups, false),
				elem.P(attrs.Props{attrs.Class: "settings-note"}, i18nText("Enter exact groups claim values, separated by commas. Membership in any listed group allows OIDC sign-in and device registration. Leave empty to allow all users accepted by the OIDC provider. This does not grant admin access.")),
				elem.H2(nil, i18nText("Admin access")),
				elem.P(attrs.Props{attrs.Class: "muted"}, i18nText("Choose which Authentik identities can administer this instance.")),
				elem.Div(attrs.Props{attrs.Class: "settings-fields"},
					textField("Authentik groups with admin access", "admin_groups", settings.AdminGroups, false),
					textField("Admin roles", "admin_roles", settings.AdminRoles, false)),
				elem.P(attrs.Props{attrs.Class: "settings-note"}, i18nText("Use comma-separated names. If both groups and roles are set, both must match. Leave both empty to disable admin OIDC.")),
				elem.Details(attrs.Props{attrs.Class: "settings-advanced"},
					elem.Summary(nil, i18nText("Advanced claim mapping")),
					textField("Roles claim", "roles_claim", settings.RolesClaim, false))),
			elem.Div(attrs.Props{attrs.Class: "settings-save"},
				elem.P(attrs.Props{attrs.Class: "muted"}, i18nText("Provider and access changes require a restart.")),
				submitButton("Save sign-in settings", "btn-primary")))
	}

	var buttonForm elem.Node = elem.P(attrs.Props{attrs.Class: "muted"}, i18nText("Zum Speichern wird eine beschreibbare YAML-Configuration benoetigt."))
	if settings.Writable {
		buttonForm = postForm(info, "/settings/oidc-button", "stack",
			textField("Text des OIDC-Login-Buttons", "button_text", settings.OIDCButtonText, false),
			elem.P(attrs.Props{attrs.Class: "muted"}, i18nText("Leer lassen fuer den Standard: Mit Authentik anmelden. Maximal 100 Zeichen. Die Aenderung gilt sofort, ohne Neustart.")),
			submitButton("Button-Text speichern", "btn-primary"),
		)
	}

	section := settings.Section
	if section == "" || section == "callbacks" {
		section = "provider"
	}

	title := "Identity provider"

	content := form

	switch section {
	case "appearance":
		title = "Appearance"

		if f, ok := buttonForm.(*elem.Element); ok {
			f.Children = append(f.Children, elem.Input(attrs.Props{attrs.Type: inputTypeHidden, attrs.Name: "settings_section", attrs.Value: section}))
		}

		content = cardSection("OIDC login button", "Customize the button label on the admin sign-in page.", buttonForm)
	default:
		index := 1
		if section == "access" {
			index = 2
			title = "Access rules"
		}

		if f, ok := form.(*elem.Element); ok && settings.Writable {
			f.Children = []elem.Node{
				f.Children[0], f.Children[index], f.Children[3],
				elem.Input(attrs.Props{attrs.Type: inputTypeHidden, attrs.Name: "settings_section", attrs.Value: section}),
			}
			if section == "access" {
				f.Children = append(f.Children,
					elem.Input(attrs.Props{attrs.Type: inputTypeHidden, attrs.Name: "issuer", attrs.Value: html.EscapeString(settings.Issuer)}),
					elem.Input(attrs.Props{attrs.Type: inputTypeHidden, attrs.Name: "client_id", attrs.Value: html.EscapeString(settings.ClientID)}))
			}
		}
	}

	if section == "provider" {
		content = elem.Div(attrs.Props{attrs.Class: "settings-primary"}, content,
			cardSection("Redirect URLs", "Add these exact URLs to Authentik using strict matching.",
				elem.Div(attrs.Props{attrs.Class: "callback-item"}, elem.H3(nil, i18nText("Administrators")), elem.Pre(attrs.Props{attrs.Class: adminCodeblockClass}, elem.Code(nil, elem.Text(settings.AdminCallbackURL)))),
				elem.Div(attrs.Props{attrs.Class: "callback-item"}, elem.H3(nil, i18nText("Device registration")), elem.Pre(attrs.Props{attrs.Class: adminCodeblockClass}, elem.Code(nil, elem.Text(settings.CallbackURL))))))
	}

	return info.shell("Settings",
		pageHead(title, "Manage sign-in, permissions and appearance."),
		elem.Div(attrs.Props{attrs.Class: "settings-status", attrs.Role: "status"},
			elem.Span(attrs.Props{attrs.Class: "settings-status-label"}, i18nText("Sign-in status")), elem.Span(nil, i18nText(status))),
		elem.Div(attrs.Props{attrs.Class: "settings-page", attrs.ID: "settings-" + section}, content))
}

func exactNodeTime(value time.Time) elem.Node {
	if value.IsZero() {
		return i18nText("Not set")
	}

	return elem.Time(attrs.Props{attrs.Datetime: value.UTC().Format(time.RFC3339)}, elem.Text(value.UTC().Format("2006-01-02 15:04:05 UTC")))
}

func routeList(routes []string) elem.Node {
	if len(routes) == 0 {
		return i18nText("None")
	}

	items := make([]elem.Node, 0, len(routes))
	for _, route := range routes {
		items = append(items, elem.Li(nil, elem.Text(route)))
	}

	return elem.Ul(attrs.Props{attrs.Class: "mono"}, items...)
}

// AdminRouteRow describes recorded route state and current policy eligibility.
type AdminRouteRow struct {
	NodeID                                uint64
	Device, Prefix                        string
	Advertised, Approved, Automatic, Exit bool
}

func routeFlag(value bool) elem.Node {
	if value {
		return i18nText("Yes")
	}

	return i18nText("No")
}

func adminRouteSection(info AdminPageInfo, routes []AdminRouteRow) elem.Node {
	rows := make([]elem.Node, 0, len(routes))
	for _, route := range routes {
		kind := "Subnet route"
		if route.Exit {
			kind = "Exit node"
		}

		mode := "Manual approval required"
		if route.Automatic {
			mode = "Automatic approval allowed by current policy"
		}

		action := i18nText("Not advertised")

		if route.Approved || route.Advertised {
			verb, label := "approve", "Approve route"
			if route.Approved {
				verb, label = "revoke", "Revoke approval"
			}

			action = postForm(info, "/nodes/"+strconv.FormatUint(route.NodeID, 10)+"/routes", "form-inline",
				elem.Input(attrs.Props{attrs.Type: inputTypeHidden, attrs.Name: "route", attrs.Value: html.EscapeString(route.Prefix)}),
				elem.Input(attrs.Props{attrs.Type: inputTypeHidden, attrs.Name: "action", attrs.Value: verb}),
				elem.Input(attrs.Props{attrs.Type: inputTypeHidden, attrs.Name: "return", attrs.Value: "node"}),
				submitButton(label, "btn-ghost"))
		}

		rows = append(rows, elem.Tr(nil,
			elem.Td(nil, twoLine(nodeHref(info.Base, route.NodeID), route.Device, "")),
			elem.Td(nil, elem.Code(nil, elem.Text(route.Prefix))),
			elem.Td(nil, i18nText(kind)),
			elem.Td(nil, routeFlag(route.Advertised)),
			elem.Td(nil, routeFlag(route.Approved)),
			elem.Td(nil, routeFlag(route.Advertised && route.Approved)),
			elem.Td(nil, i18nText(mode)),
			elem.Td(nil, action)))
	}

	var table elem.Node = emptyState("No routes", "Devices have not advertised any routes and have no saved approvals.")
	if len(rows) > 0 {
		table = elem.Div(attrs.Props{attrs.Class: "route-table"}, dataTable("Routes", []string{"Device", "Route", "Type", "Advertised", "Approved", "Available", "Approval policy", "Action"}, rows))
	}

	return cardSection("Subnet routes and exit nodes",
		"Available means advertised and approved. Approval does not prove reachability; connection state, IP forwarding, access policy and client route settings must also be checked.",
		elem.P(attrs.Props{attrs.Class: "muted"}, i18nText("The original approval source is not recorded. The policy column shows current autoApprovers eligibility, not who granted approval. Revoked routes may be automatically approved again after an advertisement change or policy reload. Exit-node actions apply to both IPv4 and IPv6 default routes.")),
		table)
}

// AdminDNSValues contains editable settings, not a claim about DNS reachability.
type AdminDNSValues struct {
	MagicDNS, OverrideLocalDNS        bool
	BaseDomain, Global, Search, Split string
}

type AdminDNSSplit struct{ Domain, Resolvers string }

type AdminDNSSettings struct {
	Active, Saved, Form                                    AdminDNSValues
	Revision, Error, ReadOnlyReason                        string
	Overrides                                              []string
	HasSaved, Writable, PendingRestart                     bool
	EffectiveAvailable, EffectiveMagic                     bool
	EffectiveResolvers, EffectiveFallback, EffectiveSearch []string
	EffectiveRoutes                                        []AdminDNSSplit
	ExtraRecordCount                                       int
}

func dnsMultiline(value string) elem.Node {
	if value == "" {
		return i18nText("None")
	}

	return elem.Pre(attrs.Props{attrs.Class: adminCodeblockClass}, elem.Text(value))
}

func dnsTextArea(label, name, value string) elem.Node {
	return elem.Div(attrs.Props{attrs.Class: "field"},
		elem.Label(attrs.Props{attrs.For: "dns-" + name}, i18nText(label)),
		elem.Textarea(attrs.Props{attrs.ID: "dns-" + name, attrs.Name: name, "rows": "4", "spellcheck": "false"}, elem.Text(value)))
}

func AdminDNS(info AdminPageInfo, dns AdminDNSSettings) *elem.Element {
	content := []elem.Node{
		pageHead("DNS", "MagicDNS, nameservers, search domains and Split DNS"),
		cardSection("Configuration source",
			"The YAML file is authoritative. The dashboard edits that file; environment overrides take precedence and disable editing. DNS changes require a Meshora restart. SIGHUP reloads policy only.",
			elem.P(nil, i18nText("Configuration checks do not test DNS resolution. Client DNS preferences, network access and policy can affect the result."))),
	}
	if dns.Error != "" {
		content = append(content, elem.Div(attrs.Props{attrs.Class: adminFlashErrorClass, attrs.Role: "alert"}, i18nText(dns.Error)))
	}

	if dns.PendingRestart {
		content = append(content, elem.Div(attrs.Props{attrs.Class: adminFlashErrorClass, attrs.Role: "status"}, i18nText("Saved DNS settings differ from the running configuration. Restart required; saved values are not active yet.")))
	}

	if dns.ReadOnlyReason != "" {
		content = append(content, cardSection("Read-only DNS settings", dns.ReadOnlyReason, routeList(dns.Overrides)))
	}

	active, saved := dns.Active, dns.Saved

	type comparison struct {
		label         string
		active, saved elem.Node
		changed       bool
	}

	pairs := []comparison{
		{"MagicDNS", routeFlag(active.MagicDNS), routeFlag(saved.MagicDNS), active.MagicDNS != saved.MagicDNS},
		{"MagicDNS base domain", textOrDash(active.BaseDomain), textOrDash(saved.BaseDomain), active.BaseDomain != saved.BaseDomain},
		{"Override local DNS", routeFlag(active.OverrideLocalDNS), routeFlag(saved.OverrideLocalDNS), active.OverrideLocalDNS != saved.OverrideLocalDNS},
		{"Global nameservers", dnsMultiline(active.Global), dnsMultiline(saved.Global), active.Global != saved.Global},
		{"Search domains", dnsMultiline(active.Search), dnsMultiline(saved.Search), active.Search != saved.Search},
		{"Split DNS", dnsMultiline(active.Split), dnsMultiline(saved.Split), active.Split != saved.Split},
	}

	rows := make([]elem.Node, 0, len(pairs))
	for _, p := range pairs {
		var (
			next  = p.saved
			state = i18nText("Matches running configuration")
		)
		if !dns.HasSaved {
			next = i18nText("Unavailable")
			state = i18nText("Unknown")
		} else if p.changed {
			state = i18nText("Restart required")
		}

		rows = append(rows, elem.Tr(nil, elem.Td(nil, i18nText(p.label)), elem.Td(nil, p.active), elem.Td(nil, next), elem.Td(nil, state)))
	}

	content = append(content, cardSection("Running and saved DNS settings", "Running values were loaded at startup. Saved values are read from the current YAML file.",
		dataTable("DNS settings", []string{"Setting", "Running", "Saved for next start", "Activation"}, rows)))

	if dns.EffectiveAvailable {
		routeRows := make([]elem.Node, 0, len(dns.EffectiveRoutes))
		for _, entry := range dns.EffectiveRoutes {
			resolvers := dnsMultiline(entry.Resolvers)
			if entry.Resolvers == "" {
				resolvers = i18nText("Resolved locally by the client")
			}

			routeRows = append(routeRows, elem.Tr(nil, elem.Td(nil, elem.Text(entry.Domain)), elem.Td(nil, resolvers)))
		}

		content = append(content, cardSection("Effective client DNS baseline",
			"This is the current server-side DNS baseline, including generated MagicDNS reverse zones. Policy may customize NextDNS per device. This does not confirm that a client applied it or resolved a name.",
			detailList(detail("MagicDNS", routeFlag(dns.EffectiveMagic)),
				detail("Override nameservers", routeList(dns.EffectiveResolvers)),
				detail("Fallback nameservers", routeList(dns.EffectiveFallback)),
				detail("Effective search domains", routeList(dns.EffectiveSearch)),
				detail("Active extra records", elem.Text(strconv.Itoa(dns.ExtraRecordCount)))),
			elem.Details(nil, elem.Summary(nil, i18nText("Show effective DNS routes")), dataTable("Effective DNS routes", []string{"Domain", "Nameservers or local resolution"}, routeRows))))
	} else {
		content = append(content, cardSection("Effective client DNS baseline", "No client DNS configuration is available from the running server."))
	}

	if dns.Writable {
		edit := dns.Form
		choices := func(value bool) []AdminChoice {
			return []AdminChoice{{Value: "true", Label: "Yes", Selected: value}, {Value: "false", Label: "No", Selected: !value}}
		}
		form := postForm(info, "/dns", "stack",
			elem.Input(attrs.Props{attrs.Type: inputTypeHidden, attrs.Name: "revision", attrs.Value: html.EscapeString(dns.Revision)}),
			selectField("MagicDNS", "magic_dns", choices(edit.MagicDNS), ""),
			textField("MagicDNS base domain", "base_domain", edit.BaseDomain, false),
			selectField("Override local DNS", "override_local_dns", choices(edit.OverrideLocalDNS), ""),
			dnsTextArea("Global nameservers (one per line)", "global", edit.Global),
			dnsTextArea("Search domains (one per line)", "search", edit.Search),
			dnsTextArea("Split DNS (YAML domain to resolver lists)", "split", edit.Split),
			elem.P(attrs.Props{attrs.Class: "muted"}, i18nText("Use IPv4, IPv6 or HTTPS DNS-over-HTTPS URLs for nameservers. Override local DNS requires at least one global nameserver. Split DNS uses lowercase domains without trailing dots; [] means local resolution. Use {} to remove all Split DNS rules.")),
			elem.Pre(attrs.Props{attrs.Class: adminCodeblockClass}, elem.Text("corp.example.net:\n  - 192.0.2.53\nlocal.example.net: []")),
			elem.P(attrs.Props{attrs.Class: "muted"}, i18nText("Only changed DNS fields are replaced. Extra records, file references, comments and other configuration values are retained. Do not edit the file externally while saving from the dashboard.")),
			submitButton("Save DNS for next start", "btn-primary"))
		content = append(content, cardSection("Edit saved DNS settings", "Saving validates configuration and writes the YAML file. It does not activate DNS or test resolution.", form))
	}

	return info.shell("DNS", content...)
}

type (
	AdminPolicyField struct{ Key, Value string }
	AdminPolicyEntry struct {
		Fields []AdminPolicyField
		Value  string
	}
	AdminPolicySection struct {
		Key, Value string
		Fields     []AdminPolicyField
		Entries    []AdminPolicyEntry
	}
	AdminPolicyTagged struct {
		ID   uint64
		Name string
		Tags []string
	}
	AdminPolicyInfo struct {
		Loaded                                     types.PolicySnapshot
		Source, Reference, Fingerprint, Raw, Error string
		Empty                                      bool
		Sections                                   []AdminPolicySection
		Tagged                                     []AdminPolicyTagged
	}
)

func policySectionDescription(key string) (string, string, bool) {
	switch key {
	case "acls":
		return "Network rules (ACLs)", "Configured source and destination selectors, protocol and action. These rules have not been evaluated by this page.", true
	case "grants":
		return "Network and capability grants", "Configured selectors, IP rules, application capabilities and via targets are shown completely.", true
	case "groups":
		return "Groups", "Declared membership only. Group and user selectors are not expanded into effective device permissions.", true
	case "tagOwners":
		return "Tags and tag owners", "Declared identities allowed to assign tags. Tagged devices belong to their tags; a recorded creator is not their user owner.", true
	case "autoApprovers":
		return "Automatic route approvals", "Declared subnet and exit-node auto-approvers. These entries do not prove an approval, access or route reachability.", true
	case "hosts":
		return "Host aliases", "Configured address aliases. This is not a DNS resolution result.", true
	case "ssh":
		return "SSH rules", "Configured SSH selectors, local users, actions and check periods. No SSH authorization check was run.", true
	case "nodeAttrs":
		return "Device attributes", "Configured targets, capabilities and address pools. NextDNS profile and metadata settings may affect individual devices.", true
	case "tests":
		return "Declared policy tests", "Test declarations from the source. This page does not execute them or report a pass result.", true
	case "sshTests":
		return "Declared SSH tests", "SSH test declarations from the source. This page does not execute them or report a pass result.", true
	case "randomizeClientPort":
		return "Client port randomization", "Configured value from the source, not confirmation of client adoption.", true
	default:
		return "Additional policy section", "This source field is not interpreted by this view. Its complete value is retained below; engine support is not asserted.", false
	}
}

func policyFieldLabel(key string) string {
	switch key {
	case "action":
		return "Configured action"
	case "src":
		return "Sources"
	case "dst":
		return "Destinations"
	case "proto":
		return "Protocol"
	case "ip":
		return "IP rules"
	case "app":
		return "Application capabilities"
	case "via":
		return "Via targets"
	case "target":
		return "Targets"
	case "attr":
		return "Attributes"
	case "ipPool":
		return "Address pools"
	case "users":
		return "Local SSH users"
	case "checkPeriod":
		return "SSH check period"
	case "acceptEnv":
		return "Accepted environment variables"
	case "accept":
		return "Expected accepts"
	case "deny":
		return "Expected denies"
	default:
		return "Other source field"
	}
}

func policyFields(fields []AdminPolicyField, rule bool) elem.Node {
	rows := make([]elem.Node, 0, len(fields))
	for _, field := range fields {
		var label elem.Node = elem.Code(nil, elem.Text(field.Key))
		if rule {
			label = elem.Div(attrs.Props{attrs.Class: "cell-stack"}, i18nText(policyFieldLabel(field.Key)), elem.Code(nil, elem.Text(field.Key)))
		}

		rows = append(rows, elem.Tr(nil, elem.Td(nil, label), elem.Td(nil, dnsMultiline(field.Value))))
	}

	return dataTable("Configured source fields", []string{"Source field", "Configured value"}, rows)
}

func AdminPolicy(info AdminPageInfo, policy AdminPolicyInfo) *elem.Element {
	content := []elem.Node{
		pageHead("Policy", "Read-only policy source overview"),
		cardSection("Evidence and limits",
			"This page reads the current configured source. It does not compile policy, evaluate permissions, run policy tests, reload policy or change configuration.",
			detailList(detail("Source mode", i18nText(policy.Source)),
				detail("Source reference", textOrDash(policy.Reference)),
				detail("Current source SHA-256", textOrDash(policy.Fingerprint))),
			detailList(detail("Loaded revision", elem.Text(strconv.FormatUint(policy.Loaded.Revision, 10))),
				detail("Loaded SHA-256", elem.Text(policy.Loaded.SHA256)),
				detail("Engine acceptance time (UTC)", elem.Text(policy.Loaded.LoadedAt.UTC().Format(time.RFC3339Nano)))),
			elem.P(nil, i18nText("The source and loaded snapshot are separate evidence. The loaded snapshot records engine acceptance, not client delivery or effective access.")),
			linkButton(info.Base+"/policy/edit", "Edit policy", "btn-primary"),
			elem.P(nil, i18nText("Configured rules do not prove allowed access or reachability. Actual evaluation depends on device identity, tags, policy context and current state."))),
	}
	if policy.Error != "" {
		content = append(content, elem.Div(attrs.Props{attrs.Class: adminFlashErrorClass, attrs.Role: "alert"}, i18nText(policy.Error)))
	}

	if policy.Empty && policy.Error == "" {
		content = append(content, cardSection("No explicit policy in the source", "An empty or absent source is not proof of the currently loaded policy or of any device permission."))
	}

	if len(policy.Sections) > 0 {
		content = append(content, cardSection("Configured source sections", "Parsed for display only. Recognized field names do not mean that the source passed policy-engine validation. All source fields remain visible."))
	}

	for _, section := range policy.Sections {
		title, hint, known := policySectionDescription(section.Key)

		children := []elem.Node{elem.P(nil, elem.Code(nil, elem.Text(section.Key)))}
		if !known {
			children = append(children, elem.P(nil, i18nText("Not interpreted by this view")))
		}

		switch {
		case len(section.Fields) > 0:
			children = append(children, policyFields(section.Fields, false))
		case len(section.Entries) > 0:
			for index, entry := range section.Entries {
				body := dnsMultiline(entry.Value)
				if len(entry.Fields) > 0 {
					body = policyFields(entry.Fields, true)
				}

				children = append(children, elem.Details(nil,
					elem.Summary(nil, i18nText("Source entry"), elem.Text(" "+strconv.Itoa(index+1))), body))
			}
		default:
			children = append(children, dnsMultiline(section.Value))
		}

		content = append(content, cardSection(title, hint, children...))
	}

	if len(policy.Tagged) > 0 {
		rows := make([]elem.Node, 0, len(policy.Tagged))
		for _, node := range policy.Tagged {
			rows = append(rows, elem.Tr(nil,
				elem.Td(nil, twoLine(nodeHref(info.Base, node.ID), node.Name, "")),
				elem.Td(nil, routeList(node.Tags))))
		}

		content = append(content, cardSection("Recorded tagged devices",
			"These tags come from the current device store. They are not proof of authorization under the source shown above. Tagged devices belong to their tags, even when a creator user is recorded.",
			dataTable("Recorded tagged devices", []string{"Device", "Owning tags"}, rows)))
	}

	content = append(content, cardSection("Policy and DNS",
		"Policy reloads do not activate DNS settings saved for the next Meshora start. NextDNS capabilities can customize DNS per device and must be considered separately from the global DNS baseline.",
		linkButton(info.Base+"/dns", "View DNS baseline", "btn-ghost"),
		linkButton(info.Base+"/zones#zone-routes", "Zonen und bereitgestellte Netze", "btn-ghost")))
	if policy.Raw != "" {
		content = append(content, cardSection("Complete policy source",
			"Original source, including comments, unknown fields and unsupported content. This is source evidence, not a permission evaluation.",
			elem.Details(nil, elem.Summary(nil, i18nText("Show complete policy source")), dnsMultiline(policy.Raw))))
	}

	return info.shell("Policy", content...)
}

type AdminPolicyEditorInfo struct {
	Source, SourceRevision, SourceText, Text, LoadedText, LoadedHash, Token, Preview, Error, Diagnostics string
	LoadedRevision                                                                                       uint64
	LoadedAt                                                                                             time.Time
	Implicit, Writable, Pending, Validated                                                               bool
}

func AdminPolicyEditor(info AdminPageInfo, editor AdminPolicyEditorInfo) *elem.Element {
	content := []elem.Node{
		backLink(info.Base+"/policy", "Policy"),
		pageHead("Edit policy", "Validate, save and activate are separate actions"),
		cardSection("Loaded policy evidence",
			"This snapshot records the exact input accepted by the running engine. It does not confirm client delivery, allowed access or reachability.",
			detailList(detail("Loaded revision", elem.Text(strconv.FormatUint(editor.LoadedRevision, 10))),
				detail("Loaded SHA-256", elem.Text(editor.LoadedHash)),
				detail("Engine acceptance time (UTC)", elem.Text(editor.LoadedAt.UTC().Format(time.RFC3339Nano))),
				detail("Implicit boot policy", routeFlag(editor.Implicit)),
				detail("Saved source", textOrDash(editor.Source))),
			elem.Details(nil, elem.Summary(nil, i18nText("Show loaded policy text")), dnsMultiline(editor.LoadedText))),
	}
	if editor.Error != "" {
		content = append(content, elem.Div(attrs.Props{attrs.Class: adminFlashErrorClass, attrs.Role: "alert"}, i18nText(editor.Error)))
	}

	if editor.Diagnostics != "" {
		content = append(content, cardSection("Engine diagnostics", "Diagnostic text is shown as text, never executed.", dnsMultiline(editor.Diagnostics)))
	}

	pending := "The saved source matches the loaded text."
	if editor.Pending {
		pending = "The saved source differs from the loaded policy. Saving alone does not activate it."
	}

	content = append(content, cardSection("Saved and loaded state", pending,
		elem.Details(nil, elem.Summary(nil, i18nText("Show current saved source")), dnsMultiline(editor.SourceText))))
	if editor.Writable {
		buttons := []elem.Node{submitButton("1. Validate and preview", "btn-primary")}
		if editor.Validated {
			buttons = append(buttons, elem.Button(attrs.Props{attrs.Type: submitType, attrs.Class: "btn btn-ghost", "formaction": info.Base + "/policy/save"}, i18nText("2. Save validated draft")))
		}

		form := postForm(info, "/policy/validate", "stack",
			elem.Input(attrs.Props{attrs.Type: inputTypeHidden, attrs.Name: "source_revision", attrs.Value: html.EscapeString(editor.SourceRevision)}),
			elem.Input(attrs.Props{attrs.Type: inputTypeHidden, attrs.Name: "loaded_revision", attrs.Value: strconv.FormatUint(editor.LoadedRevision, 10)}),
			elem.Input(attrs.Props{attrs.Type: inputTypeHidden, attrs.Name: "validation_token", attrs.Value: html.EscapeString(editor.Token)}),
			elem.Div(attrs.Props{attrs.Class: "field"},
				elem.Label(attrs.Props{attrs.For: "policy-text"}, i18nText("HuJSON policy draft")),
				elem.Textarea(attrs.Props{attrs.ID: "policy-text", attrs.Name: "policy_text", "rows": "18", "spellcheck": "false"}, elem.Text("\n"+editor.Text))),
			elem.P(attrs.Props{attrs.Class: "muted"}, i18nText("Comments and unknown fields are preserved without JSON reformatting. Duplicate keys must be resolved explicitly. An edited draft must be validated again before saving.")),
			elem.Div(attrs.Props{attrs.Class: adminActionsClass}, buttons...))
		content = append(content, cardSection("Policy draft",
			"Validation uses an isolated instance of the policy engine and its declared tests. It does not replace the active policy. Saving writes the reviewed draft only.", form))

		if editor.Validated {
			preview := dnsMultiline(editor.Preview)
			if editor.Preview == "" {
				preview = i18nText("No source changes.")
			}

			content = append(content, cardSection("Change preview",
				"Minus lines belong to the current source; plus lines belong to the draft. The preview does not rewrite the draft. Validation must be repeated if text or runtime context changes.",
				preview))
		}

		content = append(content, cardSection("Activate saved policy",
			"Activation revalidates the saved source against current users and devices. Invalid input or an engine-switch failure keeps the previous loaded policy. A later route-processing failure is reported as new policy active, follow-up work failed; it is not rolled back.",
			postForm(info, "/policy/activate", "stack",
				elem.Input(attrs.Props{attrs.Type: inputTypeHidden, attrs.Name: "source_revision", attrs.Value: html.EscapeString(editor.SourceRevision)}),
				elem.Input(attrs.Props{attrs.Type: inputTypeHidden, attrs.Name: "loaded_revision", attrs.Value: strconv.FormatUint(editor.LoadedRevision, 10)}),
				submitButton("3. Activate saved source", "btn-primary"))))
	}

	content = append(content, cardSection("Source and concurrency",
		"File and database sources are editable. Writes in this process are serialized and stale source or loaded revisions are rejected. Do not write the source simultaneously through an external editor or another server process. Policy activation does not activate saved DNS settings.",
		linkButton(info.Base+"/dns", "View DNS baseline", "btn-ghost")))

	return info.shell("Policy", content...)
}
