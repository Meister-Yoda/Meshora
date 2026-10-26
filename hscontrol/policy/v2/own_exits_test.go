package v2

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"testing"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

func ownTestContains(raw string, ip netip.Addr) bool {
	if raw == "*" {
		return true
	}

	prefix, err := netip.ParsePrefix(raw)
	if err == nil {
		return prefix.Contains(ip)
	}

	address, err := netip.ParseAddr(raw)

	return err == nil && address == ip
}

func ownTestTCP(rules []tailcfg.FilterRule, src string, port uint16) bool {
	source := netip.MustParseAddr(src)
	destination := netip.MustParseAddr("8.8.8.8")

	for _, rule := range rules {
		if len(rule.IPProto) > 0 && !slices.Contains(rule.IPProto, 6) {
			continue
		}

		if !slices.ContainsFunc(rule.SrcIPs, func(raw string) bool { return ownTestContains(raw, source) }) {
			continue
		}

		for _, target := range rule.DstPorts {
			if ownTestContains(target.IP, destination) && target.Ports.First <= port && target.Ports.Last >= port {
				return true
			}
		}
	}

	return false
}

func TestOwnExitPacketFiltersAndLockedZone(t *testing.T) {
	user := types.User{ID: 1, Name: "alice"}
	other := types.User{ID: 2, Name: "bob"}
	client := node("client", "100.64.1.1", "fd7a:115c:a1e0::1", user)
	client.ID = 1
	provider := node("provider", "100.64.1.2", "fd7a:115c:a1e0::2", types.User{})
	provider.ID = 2
	provider.Tags = []string{"tag:exit-a"}
	outsider := node("outsider", "100.64.2.1", "fd7a:115c:a1e0::3", other)
	outsider.ID = 3
	defaults := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
	provider.ApprovedRoutes = defaults
	provider.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: defaults}
	model := types.NetworkZones{Zones: []types.NetworkZone{
		{ID: "z", Name: "Z", Users: []types.UserID{1}, Nodes: []types.NodeID{2}, Exits: []string{"tag:exit-a"}, OwnExits: &types.OwnExitRule{Mode: "allow", Services: []string{"tcp:443"}}},
		{ID: "other", Name: "Other", Users: []types.UserID{2}, Exits: []string{"tag:exit-a"}},
	}}
	nodes := types.Nodes{client, provider, outsider}
	raw := map[string]any{}
	err := json.Unmarshal(zoneTestPolicy(t, model), &raw)
	require.NoError(t, err)

	raw["tests"] = []map[string]any{{"src": "100.64.1.1", "accept": []string{"8.8.8.8:443"}, "deny": []string{"8.8.8.8:22"}}}
	policyText, err := json.Marshal(raw)
	require.NoError(t, err)
	pm, err := NewPolicyManager(policyText, []types.User{user, other}, nodes.ViewSlice())
	require.NoError(t, err)
	_, err = pm.SetPolicy(policyText)
	require.NoError(t, err)

	require.NoError(t, err)
	rules, err := pm.FilterForNode(provider.View())
	require.NoError(t, err)
	require.True(t, ownTestTCP(rules, "100.64.1.1", 443))
	require.False(t, ownTestTCP(rules, "100.64.1.1", 22), "legacy allow-all via must not bypass own-exit service limits")
	require.True(t, ownTestTCP(rules, "100.64.2.1", 22), "unrelated exit grants remain independent")
	require.ElementsMatch(t, defaults, pm.ViaRoutesForPeer(client.View(), provider.View()).Include)
	require.Contains(t, pm.BuildPeerMap(nodes.ViewSlice())[client.ID], provider.ID)

	model.Zones[0].OwnExits = &types.OwnExitRule{Mode: "deny", Locked: true}
	model.OwnExitOverrides = []types.OwnExitOverride{{Node: 1, Rule: types.OwnExitRule{Mode: "allow", Services: []string{"*"}}}}
	_, err = pm.SetPolicy(zoneTestPolicy(t, model))
	require.NoError(t, err)
	rules, err = pm.FilterForNode(provider.View())
	require.NoError(t, err)
	require.False(t, ownTestTCP(rules, "100.64.1.1", 443))
	require.True(t, ownTestTCP(rules, "100.64.2.1", 22))
	require.ElementsMatch(t, defaults, pm.ViaRoutesForPeer(client.View(), provider.View()).Exclude)
}

