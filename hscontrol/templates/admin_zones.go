package templates

import (
	"html"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/chasefleming/elem-go"
	"github.com/chasefleming/elem-go/attrs"
	"github.com/juanfont/headscale/hscontrol/types"
)

const (
	zoneReturnState   = "return"
	zoneBlockedState  = "blocked"
	zoneBlockedLabel  = "Gesperrt"
	zoneActionHeading = "Action"
	zoneSettingsClass = "zone-settings"
	zoneAriaLabel     = "aria-label"
)

type (
	AdminZoneDevice struct{ ID, Name, Zone, Exits string }
	AdminZoneForm   struct {
		ID, Name, Subnets, Pool4, Pool6 string
		AddressRange                    string
		Nodes, Users, Exits             []string
		ReplaceNetwork                  bool
		BlockOwnExits                   bool
	}
	AdminZonesInfo struct {
		Model                              types.NetworkZones
		Selected                           *types.NetworkZone
		ZoneForm                           *AdminZoneForm
		Zones, Users, Nodes, Gateways      []AdminChoice
		Devices                            []AdminZoneDevice
		SourceRevision                     string
		LoadedRevision                     uint64
		Enabled, Active, Pending, Writable bool
		Error                              string
		FilterFrom, FilterTo               string
		SelectedOwnGroup                   string
		OwnAttempt                         *types.OwnExitRule
		PolicyGroups                       []AdminChoice
		OpenZoneEditor                     bool
		LinkAttempt                        bool
		LinkMode, LinkServices             string
		MeshPrefix4, MeshPrefix6           string
	}
)

func zoneHidden(name, value string) elem.Node {
	return elem.Input(attrs.Props{attrs.Type: inputTypeHidden, attrs.Name: name, attrs.Value: html.EscapeString(value)})
}

func zoneField(label, name, id, value string, multiline bool) elem.Node {
	props := attrs.Props{attrs.ID: id, attrs.Name: name}

	var input elem.Node

	if multiline {
		props[attrs.Rows] = "3"
		input = elem.Textarea(props, elem.Text(value))
	} else {
		props[attrs.Type] = "text"
		props[attrs.Value] = html.EscapeString(value)
		input = elem.Input(props)
	}

	return elem.Div(attrs.Props{attrs.Class: "field"}, elem.Label(attrs.Props{attrs.For: id}, elem.Text(label)), input)
}

func zoneSelect(label, name, id string, choices []AdminChoice, selected []string, multiple bool) elem.Node {
	props := attrs.Props{attrs.ID: id, attrs.Name: name}
	if id == "assignment-zone" {
		props[attrs.Required] = "true"
	}
	if multiple {
		props["multiple"] = "true"
		props["size"] = "5"
	}

	var options []elem.Node
	if !multiple && !slices.ContainsFunc(choices, func(c AdminChoice) bool { return c.Value == "" }) {
		options = append(options, elem.Option(attrs.Props{attrs.Value: ""}, elem.Text("Bitte wählen")))
	}

	seen := map[string]bool{}
	for _, choice := range choices {
		seen[choice.Value] = true

		option := attrs.Props{attrs.Value: html.EscapeString(choice.Value)}
		if slices.Contains(selected, choice.Value) {
			option[attrs.Selected] = "true"
		}

		options = append(options, elem.Option(option, elem.Text(choice.Label)))
	}

	for _, value := range selected {
		if !seen[value] {
			options = append(options, elem.Option(attrs.Props{attrs.Value: html.EscapeString(value), attrs.Selected: "true"}, elem.Text("Nicht verfügbar: "+value)))
		}
	}

	return elem.Div(attrs.Props{attrs.Class: "field"}, elem.Label(attrs.Props{attrs.For: id}, elem.Text(label)), elem.Select(props, options...))
}

func zonePanel(title string, children ...elem.Node) elem.Node {
	return elem.Section(attrs.Props{attrs.Class: "zone-panel"}, append([]elem.Node{elem.H2(nil, elem.Text(title))}, children...)...)
}

