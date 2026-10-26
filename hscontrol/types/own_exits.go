package types

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

const OwnExitsTarget = "autogroup:own-exits"

type OwnExitRule struct {
	Mode     string   `json:"mode"`
	Services []string `json:"services,omitempty"`
	Locked   bool     `json:"locked,omitempty"`
}

type OwnExitOverride struct {
	Node  NodeID      `json:"node,omitempty"`
	User  UserID      `json:"user,omitempty"`
	Group string      `json:"group,omitempty"`
	Rule  OwnExitRule `json:"rule"`
}

type OwnExitDecision struct {
	Rule   OwnExitRule
	Source string
}

func (r OwnExitRule) Validate(zone bool) error {
	if r.Mode != "inherit" && r.Mode != "allow" && r.Mode != "deny" {
		return fmt.Errorf("%w: own exit mode must be inherit, allow or deny", ErrNetworkZones)
	}

	if r.Locked && (!zone || r.Mode != "deny") {
		return fmt.Errorf("%w: only a zone deny may be locked", ErrNetworkZones)
	}

	if r.Mode == "allow" && len(r.Services) == 0 {
		return fmt.Errorf("%w: allowed own exits require services", ErrNetworkZones)
	}

	if r.Mode != "allow" && len(r.Services) > 0 {
		return fmt.Errorf("%w: only allow rules may contain services", ErrNetworkZones)
	}

	for _, service := range r.Services {
		if !zoneServiceValid(service) {
			return fmt.Errorf("%w: invalid own exit service %q", ErrNetworkZones, service)
		}
	}

	return nil
}

func (n *NetworkZones) ValidateOwnExits() error {
	if n == nil {
		return nil
	}

	if len(n.OwnExitOverrides) > 4096 {
		return fmt.Errorf("%w: too many own exit overrides", ErrNetworkZones)
	}

	for _, zone := range n.Zones {
		if zone.ID == OwnExitsTarget {
			return fmt.Errorf("%w: reserved zone ID", ErrNetworkZones)
		}

		if zone.OwnExits != nil {
			err := zone.OwnExits.Validate(true)
			if err != nil {
				return err
			}
		}
	}

	seen := map[string]bool{}

	for _, override := range n.OwnExitOverrides {
		targets := 0
		if override.Node != 0 {
			targets++
		}

		if override.User != 0 {
			targets++
		}

		if override.Group != "" {
			targets++
		}

		if targets != 1 || (override.Group != "" && !strings.HasPrefix(override.Group, "group:")) {
			return fmt.Errorf("%w: own exit override requires exactly one valid target", ErrNetworkZones)
		}

		key := fmt.Sprintf("%d/%d/%s", override.Node, override.User, override.Group)
		if seen[key] {
			return fmt.Errorf("%w: duplicate own exit override", ErrNetworkZones)
		}

		seen[key] = true

		err := override.Rule.Validate(false)
		if err != nil {
			return err
		}
	}

	return nil
}

// OwnExitProviderFor confines personal providers to their owner, and named
// providers to the same effective zone. Tagged identity never inherits a user.
func (n *NetworkZones) OwnExitProviderFor(viewer, provider NodeView) bool {
	if n == nil || !viewer.Valid() || !provider.Valid() || viewer.ID() == provider.ID() {
		return false
	}

	a, b := n.ZoneForNode(viewer), n.ZoneForNode(provider)
	if a != nil || b != nil {
		return a != nil && b != nil && a.ID == b.ID
	}

	return !viewer.IsTagged() && !provider.IsTagged() && viewer.TypedUserID() != 0 && viewer.TypedUserID() == provider.TypedUserID()
}

