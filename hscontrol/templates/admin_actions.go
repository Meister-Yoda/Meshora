package templates

import (
	"html"
	"strconv"
	"strings"
	"time"

	"github.com/chasefleming/elem-go"
	"github.com/chasefleming/elem-go/attrs"
)

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
		class, role = "flash flash-error", "alert"
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
		if name == "expiry" {
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
func AdminUserDetail(info AdminPageInfo, user AdminUserRow, nodes []AdminNodeRow) *elem.Element {
	path := "/users/" + strconv.FormatUint(uint64(user.ID), 10)

	var nodeList elem.Node = elem.P(attrs.Props{attrs.Class: "muted"}, i18nText("No devices yet."))
	if len(nodes) > 0 {
		nodeList = nodeTable(info, nodes)
	}

	return info.shell("Users",
		backLink(info.Base+"/users", "Users"),
		pageHeadNodes(elem.Text(user.displayName()), elem.Text(user.Provider+" user")),
		elem.Section(attrs.Props{attrs.Class: "card"}, detailList(
			detail("Username", textOrDash(user.Name)),
			detail("Display name", textOrDash(user.DisplayName)),
			detail("Email", textOrDash(user.Email)),
			detail("Created", dateCell(user.CreatedAt)),
		)),
		elem.H2(attrs.Props{attrs.Class: "section-title"}, i18nText("Devices")),
		nodeList,
		cardSection("Rename", "Changes the username used in ACL policies.",
			postForm(info, path+"/rename", "form-inline",
				textField("New username", "name", user.Name, true),
				submitButton("Rename", "btn-primary"),
			),
		),
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
		elem.Section(attrs.Props{attrs.Class: "card"}, detailList(
			detail("Owner", ownerCell(node.Tags, node.Owner)),
			detail("Status", statusCell(node.Online)),
			detail("Addresses", elem.Span(attrs.Props{attrs.Class: "mono"}, addressCell(node.IPv4, node.IPv6))),
			detail("Last seen", relCell(node.LastSeen, now)),
			detail("Expiry", expiryCell(node.Expiry, node.Expired)),
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
			elem.Div(attrs.Props{attrs.Class: "actions"},
				linkButton(info.Base+path+"/expire", "Expire node…", "btn-ghost"),
				linkButton(info.Base+path+"/delete", "Delete node…", "btn-danger"),
			),
		),
	)
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
			elem.Div(attrs.Props{attrs.Class: "actions"},
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
			elem.Div(attrs.Props{attrs.Class: "actions"},
				postForm(info, c.Action, "form-plain", submitButton(c.Button, "btn-danger")),
				linkButton(info.Base+c.Cancel, "Cancel", "btn-ghost"),
			),
		),
	)
}

// AdminOIDCSettings contains public settings only; secrets never enter templates.
type AdminOIDCSettings struct {
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

	var form elem.Node = elem.P(attrs.Props{attrs.Class: "muted"},
		i18nText("Diese Instanz benötigt eine lesbare YAML-Konfigurationsdatei, um Einstellungen hier zu speichern."))
	if settings.Writable {
		form = postForm(info, "/settings/oidc", "stack",
			textField("Issuer-URL", "issuer", settings.Issuer, true),
			elem.P(attrs.Props{attrs.Class: "muted"}, i18nText("Authentik: https://auth.example.com/application/o/meshora/")),
			textField("Client-ID", "client_id", settings.ClientID, true),
			textField("Authentik-Gruppen mit Admin-Zugriff", "admin_groups", settings.AdminGroups, false),
			textField("Admin roles", "admin_roles", settings.AdminRoles, false),
			textField("Roles claim", "roles_claim", settings.RolesClaim, false),
			elem.P(attrs.Props{attrs.Class: "muted"}, i18nText("Comma-separated admin roles from a signed ID-token claim (default: roles). If both groups and roles are configured, both restrictions must match. Empty groups and roles disable admin OIDC.")),
			elem.P(attrs.Props{attrs.Class: "muted"}, i18nText("Comma-separated groups, e.g. meshora-admins. Device OIDC groups never grant admin access.")),
			elem.Div(attrs.Props{attrs.Class: "field"},
				elem.Label(attrs.Props{attrs.For: "f-client_secret"}, i18nText("Client-Secret")),
				elem.Input(secretProps),
				elem.P(attrs.Props{attrs.ID: "secret-hint", attrs.Class: "muted"}, i18nText(hint)),
			),
			elem.P(attrs.Props{attrs.Class: "muted"},
				i18nText("Speichern ändert die Anmeldung nach einem Neustart. Bestehende Zugriffsbeschränkungen und OIDC-Optionen bleiben erhalten.")),
			submitButton("Speichern", "btn-primary"),
		)
	}

	var buttonForm elem.Node = elem.P(attrs.Props{attrs.Class: "muted"}, i18nText("Zum Speichern wird eine beschreibbare YAML-Configuration benoetigt."))
	if settings.Writable {
		buttonForm = postForm(info, "/settings/oidc-button", "stack",
			textField("Text des OIDC-Login-Buttons", "button_text", settings.OIDCButtonText, false),
			elem.P(attrs.Props{attrs.Class: "muted"}, i18nText("Leer lassen fuer den Standard: Mit Authentik anmelden. Maximal 100 Zeichen. Die Aenderung gilt sofort, ohne Neustart.")),
			submitButton("Button-Text speichern", "btn-primary"),
		)
	}

	return info.shell("Settings",
		pageHead("Einstellungen", "Authentik / OpenID Connect für die Anmeldung deiner Geräte."),
		cardSection("Anmeldestatus", status),
		cardSection("OIDC-Login-Button", "Passe die Beschriftung auf der Admin-Anmeldeseite an.", buttonForm),
		cardSection("Authentik-Provider", "Die Client-Zugangsdaten findest du im OAuth2/OpenID-Connect-Provider von Authentik.", form),
		cardSection("Rückleitungs-URL", "Diese URL in Authentik als Redirect-URI mit Strict-Abgleich eintragen.",
			elem.Pre(attrs.Props{attrs.Class: "codeblock"}, elem.Code(nil, elem.Text(settings.CallbackURL)))),
		cardSection("Admin-Redirect-URI", "In Authentik ebenfalls mit Strict-Abgleich eintragen. Der signierte ID-Token muss den groups-Claim mit den freigegebenen Gruppennamen enthalten.",
			elem.Pre(attrs.Props{attrs.Class: "codeblock"}, elem.Code(nil, elem.Text(settings.AdminCallbackURL)))),
	)
}
