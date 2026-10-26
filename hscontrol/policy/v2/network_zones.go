package v2

import (
	"maps"
	"slices"

	"github.com/go-json-experiment/json"
	"github.com/juanfont/headscale/hscontrol/types"
	"tailscale.com/types/views"
)

type zoneGrant struct {
	Src []string `json:"src"`
	Dst []string `json:"dst"`
	IP  []string `json:"ip"`
	Via []string `json:"via,omitempty"`
}

// materializeNetworkZones is rerun for each node snapshot. New user-owned
// devices inherit their owner's zone; tagged devices never inherit ownership.
// It works on a copy, leaving the previous policy intact on compile failure.
func (pol *Policy) materializeNetworkZones(nodes views.Slice[types.NodeView]) (*Policy, error) {
	if pol == nil || pol.NetworkZones == nil {
		return pol, nil
	}

	model := pol.NetworkZones

	members := map[string][]string{}
	for _, zone := range model.Zones {
		members[zone.ID] = slices.Clone(zone.Subnets)
	}

	for _, node := range nodes.All() {
		if zone := model.ZoneForNode(node); zone != nil {
			for _, ip := range node.IPs() {
				members[zone.ID] = append(members[zone.ID], ip.String())
			}
		}
	}

	// Hidden default zones contain only otherwise unassigned, user-owned
	// device IPs. Explicit zones and tagged devices never join these groups.
	private := map[types.UserID][]string{}

	for _, node := range nodes.All() {
		if !node.IsTagged() && node.TypedUserID() != 0 && model.ZoneForNode(node) == nil {
			for _, ip := range node.IPs() {
				private[node.TypedUserID()] = append(private[node.TypedUserID()], ip.String())
			}
		}
	}
	var wire []zoneGrant

	for _, id := range slices.Sorted(maps.Keys(private)) {
		ips := private[id]
		wire = append(wire, zoneGrant{Src: ips, Dst: ips, IP: []string{"*"}})
	}

	for _, link := range model.Links {
		if link.Mode != "" {
			continue
		}

		src, dst := members[link.From], members[link.To]
		if len(src) > 0 && len(dst) > 0 {
			wire = append(wire, zoneGrant{Src: src, Dst: dst, IP: link.Services})
		}
	}

	for _, node := range nodes.All() {
		exits := model.ExitsForNode(node)
		if len(exits) == 0 {
			continue
		}

		var src []string
		for _, ip := range node.IPs() {
			src = append(src, ip.String())
		}

		if len(src) > 0 {
			wire = append(wire, zoneGrant{Src: src, Dst: []string{"autogroup:internet"}, IP: []string{"*"}, Via: exits})
		}
	}

	for _, zone := range model.Zones {
		if len(zone.Exits) > 0 && len(zone.Subnets) > 0 {
			wire = append(wire, zoneGrant{Src: zone.Subnets, Dst: []string{"autogroup:internet"}, IP: []string{"*"}, Via: zone.Exits})
		}
	}

	data, err := json.Marshal(wire)
	if err != nil {
		return nil, err
	}

	grants := []Grant{}
	// Use the actual grant decoder, including protocol and tag validation.
	if len(wire) > 0 {
		err = json.Unmarshal(data, &grants, policyJSONOpts...)
		if err != nil {
			return nil, err
		}
	}

	expanded := *pol
	expanded.Grants = grants
	expanded.ACLs = []ACL{}

	return &expanded, nil
}