func (n *NetworkZones) ResolveOwnExits(node NodeView, groups []string) OwnExitDecision {
	result := OwnExitDecision{Rule: OwnExitRule{Mode: "deny"}, Source: "Benutzerzone (Standard)"}
	if n == nil || !node.Valid() {
		return result
	}

	if zone := n.ZoneForNode(node); zone != nil {
		result.Source = "Zone: " + zone.Name
		if zone.OwnExits != nil && zone.OwnExits.Mode != "inherit" {
			result.Rule = *zone.OwnExits
		}
	}

	if result.Rule.Locked {
		return result
	}

	for _, level := range []string{"device", "user"} {
		for _, override := range n.OwnExitOverrides {
			match := level == "device" && node.ID() != 0 && override.Node == node.ID()

			match = match || (level == "user" && !node.IsTagged() && override.User != 0 && override.User == node.TypedUserID())
			if match && override.Rule.Mode != "inherit" {
				source := "Gerät"
				if level == "user" {
					source = "Benutzer"
				}

				return OwnExitDecision{Rule: override.Rule, Source: source}
			}
		}
	}

	var (
		groupRules []OwnExitRule
		names      []string
	)

	if !node.IsTagged() {
		for _, override := range n.OwnExitOverrides {
			if override.Group != "" && slices.Contains(groups, override.Group) && override.Rule.Mode != "inherit" {
				groupRules = append(groupRules, override.Rule)
				names = append(names, override.Group)
			}
		}
	}

	if len(groupRules) == 0 {
		return result
	}

	slices.Sort(names)

	result = OwnExitDecision{Rule: groupRules[0], Source: "Gruppe: " + strings.Join(names, ", ")}
	for _, rule := range groupRules {
		if rule.Mode == "deny" {
			result.Rule = OwnExitRule{Mode: "deny"}
			return result
		}

		result.Rule.Services = IntersectOwnExitServices(result.Rule.Services, rule.Services)
	}

	if len(result.Rule.Services) == 0 {
		result.Rule = OwnExitRule{Mode: "deny"}
	}

	return result
}

type ownExitPorts struct{ low, high uint64 }

func ownExitServiceRanges(services []string) map[string][]ownExitPorts {
	result := map[string][]ownExitPorts{}

	for _, service := range services {
		protocol, ports, ok := strings.Cut(service, ":")
		if !ok {
			continue
		}

		span := ownExitPorts{high: 65535}

		if ports != "*" {
			first, last, ranged := strings.Cut(ports, "-")
			span.low, _ = strconv.ParseUint(first, 10, 16)

			span.high = span.low
			if ranged {
				span.high, _ = strconv.ParseUint(last, 10, 16)
			}
		}

		result[protocol] = append(result[protocol], span)
	}

	return result
}

// IntersectOwnExitServices intersects protocol/port intervals, including ranges.
// It never treats partially overlapping ranges as independent permissions.
func IntersectOwnExitServices(left, right []string) []string {
	if slices.Contains(left, "*") {
		return slices.Clone(right)
	}

	if slices.Contains(right, "*") {
		return slices.Clone(left)
	}

	a, b := ownExitServiceRanges(left), ownExitServiceRanges(right)

	var result []string

	for _, protocol := range slices.Sorted(maps.Keys(a)) {
		var spans []ownExitPorts

		for _, x := range a[protocol] {
			for _, y := range b[protocol] {
				low, high := max(x.low, y.low), min(x.high, y.high)
				if low <= high {
					spans = append(spans, ownExitPorts{low: low, high: high})
				}
			}
		}

		slices.SortFunc(spans, func(x, y ownExitPorts) int {
			if x.low < y.low {
				return -1
			}

			if x.low > y.low {
				return 1
			}

			return 0
		})

		var merged []ownExitPorts
		for _, span := range spans {
			if len(merged) > 0 && span.low <= merged[len(merged)-1].high+1 {
				merged[len(merged)-1].high = max(merged[len(merged)-1].high, span.high)
			} else {
				merged = append(merged, span)
			}
		}

		for _, span := range merged {
			ports := strconv.FormatUint(span.low, 10)
			if span.low == 0 && span.high == 65535 {
				ports = "*"
			} else if span.high != span.low {
				ports += "-" + strconv.FormatUint(span.high, 10)
			}

			result = append(result, protocol+":"+ports)
		}
	}

	return result
}

// HasOwnExitAllow keeps route-driven recompilation enabled even before the
// first provider announces a route. Policies without own-exit allows keep the
// existing global filter fast path when no other per-node grant needs it.
func (n *NetworkZones) HasOwnExitAllow() bool {
	if n == nil {
		return false
	}

	for _, zone := range n.Zones {
		if zone.OwnExits != nil && zone.OwnExits.Mode == "allow" {
			return true
		}
	}

	return slices.ContainsFunc(n.OwnExitOverrides, func(rule OwnExitOverride) bool { return rule.Rule.Mode == "allow" })
}
