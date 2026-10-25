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
	zoneSettingsClass = "zone-settings"
	zoneAriaLabel     = "aria-label"
)

type (
	AdminZoneDevice struct{ ID, Name, Zone, Exits string }
	AdminZonesInfo  struct {
		Model                              types.NetworkZones
		Selected                           *types.NetworkZone
		Zones, Users, Nodes, Gateways      []AdminChoice
		Devices                            []AdminZoneDevice
		SourceRevision                     string
		LoadedRevision                     uint64
		Enabled, Active, Pending, Writable bool
		Error                              string
		FilterFrom, FilterTo               string
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
	if multiple {
		props["multiple"] = "true"
		props["size"] = "5"
	}

	var options []elem.Node
	if !multiple {
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
	content := []elem.Node{pageHead("Zonen", "Netzbereiche, Verbindungen und Exit-Berechtigungen gemeinsam verwalten.")}
	if page.Error != "" {
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
			elem.Td(nil, elem.A(attrs.Props{attrs.Href: info.Base + "/zones?zone=" + url.QueryEscape(zone.ID)}, elem.Text(zone.Name))),
			elem.Td(nil, elem.Text(strconv.Itoa(len(zone.Nodes))+" Geräte, "+strconv.Itoa(len(zone.Users))+" Benutzer")),
			elem.Td(nil, textOrDash(ranges)), elem.Td(nil, textOrDash(strings.Join(zone.Exits, ", ")))))
	}

	zoneOverview := dataTable("Zonen", []string{"Zone", "Zuordnung", "Adressbereiche", "Erlaubte Exit-Tags"}, rows)

	zone := types.NetworkZone{}
	if page.Selected != nil {
		zone = *page.Selected
	}

	var nodes, users []string
	for _, id := range zone.Nodes {
		nodes = append(nodes, strconv.FormatUint(id.Uint64(), 10))
	}

	for _, id := range zone.Users {
		users = append(users, strconv.FormatUint(uint64(id), 10))
	}

	fields := []elem.Node{
		zoneHidden("zone", zone.ID), zoneField("Name", "name", "zone-name", zone.Name, false),
		zoneSelect("Geräte (explizite Zuordnung hat Vorrang)", "nodes", "zone-nodes", page.Nodes, nodes, true),
		zoneSelect("Benutzer (einschließlich künftig registrierter Geräte)", "users", "zone-users", page.Users, users, true),
		elem.P(nil, elem.Text("Mehrfachauswahl mit Strg/Cmd. Getaggte Geräte erben keine Benutzerzuordnung.")),
		zoneField("Geroutete IPv4-/IPv6-Subnetze, eines pro Zeile", "subnets", "zone-subnets", strings.Join(zone.Subnets, "\n"), true),
		zoneField("Mesh IPv4-Pool, z. B. 100.64.10.0/24", "pool4", "zone-pool4", zone.Pool4, false),
		zoneField("Mesh IPv6-Pool innerhalb des konfigurierten Präfixes", "pool6", "zone-pool6", zone.Pool6, false),
		elem.P(nil, elem.Text("Mesh-Pools gelten für neue Registrierungen. Bestehende Geräte behalten ihre Adressen. Subnetze müssen separat vom Router angekündigt und freigegeben werden.")),
		zoneSelect("Erlaubte Exit-Tags (leer = keine Exits)", "exits", "zone-exits", page.Gateways, zone.Exits, true),
	}
	if !page.Enabled {
		fields = append(fields, checkboxField("Zonenmodus verwenden und vorhandene ACLs und Grants einschließlich App-Freigaben ersetzen. SSH-Regeln und andere Policy-Einstellungen bleiben erhalten.", "replace_network"))
	}

	fields = append(fields, submitButton("Zonenentwurf speichern", "btn-primary"))

	title := "Zone erstellen"
	if zone.ID != "" {
		title = "Zone bearbeiten: " + zone.Name
	}

	editor := []elem.Node{form("zone", fields...)}
	if zone.ID != "" {
		editor = append(editor, linkButton(info.Base+"/zones", "Neue Zone", "btn-secondary"),
			form("delete-zone", zoneHidden("zone", zone.ID), elem.P(nil, elem.Text("Löschen entfernt auch die Verbindungen und IPv6-Reservierungen dieser Zone aus dem Entwurf.")), submitButton("Zone und Verbindungen löschen", "btn-danger")))
	}

	settingsProps := attrs.Props{attrs.Class: zoneSettingsClass, attrs.ID: zoneSettingsClass}
	if page.Selected != nil || !page.Enabled {
		settingsProps["open"] = "true"
	}

	settings := elem.Details(settingsProps,
		elem.Summary(nil, elem.Text("Zonen und Adressbereiche")),
		zoneOverview, zonePanel(title, editor...))
	if !page.Enabled {
		content = append(content, settings)
		return info.shell("Zonen", content...)
	}

	rows = nil
	for _, link := range page.Model.Links {
		if page.FilterFrom != "" && (link.From != page.FilterFrom || link.To != page.FilterTo) {
			continue
		}
		rows = append(rows, elem.Tr(nil, elem.Td(nil, elem.Text(names[link.From])), elem.Td(nil, elem.Text(names[link.To])), elem.Td(nil, elem.Text(strings.Join(link.Services, ", "))),
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
	if page.FilterFrom != "" {
		policyContent = append(policyContent, elem.Div(attrs.Props{attrs.Class: "zone-policy-actions"},
			form("link", zoneHidden("from", page.FilterFrom), zoneHidden("to", page.FilterTo), zoneHidden("services", "*"), submitButton("Alles erlauben", "btn-primary")),
			form("delete-link", zoneHidden("from", page.FilterFrom), zoneHidden("to", page.FilterTo), submitButton("Freigabe entfernen", "btn-danger"))))
	}

	if len(rows) == 0 {
		policyContent = append(policyContent, elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Keine Freigabe für diese Auswahl. Neue Verbindungen sind gesperrt; Antworten auf erlaubte Verbindungen bleiben möglich.")))
	}

	policyContent = append(policyContent, form("link",
		elem.Div(attrs.Props{attrs.Class: "zone-form-grid"},
			zoneSelect("Quelle", "from", "link-from", page.Zones, selectedFrom, false),
			zoneSelect("Ziel", "to", "link-to", page.Zones, selectedTo, false)),
		zoneField("Erlaubte Dienste: tcp:443, udp:53, icmp:* oder *", "services", "link-services", services, false),
		elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Eine Freigabe gilt für neue Verbindungen von der Quelle zum Ziel. Speichern ändert den Entwurf; erst Aktivieren wendet ihn an.")),
		submitButton("Dienstfreigabe speichern", "btn-primary")))
	content = append(content, elem.Section(attrs.Props{attrs.Class: "zone-panel", attrs.ID: "zone-policy-list"}, policyContent...), settings)

	var targets []AdminChoice

	targetNames := map[string]string{}

	for _, user := range page.Users {
		key := "user:" + user.Value
		label := "Benutzer: " + user.Label
		targets = append(targets, AdminChoice{Value: key, Label: label})
		targetNames[key] = label
	}

	for _, node := range page.Nodes {
		key := "node:" + node.Value
		label := "Gerät: " + node.Label
		targets = append(targets, AdminChoice{Value: key, Label: label})
		targetNames[key] = label
	}

	rows = nil

	for _, limit := range page.Model.ExitLimits {
		target := "user:" + strconv.FormatUint(uint64(limit.User), 10)
		if limit.Node != 0 {
			target = "node:" + strconv.FormatUint(limit.Node.Uint64(), 10)
		}

		label := targetNames[target]
		if label == "" {
			label = target + " (nicht verfügbar)"
		}

		exits := strings.Join(limit.Exits, ", ")
		if exits == "" {
			exits = "Keine Exits"
		}

		rows = append(rows, elem.Tr(nil, elem.Td(nil, elem.Text(label)), elem.Td(nil, elem.Text(exits)), elem.Td(nil, form("delete-limit", zoneHidden("target", target), submitButton("Einschränkung entfernen", "btn-secondary")))))
	}

	content = append(content, elem.Details(attrs.Props{attrs.Class: zoneSettingsClass}, elem.Summary(nil, elem.Text("Exits je Benutzer und Gerät")),
		elem.P(nil, elem.Text("Wirksam ist die Schnittmenge aus Zone, Benutzerregel und Geräteregel. Eine leere Auswahl sperrt alle Exits. Ohne Einschränkung wird die Zonenfreigabe geerbt. Ein Tag erlaubt alle Exits mit diesem Tag; für einzelne Exits eindeutige Tags verwenden.")),
		dataTable("Exit-Einschränkungen", []string{"Gültig für", "Erlaubte Exit-Tags", "Aktion"}, rows),
		form("limit", zoneSelect("Benutzer oder Gerät", "target", "limit-target", targets, nil, false), zoneSelect("Erlaubte Exit-Tags", "exits", "limit-exits", page.Gateways, nil, true), submitButton("Exit-Einschränkung speichern", "btn-primary"))))

	rows = nil
	for _, device := range page.Devices {
		rows = append(rows, elem.Tr(nil, elem.Td(nil, elem.Text(device.Name)), elem.Td(nil, elem.Text(device.Zone)), elem.Td(nil, elem.Text(device.Exits))))
	}

	content = append(content, elem.Details(attrs.Props{attrs.Class: zoneSettingsClass}, elem.Summary(nil, elem.Text("Wirksame Gerätezuordnung")), dataTable("Wirksame Zuordnung im Entwurf", []string{"Gerät", "Zone", "Verfügbare Exit-Tags"}, rows)))

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
			if slices.Contains(link.Services, "*") {
				return "allow", "Alles erlaubt"
			}

			return "services", "Dienste (" + strconv.Itoa(len(link.Services)) + ")"
		}
	}

	for _, link := range model.Links {
		if link.From == to && link.To == from {
			return "return", "Nur Antworten"
		}
	}

	return "blocked", "Gesperrt"
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
	for _, zone := range page.Model.Zones {
		headers = append(headers, elem.Th(attrs.Props{"scope": "col"}, elem.Text(zone.Name)))
	}

	var rows []elem.Node

	for _, source := range page.Model.Zones {
		cells := []elem.Node{elem.Th(attrs.Props{"scope": "row"}, elem.A(attrs.Props{attrs.Href: info.Base + "/zones?zone=" + url.QueryEscape(source.ID) + "#zone-settings"}, elem.Text(source.Name)))}
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
