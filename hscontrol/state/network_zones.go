package state

import (
	"fmt"
	"net/netip"

	"github.com/juanfont/headscale/hscontrol/types"
)

func (s *State) allocateZoneIPs(node types.NodeView) (*netip.Addr, *netip.Addr, error) {
	zones, err := types.NetworkZonesFromPolicy(s.polMan.PolicySnapshot().Text)
	if err != nil {
		return nil, nil, fmt.Errorf("reading active zone pools: %w", err)
	}

	var zone *types.NetworkZone
	if zones != nil {
		zone = zones.ZoneForNode(node)
	}

	return s.ipAlloc.NextForZone(zone, zones)
}