func TestOwnExitPolicyGroupsAndProviderIsolation(t *testing.T) {
	user := types.User{ID: 1, Name: "alice"}
	other := types.User{ID: 2, Name: "bob"}
	client := node("client", "100.64.1.1", "fd7a:115c:a1e0::1", user)
	client.ID = 1
	provider := node("provider", "100.64.1.2", "fd7a:115c:a1e0::2", user)
	provider.ID = 2
	foreign := node("foreign", "100.64.2.1", "fd7a:115c:a1e0::3", other)
	foreign.ID = 3

	defaults := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
	for _, gateway := range []*types.Node{provider, foreign} {
		gateway.ApprovedRoutes = defaults
		gateway.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: defaults}
	}

	model := types.NetworkZones{OwnExitOverrides: []types.OwnExitOverride{{Group: "group:a", Rule: types.OwnExitRule{Mode: "allow", Services: []string{"tcp:440-450"}}}, {Group: "group:b", Rule: types.OwnExitRule{Mode: "allow", Services: []string{"tcp:443-460"}}}}}
	raw, err := json.Marshal(map[string]any{"networkZones": model, "groups": map[string][]string{"group:a": {"alice@"}, "group:b": {"alice@"}}})
	require.NoError(t, err)
	pm, err := NewPolicyManager(raw, []types.User{user, other}, types.Nodes{client, provider, foreign}.ViewSlice())
	require.NoError(t, err)

	decision := pm.OwnExitDecision(client.View())
	require.Equal(t, []string{"tcp:443-450"}, decision.Rule.Services)

	rules, err := pm.FilterForNode(provider.View())
	require.NoError(t, err)
	require.True(t, ownTestTCP(rules, "100.64.1.1", 445))
	require.False(t, ownTestTCP(rules, "100.64.1.1", 441))
	rules, err = pm.FilterForNode(foreign.View())
	require.NoError(t, err)
	require.False(t, ownTestTCP(rules, "100.64.1.1", 445), "own-exit filter must never reach another provider")
	require.ElementsMatch(t, defaults, pm.ViaRoutesForPeer(client.View(), foreign.View()).Exclude)
}

func TestOwnExitProviderAppearsAfterRouteApproval(t *testing.T) {
	user := types.User{ID: 1, Name: "alice"}
	client := node("client", "100.64.1.1", "fd7a:115c:a1e0::1", user)
	client.ID = 1
	provider := node("provider", "100.64.1.2", "fd7a:115c:a1e0::2", user)
	provider.ID = 2
	model := types.NetworkZones{OwnExitOverrides: []types.OwnExitOverride{{User: 1, Rule: types.OwnExitRule{Mode: "allow", Services: []string{"tcp:443"}}}}}
	nodes := types.Nodes{client, provider}
	pm, err := NewPolicyManager(zoneTestPolicy(t, model), []types.User{user}, nodes.ViewSlice())
	require.NoError(t, err)
	rules, err := pm.FilterForNode(provider.View())
	require.NoError(t, err)
	require.False(t, ownTestTCP(rules, "100.64.1.1", 443))

	changedProvider := provider.Clone()
	defaults := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
	changedProvider.ApprovedRoutes = defaults
	changedProvider.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: defaults}
	nodes[1] = changedProvider
	changed, err := pm.SetNodes(nodes.ViewSlice())
	require.NoError(t, err)
	require.True(t, changed)

	rules, err = pm.FilterForNode(changedProvider.View())
	require.NoError(t, err)
	require.True(t, ownTestTCP(rules, "100.64.1.1", 443))
	require.ElementsMatch(t, defaults, pm.ViaRoutesForPeer(client.View(), changedProvider.View()).Include)
}

func BenchmarkOwnExitPeerMap(b *testing.B) {
	for _, allow := range []bool{false, true} {
		name := "standard"
		if allow {
			name = "own-exits"
		}

		b.Run(name, func(b *testing.B) {
			var (
				users []types.User
				nodes types.Nodes
			)

			model := types.NetworkZones{}

			for userID := 1; userID <= 8; userID++ {
				user := types.User{ID: uint(userID), Name: "user"}
				users = append(users, user)

				for device := 1; device <= 8; device++ {
					address := fmt.Sprintf("100.64.%d.%d", userID, device)
					ipv6 := fmt.Sprintf("fd7a:115c:a1e0::%x:%x", userID, device)
					machine := node("device", address, ipv6, user)

					machine.ID = types.NodeID((userID-1)*8 + device)
					if device == 8 {
						defaults := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
						machine.ApprovedRoutes = defaults
						machine.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: defaults}
					}

					nodes = append(nodes, machine)
				}

				if allow {
					model.OwnExitOverrides = append(model.OwnExitOverrides, types.OwnExitOverride{User: types.UserID(userID), Rule: types.OwnExitRule{Mode: "allow", Services: []string{"tcp:443"}}})
				}
			}

			raw, err := json.Marshal(map[string]any{"networkZones": model})
			if err != nil {
				b.Fatal(err)
			}

			pm, err := NewPolicyManager(raw, users, nodes.ViewSlice())
			if err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				pm.BuildPeerMap(nodes.ViewSlice())
			}
		})
	}
}
