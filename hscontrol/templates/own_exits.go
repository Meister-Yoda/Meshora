package templates

import (
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/chasefleming/elem-go"
	"github.com/chasefleming/elem-go/attrs"
	"github.com/juanfont/headscale/hscontrol/types"
)

const ownExitInheritMode = "inherit"

func ownExitRuleFields(prefix string, rule types.OwnExitRule, zone bool) []elem.Node {
	mode := rule.Mode
	if mode == "" {
		mode = ownExitInheritMode
	}

	choices := []AdminChoice{{Value: ownExitInheritMode, Label: "Erben"}, {Value: "deny", Label: "Gesperrt"}, {Value: "allow", Label: "Erlaubte Dienste"}}

	services := strings.Join(rule.Services, ", ")
	if services == "" {
		services = "*"
	}

	fields := []elem.Node{
		zoneSelect("Regel", "own_mode", prefix+"-mode", choices, []string{mode}, false),
		zoneField("Dienste (* = alles), z. B. tcp:443, udp:53", "own_services", prefix+"-services", services, false),
	}

	if zone {
		props := attrs.Props{attrs.Type: "checkbox", attrs.Name: "own_locked"}
		if rule.Locked {
			props[attrs.Checked] = "true"
		}

		fields = append(fields, elem.Label(attrs.Props{attrs.Class: "check"}, elem.Input(props), elem.Span(nil, elem.Text("Nicht überschreibbare Sperre (nur bei Gesperrt)"))))
	}

	return fields
}

func ownExitRuleLabel(rule types.OwnExitRule) string {
	if rule.Locked {
		return "Verbindlich gesperrt"
	}

	if rule.Mode == "allow" {
		return "Erlaubt: " + strings.Join(rule.Services, ", ")
	}

	if rule.Mode == ownExitInheritMode || rule.Mode == "" {
		return "Erben (Standard: gesperrt)"
	}

	return "Gesperrt"
}

func ownExitMatrixState(zone types.NetworkZone) (string, string) {
	if zone.OwnExits == nil || zone.OwnExits.Mode == ownExitInheritMode {
		return "blocked", "Standard: gesperrt"
	}

	if zone.OwnExits.Locked {
		return "blocked", "Verbindlich gesperrt"
	}

	if zone.OwnExits.Mode == "allow" {
		if slices.Contains(zone.OwnExits.Services, "*") {
			return "allow", "Alles erlaubt"
		}

		return "services", "Dienste (" + strconv.Itoa(len(zone.OwnExits.Services)) + ")"
	}

	return "blocked", "Gesperrt"
}

func ownExitMatrixEditor(page AdminZonesInfo, form func(string, ...elem.Node) elem.Node) elem.Node {
	rule := types.OwnExitRule{Mode: ownExitInheritMode}
	name := ""

	for _, zone := range page.Model.Zones {
		if zone.ID == page.FilterFrom {
			name = zone.Name
			if zone.OwnExits != nil {
				rule = *zone.OwnExits
			}
		}
	}

	if page.OwnAttempt != nil {
		rule = *page.OwnAttempt
	}

	fields := make([]elem.Node, 0, 7)
	fields = append(fields, zoneHidden("from", page.FilterFrom), zoneHidden("to", types.OwnExitsTarget))
	fields = append(fields, ownExitRuleFields("zone-own-exits", rule, true)...)
	fields = append(fields, elem.P(nil, elem.Text("Dieser Zonenstandard regelt die Nutzung eigener Exits derselben Zone. Gerät → Benutzer → Gruppe → Zone. Erben übernimmt die nächste Ebene; ohne Regel gilt gesperrt. Die verbindliche Sperre verhindert alle Ausnahmen.")), submitButton("Exit-Zonenregel speichern", "btn-primary"))

	return elem.Section(attrs.Props{attrs.Class: "zone-panel", attrs.ID: "zone-policy-list"}, elem.H2(nil, elem.Text(name+" → Eigene Exits")), form("own-exits", fields...))
}

func ownExitGroupEditor(info AdminPageInfo, page AdminZonesInfo, form func(string, ...elem.Node) elem.Node) elem.Node {
	selectedRule := types.OwnExitRule{Mode: ownExitInheritMode}

	for _, override := range page.Model.OwnExitOverrides {
		if override.Group == page.SelectedOwnGroup && override.Group != "" {
			selectedRule = override.Rule
		}
	}

	if page.OwnAttempt != nil {
		selectedRule = *page.OwnAttempt
	}

	var rows []elem.Node

	for _, override := range page.Model.OwnExitOverrides {
		if override.Group != "" {
			rows = append(rows, elem.Tr(nil, elem.Td(nil, elem.Text(override.Group)), elem.Td(nil, elem.Text(ownExitRuleLabel(override.Rule))), elem.Td(nil, elem.A(attrs.Props{attrs.Href: info.Base + "/zones?own_group=" + url.QueryEscape(override.Group) + "#own-exit-groups"}, elem.Text("Bearbeiten")))))
		}
	}

	children := []elem.Node{elem.Summary(nil, elem.Text("Eigene Exits: Gruppenregeln")), elem.P(nil, elem.Text("Verwendet die Gruppen der Policy. Mehrere passende Gruppen werden geschnitten; eine Sperre hat Vorrang. Benutzer und Geräte können Gruppenregeln ersetzen, sofern keine verbindliche Zonensperre gilt.")), dataTable("Gruppenregeln für eigene Exits", []string{"Gruppe", "Regel", zoneActionHeading}, rows)}

	if len(page.PolicyGroups) > 0 {
		fields := make([]elem.Node, 0, 4)
		fields = append(fields, zoneSelect("Policy-Gruppe", "group", "own-exit-group", page.PolicyGroups, []string{page.SelectedOwnGroup}, false))
		fields = append(fields, ownExitRuleFields("group-own-exits", selectedRule, false)...)
		fields = append(fields, submitButton("Gruppenregel speichern", "btn-primary"))
		children = append(children, form("own-exit-group", fields...))
	} else {
		children = append(children, elem.P(nil, elem.Text("Noch keine Policy-Gruppen definiert.")), linkButton(info.Base+"/policy/edit", "Policy-Gruppen bearbeiten", "btn-secondary"))
	}

	props := attrs.Props{attrs.Class: zoneSettingsClass, attrs.ID: "own-exit-groups"}
	if page.SelectedOwnGroup != "" {
		props["open"] = "true"
	}

	return elem.Details(props, children...)
}

func ownExitEntityEditor(info AdminPageInfo, path string, assignment *AdminZoneAssignment) elem.Node {
	fields := make([]elem.Node, 0, 5)
	fields = append(fields, zoneHidden("source_revision", assignment.SourceRevision), zoneHidden("loaded_revision", strconv.FormatUint(assignment.LoadedRevision, 10)))
	fields = append(fields, ownExitRuleFields("entity-own-exits", assignment.OwnRule, false)...)
	fields = append(fields, submitButton("Regel für eigene Exits speichern", "btn-primary"))

	return elem.Section(nil, elem.H3(nil, elem.Text("Eigene Exits benutzen")),
		elem.P(nil, elem.Text("Wirksam: "+ownExitRuleLabel(assignment.OwnDecision.Rule)+" · Herkunft: "+assignment.OwnDecision.Source)),
		postForm(info, path+"/own-exits", "action-form", fields...))
}
