package v2

import (
	"net/netip"
	"slices"

	"github.com/go-json-experiment/json"
	"github.com/juanfont/headscale/hscontrol/types"
	"tailscale.com/tailcfg"
	"tailscale.com/types/views"
)

func (pol *Policy) ownExitGroups(node types.NodeView, users types.Users) []string {
	var groups []string
	if node.IsTagged() {
		return groups
	}

	for group, members := range pol.Groups {
		for _, member := range members {
			user, err := member.resolveUser(users)
			if err == nil && types.UserID(user.ID) == node.TypedUserID() {
				groups = append(groups, string(group))
				break
			}
		}
	}

	slices.Sort(groups)

	return groups
}

func (pol *Policy) ownExitDecision(node types.NodeView, users types.Users) types.OwnExitDecision {
	if pol == nil {
		return (*types.NetworkZones)(nil).ResolveOwnExits(node, nil)
	}

	return pol.NetworkZones.ResolveOwnExits(node, pol.ownExitGroups(node, users))
}

func (pm *PolicyManager) OwnExitDecision(node types.NodeView) types.OwnExitDecision {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.pol.ownExitDecision(node, pm.users)
}

func (pol *Policy) compileOwnExitGrants(users types.Users, nodes views.Slice[types.NodeView]) []compiledGrant {
	if pol == nil || pol.NetworkZones == nil {
		return nil
	}

	var result []compiledGrant

	for _, viewer := range nodes.All() {
		decision := pol.ownExitDecision(viewer, users)
		if decision.Rule.Mode != "allow" {
			continue
		}

		var src []string
		for _, ip := range viewer.IPs() {
			src = append(src, ip.String())
		}

		if len(src) == 0 {
			continue
		}

		wire := zoneGrant{Src: src, Dst: []string{"autogroup:internet"}, IP: decision.Rule.Services}

		data, err := json.Marshal(wire)
		if err != nil {
			continue
		}

		var grant Grant

		err = json.Unmarshal(data, &grant, policyJSONOpts...)
		if err != nil {
			continue
		}

		for _, provider := range nodes.All() {
			if !provider.IsExitNode() || pol.NetworkZones.ExitProviderBlocked(provider) || !pol.NetworkZones.OwnExitProviderFor(viewer, provider) {
				continue
			}

			compiled, err := pol.compileOneGrant(grant, users, nodes)
			if err != nil || compiled == nil {
				continue
			}

			compiled.category = grantCategoryOwnExit
			compiled.exitProvider = provider.ID()
			result = append(result, *compiled)
		}
	}

	return result
}

// Remove matching sources from legacy Internet-via rules at their own provider.
// Otherwise a prior broad allow could bypass the new deny or port restriction.
func (pol *Policy) ownExitLegacyGrants(grants []compiledGrant, nodes views.Slice[types.NodeView], node types.NodeView) []compiledGrant {
	if pol == nil || pol.NetworkZones == nil || !node.IsExitNode() || !slices.ContainsFunc(grants, func(g compiledGrant) bool { return g.via != nil && g.via.hasAutoGroupInternet }) {
		return grants
	}

	ownIPs := map[string]bool{}

	for _, viewer := range nodes.All() {
		if pol.NetworkZones.OwnExitProviderFor(viewer, node) {
			for _, ip := range viewer.IPs() {
				ownIPs[ip.String()] = true
				ownIPs[netip.PrefixFrom(ip, ip.BitLen()).String()] = true
			}
		}
	}

	grants = slices.Clone(grants)
	for i := range grants {
		if grants[i].via == nil || !grants[i].via.hasAutoGroupInternet {
			continue
		}

		via := *grants[i].via
		via.srcIPStrings = slices.DeleteFunc(slices.Clone(via.srcIPStrings), func(ip string) bool { return ownIPs[ip] })
		grants[i].via = &via
		grants[i].srcIPStrings = slices.DeleteFunc(slices.Clone(grants[i].srcIPStrings), func(ip string) bool { return ownIPs[ip] })
	}

	return grants
}

// Policy tests evaluate reachability through any approved, authorized provider.
// Running filters remain per-provider; only this validation view is combined.
func (pol *Policy) ownExitTestFilters(base []tailcfg.FilterRule, users types.Users, nodes views.Slice[types.NodeView]) []tailcfg.FilterRule {
	if pol == nil || pol.NetworkZones == nil {
		return base
	}

	rules := slices.Clone(base)

	grants := pol.compileGrants(users, nodes)
	for _, provider := range nodes.All() {
		if !provider.IsExitNode() || pol.NetworkZones.ExitProviderBlocked(provider) {
			continue
		}

		filtered := pol.ownExitLegacyGrants(grants, nodes, provider)
		rules = append(rules, filterRulesForNode(filtered, provider, buildUserNodeIndex(nodes))...)
	}

	return mergeFilterRules(rules)
}

func (pm *PolicyManager) ownExitPeerFastPath() bool {
	return pm.pol != nil && pm.pol.NetworkZones.HasOwnExitAllow() && !slices.ContainsFunc(pm.compiledGrants, func(grant compiledGrant) bool {
		return grant.category != grantCategoryRegular && grant.category != grantCategoryOwnExit
	})
}

// Own-exit peer edges can augment the global bitset path without rebuilding
// the identical global matcher set once per ordinary client.
func (pm *PolicyManager) addOwnExitPeers(peers map[types.NodeID][]types.NodeID, nodes views.Slice[types.NodeView]) {
	var providers []types.NodeView

	for _, node := range nodes.All() {
		if node.IsExitNode() && !pm.pol.NetworkZones.ExitProviderBlocked(node) {
			providers = append(providers, node)
		}
	}

	for _, viewer := range nodes.All() {
		if pm.pol.ownExitDecision(viewer, pm.users).Rule.Mode != "allow" {
			continue
		}

		for _, provider := range providers {
			if !pm.pol.NetworkZones.OwnExitProviderFor(viewer, provider) {
				continue
			}

			if !slices.Contains(peers[viewer.ID()], provider.ID()) {
				peers[viewer.ID()] = append(peers[viewer.ID()], provider.ID())
			}

			if !slices.Contains(peers[provider.ID()], viewer.ID()) {
				peers[provider.ID()] = append(peers[provider.ID()], viewer.ID())
			}
		}
	}
}
