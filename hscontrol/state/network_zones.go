package state

import (
	"fmt"
	"net/netip"

	policyv2 "github.com/juanfont/headscale/hscontrol/policy/v2"
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

// InspectOwnExitDraft resolves policy-group membership with the same identity
// resolver as the running engine, without activating the draft.
func (s *State) InspectOwnExitDraft(text string, user types.UserID, node types.NodeID) (types.OwnExitRule, types.OwnExitDecision, error) {
	rule := types.OwnExitRule{Mode: "inherit"}

	users, err := s.ListAllUsers()
	if err != nil {
		return rule, types.OwnExitDecision{}, err
	}

	var view types.NodeView
	if node != 0 {
		view, _ = s.GetNodeByID(node)
	} else {
		owner, err := s.GetUserByID(user)
		if err != nil {
			return rule, types.OwnExitDecision{}, err
		}

		working := types.Node{User: owner, UserID: &owner.ID}
		view = working.View()
	}

	manager, err := policyv2.NewPolicyManager([]byte(text), users, s.ListNodes())
	if err != nil {
		return rule, types.OwnExitDecision{}, err
	}

	direct, decision := manager.InspectOwnExits(view, user, node)

	return direct, decision, nil
}