func AdminZones(info AdminPageInfo, page AdminZonesInfo) *elem.Element {
	content := []elem.Node{elem.Script(attrs.Props{attrs.Src: info.Base + "/assets/zones.js", "defer": "true"}), pageHead("Zonen", "Netzbereiche, Verbindungen und Exit-Berechtigungen gemeinsam verwalten.")}
	if page.Error != "" && page.ZoneForm == nil {
		content = append(content, elem.Div(attrs.Props{attrs.Class: adminFlashErrorClass, attrs.Role: "alert"}, elem.Text(page.Error)))
	}

	status := "Zonenmodus noch nicht aktiv."
	if page.Active {
		status = "Zonenmodus ist aktiv."
	}

	if page.Pending {
		status += " Die angezeigte Einrichtung ist ein gespeicherter Entwurf und weicht von der aktiven Policy ab."
	}

	content = append(content, elem.P(nil, elem.Text(status)), elem.P(nil, elem.Text("Ohne Freigabe ist kein Zugriff erlaubt, auch innerhalb derselben Zone. Verbindungen sind gerichtet; Antwortverkehr ist erlaubt. Exits werden am Client gewählt.")))
	if !page.Writable {
		return info.shell("Zonen", content...)
	}

	form := func(op string, children ...elem.Node) elem.Node {
		fields := make([]elem.Node, 0, 3+len(children))
		fields = append(fields, zoneHidden("op", op), zoneHidden("source_revision", page.SourceRevision), zoneHidden("loaded_revision", strconv.FormatUint(page.LoadedRevision, 10)))

		return postForm(info, "/zones", "action-form", append(fields, children...)...)
	}

	if page.Enabled && page.Pending {
		content = append(content, zonePanel("Entwurf anwenden",
			elem.P(nil, elem.Text("Prüfe die Zonen und Freigaben vor der Aktivierung. Nicht zugeordnete Geräte verlieren ihre Netzwerkfreigaben.")),
			postForm(info, "/zones/activate", "action-form", zoneHidden("source_revision", page.SourceRevision), zoneHidden("loaded_revision", strconv.FormatUint(page.LoadedRevision, 10)), submitButton("Zonen-Policy aktivieren", "btn-primary")),
			linkButton(info.Base+"/policy/edit", "Aktive Policy und Entwurf vergleichen", "btn-secondary")))
	}

	if page.Enabled {
		content = append(content, zoneMatrix(info, page))
	}

	names := map[string]string{}

	var rows []elem.Node

	for _, zone := range page.Model.Zones {
		names[zone.ID] = zone.Name

		ranges := strings.Join(zone.Subnets, ", ")
		if zone.Pool4 != "" {
			ranges += " Mesh IPv4: " + zone.Pool4
		}

		if zone.Pool6 != "" {
			ranges += " Mesh IPv6: " + zone.Pool6
		}

		rows = append(rows, elem.Tr(nil,
			elem.Td(nil, elem.A(attrs.Props{attrs.Href: info.Base + "/zones?zone=" + url.QueryEscape(zone.ID), "data-zone-opener": zone.ID}, elem.Text(zone.Name))),
			elem.Td(nil, elem.Text(strconv.Itoa(len(zone.Nodes))+" Geräte, "+strconv.Itoa(len(zone.Users))+" Benutzer")),
			elem.Td(nil, textOrDash(ranges)), elem.Td(nil, textOrDash(strings.Join(zone.Exits, ", "))), elem.Td(nil,
				elem.A(attrs.Props{attrs.Href: info.Base + "/zones?zone=" + url.QueryEscape(zone.ID), attrs.Class: "btn btn-secondary", "data-zone-opener": zone.ID}, elem.Text("Bearbeiten")))))
	}

	var zoneOverview elem.Node
	if len(rows) > 0 {
		zoneOverview = dataTable("Zonen", []string{"Zone", "Zuordnung", "Adressbereiche", "Erlaubte Exit-Tags", zoneActionHeading}, rows)
	} else {
		zoneOverview = elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Noch keine Zonen vorhanden. Erstelle die erste Zone über Neue Zone."))
	}

	zone := types.NetworkZone{}
	if page.Selected != nil {
		zone = *page.Selected
	}

	name, formID := zone.Name, zone.ID
	addressRange := strings.Join(slices.DeleteFunc([]string{zone.Pool4, zone.Pool6}, func(s string) bool { return s == "" }), ", ")
	exits := zone.Exits

	if attempted := page.ZoneForm; attempted != nil {
		formID, name = attempted.ID, attempted.Name

		addressRange = attempted.AddressRange
		if addressRange == "" {
			addressRange = strings.Join(slices.DeleteFunc([]string{attempted.Pool4, attempted.Pool6}, func(s string) bool { return s == "" }), ", ")
		}

		exits = attempted.Exits
	}

	fields := []elem.Node{
		zoneHidden("zone", formID), zoneHidden("simple_zone", "on"), zoneNameField(name, page.ZoneForm != nil),
		zoneField("IP-Adressbereich für die Vergabe", "address_range", "zone-address-range", addressRange, false),
		elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Leer lassen für den globalen Adresspool. IPv4 und optional IPv6 durch Komma trennen. Bestehende Geräte behalten ihre Adressen.")),
		zonePoolHint("IPv4", page.MeshPrefix4), zonePoolHint("IPv6", page.MeshPrefix6),
		zoneSelect("Erlaubte Exits", "exits", "zone-exits", page.Gateways, exits, true),
		elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Keine Auswahl erlaubt keine Exits. Geräte und Benutzer werden auf ihren jeweiligen Detailseiten einer Zone zugeordnet.")),
	}

	blockOwn := zone.BlockOwnExits
	if page.ZoneForm != nil {
		blockOwn = page.ZoneForm.BlockOwnExits
	}

	blockProps := attrs.Props{attrs.Type: "checkbox", attrs.Name: "block_own_exits"}
	if blockOwn {
		blockProps[attrs.Checked] = "true"
	}

	fields = append(fields, elem.Label(attrs.Props{attrs.Class: "check"}, elem.Input(blockProps), elem.Span(nil, elem.Text("Eigene Exit Nodes für Benutzer dieser Zone sperren"))))
	if !page.Enabled {
		checkbox := attrs.Props{attrs.Type: "checkbox", attrs.Name: "replace_network"}
		if page.ZoneForm != nil && page.ZoneForm.ReplaceNetwork {
			checkbox[attrs.Checked] = "true"
		}

		fields = append(fields, elem.Label(attrs.Props{attrs.Class: "check"}, elem.Input(checkbox), elem.Span(nil, elem.Text("Zonenmodus verwenden und vorhandene ACLs und Grants einschließlich App-Freigaben ersetzen. SSH-Regeln und andere Policy-Einstellungen bleiben erhalten."))))
	}

	fields = append(fields, submitButton("Zonenentwurf speichern", "btn-primary"))

	title := "Zone erstellen"
	if zone.ID != "" {
		title = "Zone bearbeiten: " + zone.Name
	}

	editor := []elem.Node{form("zone", fields...)}
	if zone.ID != "" {
		editor = append(editor, linkButton(info.Base+"/zones?new=1", "Neue Zone", "btn-secondary"),
			form("delete-zone", zoneHidden("zone", zone.ID), elem.P(nil, elem.Text("Löschen entfernt auch die Verbindungen und IPv6-Reservierungen dieser Zone aus dem Entwurf.")), submitButton("Zone und Verbindungen löschen", "btn-danger")))
	}

	owner := formID
	if owner == "" {
		owner = "new"
	}

	dialogProps := attrs.Props{attrs.ID: "zone-editor", attrs.Class: "zone-editor", "aria-labelledby": "zone-editor-title", "data-zone-owner": owner}
	if page.OpenZoneEditor || page.Selected != nil || page.ZoneForm != nil {
		dialogProps["open"] = "true"
	}

	dialogContent := []elem.Node{
		elem.Div(attrs.Props{attrs.Class: "zone-policy-heading"}, elem.H2(attrs.Props{attrs.ID: "zone-editor-title"}, elem.Text(title)),
			elem.Button(attrs.Props{attrs.Type: "button", attrs.Class: "btn btn-secondary", "data-zone-close": "true", "commandfor": "zone-editor", "command": "close"}, elem.Text("Schließen"))),
	}
	if page.ZoneForm != nil && page.Error != "" {
		dialogContent = append(dialogContent, elem.Div(attrs.Props{attrs.Class: adminFlashErrorClass, attrs.Role: "alert", "tabindex": "-1", "data-zone-error": "true"}, elem.Text(page.Error)))
	}

	dialogContent = append(dialogContent, editor...)
	modal := elem.Dialog(dialogProps, dialogContent...)
	settings := elem.Section(attrs.Props{attrs.Class: "zone-panel", attrs.ID: zoneSettingsClass},
		elem.Div(attrs.Props{attrs.Class: "zone-policy-heading"}, elem.H2(nil, elem.Text("Zonen und Adressbereiche")),
			elem.A(attrs.Props{attrs.Href: info.Base + "/zones?new=1", attrs.Class: "btn btn-primary", "data-zone-opener": "new"}, elem.Text("Neue Zone"))), zoneOverview)

	content = append(content, modal)

	if !page.Enabled {
		content = append(content, settings)
		return info.shell("Zonen", content...)
	}

	if page.FilterTo == types.OwnExitsTarget {
		content = append(content, ownExitMatrixEditor(page, form))
	} else {
		rows = nil

		for _, link := range page.Model.Links {
			if page.FilterFrom != "" && (link.From != page.FilterFrom || link.To != page.FilterTo) {
				continue
			}

			rows = append(rows, elem.Tr(nil, elem.Td(nil, elem.Text(names[link.From])), elem.Td(nil, elem.Text(names[link.To])), elem.Td(nil, elem.Text(zoneLinkLabel(link))),
				elem.Td(nil, form("delete-link", zoneHidden("from", link.From), zoneHidden("to", link.To), submitButton("Freigabe entfernen", "btn-secondary")))))
		}

		services := "tcp:443"
		policyTitle := "Alle Zonenfreigaben"

		var selectedFrom, selectedTo []string
		if page.FilterFrom != "" {
			selectedFrom, selectedTo = []string{page.FilterFrom}, []string{page.FilterTo}

			policyTitle = names[page.FilterFrom] + " → " + names[page.FilterTo]
			for _, link := range page.Model.Links {
				if link.From == page.FilterFrom && link.To == page.FilterTo {
					services = strings.Join(link.Services, ", ")
				}
			}
		}

		policyContent := []elem.Node{
			elem.Div(attrs.Props{attrs.Class: "zone-policy-heading"}, elem.H2(nil, elem.Text(policyTitle)),
				elem.A(attrs.Props{attrs.Href: info.Base + "/zones#zone-policy-list", attrs.Class: "btn btn-secondary"}, elem.Text("Alle Freigaben"))),
			dataTable("Gerichtete Freigaben", []string{"Von", "Nach", "Dienste", "Aktion"}, rows),
		}
		mode := "services"
		if page.FilterFrom != "" {
			state, _ := zoneMatrixCell(page.Model, page.FilterFrom, page.FilterTo)
			switch state {
			case "allow":
				mode = "allow"
			case zoneReturnState:
				mode = zoneReturnState
			case zoneBlockedState:
				mode = "deny"
			}
		}

		if page.LinkAttempt {
			mode, services = page.LinkMode, page.LinkServices
		}

		choices := []AdminChoice{{Value: "allow", Label: "Alles erlaubt"}, {Value: "services", Label: "Dienstfreigabe"}, {Value: zoneReturnState, Label: "Nur Antwortverkehr"}, {Value: "deny", Label: zoneBlockedLabel}}
		policyContent = append(policyContent, form("link",
			elem.Div(attrs.Props{attrs.Class: "zone-form-grid"},
				zoneSelect("Quelle", "from", "link-from", page.Zones, selectedFrom, false),
				zoneSelect("Ziel", "to", "link-to", page.Zones, selectedTo, false)),
			zoneSelect("Verbindung", "link_mode", "link-mode", choices, []string{mode}, false),
			elem.Div(attrs.Props{"data-link-services": "true"}, zoneField("Erlaubte Dienste: tcp:443, udp:53, icmp:*", "services", "link-services", services, false)),
			elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Dienstfreigabe erlaubt nur die angegebenen Dienste. Nur Antwortverkehr erlaubt keine neuen Verbindungen und benötigt eine Freigabe in Gegenrichtung. Gesperrt sperrt neue Verbindungen; Antworten auf erlaubte Gegenverbindungen bleiben möglich. Die Gegenrichtung wird nie automatisch geändert.")),
			submitButton("Verbindung speichern", "btn-primary")))
		content = append(content, elem.Section(attrs.Props{attrs.Class: "zone-panel", attrs.ID: "zone-policy-list"}, policyContent...))
	}

	content = append(content, settings,
		elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Benutzerbezogene Exit-Regeln verwaltest du unter Benutzer, gerätebezogene Regeln unter Geräte.")))

	targetNames := map[string]string{}
	for _, node := range page.Nodes {
		targetNames["node:"+node.Value] = node.Label
	}

	public := []elem.Node{elem.P(nil, elem.Text("Öffentliche IPv6-Präfixe und Adressen werden hier reserviert. Sie werden nicht automatisch auf Clients eingerichtet. Das Präfix muss zum Gateway geroutet sein; Weiterleitung, Rückroute und Client-Adresse sind separat einzurichten. Ein Wechsel des Exits übernimmt dieses Präfix nicht."))}
	if zone.ID == "" {
		public = append(public, elem.P(nil, elem.Text("Zum Verwalten öffentlicher IPv6-Adressen oben eine Zone auswählen.")))
	} else {
		public = append(public, form("pool", zoneHidden("zone", zone.ID), zoneField("Geroutetes öffentliches IPv6-Präfix", "prefix", "public-prefix", "", false),
			zoneField("Gateway-Tag (muss in tagOwners definiert sein)", "gateway", "public-gateway", "", false), submitButton("IPv6-Präfix reservieren", "btn-primary")))
		for i, pool := range zone.IPv6Pools {
			prefixFields := []elem.Node{zoneHidden("zone", zone.ID), zoneHidden("prefix", pool.Prefix)}
			public = append(public, elem.H3(nil, elem.Text(pool.Prefix+" über "+pool.Gateway)),
				form("delete-pool", append(slices.Clone(prefixFields), submitButton("Präfix und Reservierungen entfernen", "btn-danger"))...))
			rows = nil

			for _, reservation := range pool.Reservations {
				id := strconv.FormatUint(reservation.Node.Uint64(), 10)

				label := targetNames["node:"+id]
				if label == "" {
					label = "Gerät #" + id
				}

				rows = append(rows, elem.Tr(nil, elem.Td(nil, elem.Text(label)), elem.Td(nil, elem.Text(reservation.Address)), elem.Td(nil, form("delete-reservation", append(slices.Clone(prefixFields), zoneHidden("node", id), submitButton("Reservierung entfernen", "btn-secondary"))...))))
			}

			public = append(public, dataTable("Reservierungen (Einrichtung erforderlich)", []string{"Gerät", "IPv6-Adresse", "Aktion"}, rows))
			reservationFields := append(slices.Clone(prefixFields), zoneSelect("Gerät dieser Zone", "node", "reservation-node-"+strconv.Itoa(i), page.Nodes, nil, false),
				zoneField("Feste IPv6-Adresse aus diesem Präfix", "address", "reservation-address-"+strconv.Itoa(i), "", false), submitButton("Adresse reservieren", "btn-primary"))
			public = append(public, form("reservation", reservationFields...))
		}
	}

	publicProps := attrs.Props{attrs.Class: zoneSettingsClass}
	if page.Selected != nil {
		publicProps["open"] = "true"
	}

	content = append(content, elem.Details(publicProps, append([]elem.Node{elem.Summary(nil, elem.Text("Öffentliche IPv6-Adressbereiche"))}, public...)...))

	return info.shell("Zonen", content...)
}

func zonePairHref(info AdminPageInfo, from, to string) string {
	return info.Base + "/zones?" + url.Values{"from": {from}, "to": {to}}.Encode() + "#zone-policy-list"
}

func zoneMatrixCell(model types.NetworkZones, from, to string) (string, string) {
	for _, link := range model.Links {
		if link.From == from && link.To == to {
			if link.Mode == zoneReturnState {
				return zoneReturnState, "Nur Antwortverkehr"
			}

			if link.Mode == "deny" {
				return zoneBlockedState, zoneBlockedLabel
			}

			if slices.Contains(link.Services, "*") {
				return "allow", "Alles erlaubt"
			}

			return "services", "Dienste (" + strconv.Itoa(len(link.Services)) + ")"
		}
	}

	for _, link := range model.Links {
		if link.From == to && link.To == from && len(link.Services) > 0 {
			return zoneReturnState, "Nur Antworten"
		}
	}

	return zoneBlockedState, zoneBlockedLabel
}

func zoneMatrix(info AdminPageInfo, page AdminZonesInfo) elem.Node {
	heading := elem.Div(attrs.Props{attrs.Class: "zone-policy-heading"}, elem.H2(nil, elem.Text("Zonenmatrix")),
		elem.Span(attrs.Props{attrs.Class: "muted"}, elem.Text("Zonenpaar auswählen, um die Freigabe zu bearbeiten")))
	if len(page.Model.Zones) == 0 {
		return zonePanel("Zonenmatrix", elem.P(nil, elem.Text("Erstelle zuerst Zonen. Ihre Verbindungen erscheinen anschließend hier als Matrix.")))
	}

	allProps := attrs.Props{attrs.Href: info.Base + "/zones#zone-policy-list", attrs.Class: "zone-matrix-all"}
	if page.FilterFrom == "" {
		allProps["aria-current"] = "true"
	}

	headers := []elem.Node{elem.Th(attrs.Props{"scope": "col"}, elem.A(allProps, elem.Text("Alle Freigaben ("+strconv.Itoa(len(page.Model.Links))+")")))}

	headers = append(headers, elem.Th(attrs.Props{"scope": "col"}, elem.Text("⇥ Eigene Exits")))
	for _, zone := range page.Model.Zones {
		headers = append(headers, elem.Th(attrs.Props{"scope": "col"}, elem.Text(zone.Name)))
	}

	var rows []elem.Node

	for _, source := range page.Model.Zones {
		cells := []elem.Node{elem.Th(attrs.Props{"scope": "row"}, elem.A(attrs.Props{attrs.Href: info.Base + "/zones?zone=" + url.QueryEscape(source.ID) + "#zone-settings"}, elem.Text(source.Name)))}
		state, label := ownExitMatrixState(source)

		ownProps := attrs.Props{attrs.Class: "zone-matrix-cell zone-matrix-" + state, attrs.Href: html.EscapeString(zonePairHref(info, source.ID, types.OwnExitsTarget)), zoneAriaLabel: html.EscapeString(source.Name + " → Eigene Exits: " + label)}
		if page.FilterFrom == source.ID && page.FilterTo == types.OwnExitsTarget {
			ownProps["aria-current"] = "true"
		}

		cells = append(cells, elem.Td(nil, elem.A(ownProps, elem.Text(label))))
		for _, destination := range page.Model.Zones {
			state, label := zoneMatrixCell(page.Model, source.ID, destination.ID)

			props := attrs.Props{attrs.Class: "zone-matrix-cell zone-matrix-" + state, attrs.Href: html.EscapeString(zonePairHref(info, source.ID, destination.ID)), zoneAriaLabel: html.EscapeString(source.Name + " → " + destination.Name + ": " + label)}
			if page.FilterFrom == source.ID && page.FilterTo == destination.ID {
				props["aria-current"] = "true"
			}

			cells = append(cells, elem.Td(nil, elem.A(props, elem.Text(label))))
		}

		rows = append(rows, elem.Tr(nil, cells...))
	}

	legend := elem.Div(attrs.Props{attrs.Class: "zone-matrix-legend"},
		elem.Span(attrs.Props{attrs.Class: "zone-key zone-matrix-allow"}, elem.Text("Alles erlaubt")),
		elem.Span(attrs.Props{attrs.Class: "zone-key zone-matrix-services"}, elem.Text("Dienstfreigabe")),
		elem.Span(attrs.Props{attrs.Class: "zone-key zone-matrix-return"}, elem.Text("Nur Antwortverkehr")),
		elem.Span(attrs.Props{attrs.Class: "zone-key zone-matrix-blocked"}, elem.Text("Neue Verbindungen gesperrt")))

	return elem.Section(attrs.Props{attrs.Class: "zone-panel zone-matrix-panel"}, heading,
		elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Quelle ↓ · Ziel → · Angezeigt werden die gespeicherten Freigaben des Entwurfs.")),
		elem.Div(attrs.Props{attrs.Class: "zone-matrix-scroll", attrs.Role: "region", zoneAriaLabel: "Zonenmatrix; horizontal scrollbar", "tabindex": "0"},
			elem.Table(attrs.Props{attrs.Class: "zone-matrix"}, elem.Caption(attrs.Props{attrs.Class: "sr-only"}, elem.Text("Gerichtete Zonenfreigaben: Quelle in den Zeilen, Ziel in den Spalten")), elem.THead(nil, elem.Tr(nil, headers...)), elem.TBody(nil, rows...))), legend)
}

func zoneNameField(value string, submitted bool) elem.Node {
	props := attrs.Props{attrs.ID: "zone-name", attrs.Name: "name", attrs.Type: "text", attrs.Value: html.EscapeString(value), attrs.Required: "true", "maxlength": "128", "aria-describedby": "zone-name-hint"}
	hint := "Pflichtfeld. Zum Beispiel Privat, Server oder Gäste."

	if submitted && strings.TrimSpace(value) == "" {
		props["aria-invalid"] = "true"
		hint = "Bitte gib einen Namen für die Zone ein."
	}

	if submitted && len(strings.TrimSpace(value)) > 128 {
		props["aria-invalid"] = "true"
		hint = "Der Zonenname ist zu lang. Bitte verwende einen kürzeren Namen."
	}

	return elem.Div(attrs.Props{attrs.Class: "field"}, elem.Label(attrs.Props{attrs.For: "zone-name"}, elem.Text("Name (Pflichtfeld)")), elem.Input(props), elem.P(attrs.Props{attrs.ID: "zone-name-hint", attrs.Class: "muted"}, elem.Text(hint)))
}

func zonePoolHint(family, prefix string) elem.Node {
	hint := "Die " + family + "-Adressvergabe ist auf diesem Server nicht aktiviert. Dieses Feld leer lassen."
	if prefix != "" {
		hint = "Für neue Mesh-Geräte; zulässiger Bereich: " + prefix + ". Leer lassen, um den globalen Adresspool zu verwenden."
	}

	return elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text(hint))
}

