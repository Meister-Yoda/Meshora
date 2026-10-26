package templates

import (
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

	choices := []AdminChoice{{Value: ownExitInheritMode, Label: "Erben"}, {Value: "deny", Label: zoneBlockedLabel}, {Value: "allow", Label: "Erlaubte Dienste"}}

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

	return zoneBlockedLabel
}

func ownExitMatrixState(zone types.NetworkZone) (string, string) {
	if zone.OwnExits == nil || zone.OwnExits.Mode == ownExitInheritMode {
		return zoneBlockedState, "Standard: gesperrt"
	}

	if zone.OwnExits.Locked {
		return zoneBlockedState, "Verbindlich gesperrt"
	}

	if zone.OwnExits.Mode == "allow" {
		if slices.Contains(zone.OwnExits.Services, "*") {
			return "allow", "Alles erlaubt"
		}

		return "services", "Dienste (" + strconv.Itoa(len(zone.OwnExits.Services)) + ")"
	}

	return zoneBlockedState, zoneBlockedLabel
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

func ownExitEntityEditor(info AdminPageInfo, path string, assignment *AdminZoneAssignment) elem.Node {
	fields := make([]elem.Node, 0, 5)
	fields = append(fields, zoneHidden("source_revision", assignment.SourceRevision), zoneHidden("loaded_revision", strconv.FormatUint(assignment.LoadedRevision, 10)))
	fields = append(fields, ownExitRuleFields("entity-own-exits", assignment.OwnRule, false)...)
	fields = append(fields, submitButton("Regel für eigene Exits speichern", "btn-primary"))

	return elem.Section(nil, elem.H3(nil, elem.Text("Eigene Exits benutzen")),
		elem.P(nil, elem.Text("Wirksam: "+ownExitRuleLabel(assignment.OwnDecision.Rule)+" · Herkunft: "+assignment.OwnDecision.Source)),
		postForm(info, path+"/own-exits", "action-form", fields...))
}

func entityExitLimits(info AdminPageInfo, path string, assignment *AdminZoneAssignment) elem.Node {
	mode := "inherit"
	if assignment.ExitLimited {
		mode = "limit"
	}

	return elem.Section(nil, elem.H3(nil, elem.Text("Verfügbare Exits einschränken")),
		elem.P(nil, elem.Text("Erben übernimmt die Zonenfreigabe. Eine eigene Einschränkung erlaubt nur die ausgewählten Exit-Tags innerhalb der Zonenfreigabe; keine Auswahl sperrt alle Exits.")),
		postForm(info, path+"/exit-limit", "action-form",
			zoneHidden("source_revision", assignment.SourceRevision), zoneHidden("loaded_revision", strconv.FormatUint(assignment.LoadedRevision, 10)),
			zoneSelect("Einstellung", "exit_mode", "entity-exit-mode", []AdminChoice{{Value: "inherit", Label: "Erben"}, {Value: "limit", Label: "Eigene Einschränkung"}}, []string{mode}, false),
			zoneSelect("Erlaubte Exits", "exits", "entity-exit-tags", assignment.ExitChoices, assignment.ExitTags, true),
			submitButton("Exit-Einschränkung speichern", "btn-primary")))
}
