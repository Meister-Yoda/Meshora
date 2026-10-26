package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/juanfont/headscale/hscontrol/templates"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/tailscale/hujson"
)

func parseOwnExitForm(r *http.Request, zone bool) (types.OwnExitRule, error) {
	rule := types.OwnExitRule{Mode: r.PostForm.Get("own_mode"), Locked: zone && r.PostForm.Get("own_locked") == "on"}
	if rule.Mode == "allow" {
		rule.Services = strings.FieldsFunc(r.PostForm.Get("own_services"), func(c rune) bool { return c == ',' || c == ' ' || c == '\n' || c == '\r' })
	}

	err := rule.Validate(zone)

	return rule, err
}

func setOwnExitOverride(model *types.NetworkZones, override types.OwnExitOverride) {
	model.OwnExitOverrides = slices.DeleteFunc(model.OwnExitOverrides, func(old types.OwnExitOverride) bool {
		return old.Node == override.Node && old.User == override.User && old.Group == override.Group
	})
	if override.Rule.Mode != "inherit" {
		model.OwnExitOverrides = append(model.OwnExitOverrides, override)
	}
}

func mutateOwnExitZone(model *types.NetworkZones, r *http.Request) error {
	index := zoneIndex(model, r.PostForm.Get("from"))
	if index < 0 {
		return fmt.Errorf("%w: Zone existiert nicht mehr", errZoneForm)
	}

	rule, err := parseOwnExitForm(r, true)
	if err != nil {
		return err
	}

	model.Zones[index].OwnExits = &rule

	return nil
}

func ownExitPolicyGroups(text string) ([]templates.AdminChoice, error) {
	raw, err := hujson.Standardize([]byte(text))
	if err != nil {
		return nil, err
	}

	var document struct {
		Groups map[string]json.RawMessage `json:"groups"`
	}

	err = json.Unmarshal(raw, &document)
	if err != nil {
		return nil, err
	}

	var groups []templates.AdminChoice
	for name := range document.Groups {
		groups = append(groups, templates.AdminChoice{Value: name, Label: name})
	}

	slices.SortFunc(groups, func(a, b templates.AdminChoice) int { return strings.Compare(a.Value, b.Value) })

	return groups, nil
}