type AdminZoneAssignment struct {
	OwnRule                                types.OwnExitRule
	OwnDecision                            types.OwnExitDecision
	ProviderBlocked, ProviderDirectBlocked bool
	ExitChoices                            []AdminChoice
	ExitTags                               []string
	ExitLimited                            bool
	Choices                                []AdminChoice
	SourceRevision                         string
	LoadedRevision                         uint64
	Selected, Effective                    string
	Available                              bool
}

func adminZoneAssignment(info AdminPageInfo, path string, assignment *AdminZoneAssignment) elem.Node {
	if assignment == nil {
		return elem.Span(nil)
	}

	if !assignment.Available {
		return cardSection("Zone", "Zuerst unter Zonen eine Zone erstellen.", linkButton(info.Base+"/zones", "Zonen verwalten", "btn-secondary"))
	}

	button := "Zone hinzufügen"
	if assignment.Selected != "" {
		button = "Zone ändern"
	}

	content := []elem.Node{
		postForm(info, path+"/zone", "action-form",
			zoneHidden("source_revision", assignment.SourceRevision), zoneHidden("loaded_revision", strconv.FormatUint(assignment.LoadedRevision, 10)),
			zoneSelect("Zone auswählen", "zone", "assignment-zone", assignment.Choices, []string{assignment.Selected}, false), submitButton(button, "btn-primary")),
	}
	if assignment.Selected != "" {
		content = append(content, postForm(info, path+"/zone", "action-form",
			zoneHidden("source_revision", assignment.SourceRevision), zoneHidden("loaded_revision", strconv.FormatUint(assignment.LoadedRevision, 10)),
			zoneHidden("zone", ""), submitButton("Zonenzuordnung entfernen", "btn-danger")))
	}

	if strings.HasPrefix(path, "/nodes/") {
		content = append(content, elem.P(nil, elem.Text("Entfernen löscht nur die direkte Gerätezuordnung. Bei benutzereigenen Geräten gilt danach wieder die Zone des Benutzers; getaggte Geräte erben keine Benutzerzone.")))
	}

	content = append(content, ownExitEntityEditor(info, path, assignment), entityExitLimits(info, path, assignment))
	providerStatus := "In der aktuellen Zone ist das Bereitstellen eines eigenen Exit Nodes erlaubt (Routenfreigabe bleibt erforderlich)."
	providerButton, providerValue := "Eigene Exits in der Benutzerzone sperren", "true"

	if assignment.ProviderBlocked {
		providerStatus = "In der aktuellen Zone ist das Bereitstellen eines eigenen Exit Nodes gesperrt."
	}

	if assignment.ProviderDirectBlocked {
		providerButton, providerValue = "Exit-Sperre der Benutzerzone entfernen", "false"
	}

	content = append(content, elem.H3(nil, elem.Text("Eigene Exit Nodes")), elem.P(nil, elem.Text(providerStatus)),
		postForm(info, path+"/exit-provider", "action-form",
			zoneHidden("source_revision", assignment.SourceRevision), zoneHidden("loaded_revision", strconv.FormatUint(assignment.LoadedRevision, 10)),
			zoneHidden(zoneBlockedState, providerValue), submitButton(providerButton, "btn-secondary")),
		elem.P(nil, elem.Text("Diese Benutzer-/Gerätesperre gilt nur in der unsichtbaren Benutzerzone. In einer ausdrücklich zugeordneten Zone gilt deren Einstellung für eigene Exits. Eine Geräteeinstellung hebt die Benutzersperre innerhalb der Benutzerzone nicht auf.")))
	content = append(content, elem.P(nil, elem.Text("Wird als Entwurf gespeichert. Erst die Aktivierung unter Zonen wendet die Änderung an. Bestehende IP-Adressen bleiben erhalten.")), linkButton(info.Base+"/zones", "Zonen und Aktivierung", "btn-secondary"))

	return cardSection("Zone", "Wirksame Zone im Entwurf: "+assignment.Effective, content...)
}

func zoneLinkLabel(link types.ZoneLink) string {
	if link.Mode == zoneReturnState {
		return "Nur Antwortverkehr"
	}

	if link.Mode == "deny" {
		return zoneBlockedLabel
	}

	return strings.Join(link.Services, ", ")
}
