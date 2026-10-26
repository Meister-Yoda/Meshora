package v2

import (
	"encoding/json"
	"net/netip"
	"testing"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

func zoneTestPolicy(t *testing.T, model types.NetworkZones) []byte {
	t.Helper()

	data, err := json.Marshal(map[string]any{"networkZones": model, "tagOwners": map[string][]string{"tag:exit-a": {}, "tag:exit-b": {}}})
	require.NoError(t, err)

	return data
}

func TestNetworkZonesDynamicMembershipAndExits(t *testing.T) {
	user := types.User{ID: 1, Name: "alice"}
	alice := node("alice", "100.64.1.1", "fd7a:115c:a1e0::6440:101", user)
	alice.ID = 1
	server := node("server", "100.64.2.1", "fd7a:115c:a1e0::6440:201", types.User{ID: 2, Name: "bob"})
	server.ID = 2
	exitA := node("exit-a", "100.64.3.1", "fd7a:115c:a1e0::6440:301", types.User{})
	exitA.ID = 3
	exitA.Tags = []string{"tag:exit-a"}
	exitB := node("exit-b", "100.64.3.2", "fd7a:115c:a1e0::6440:302", types.User{})
	exitB.ID = 4
	exitB.Tags = []string{"tag:exit-b"}

	defaults := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
	for _, exit := range []*types.Node{exitA, exitB} {
		exit.ApprovedRoutes = defaults
		exit.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: defaults}
	}

	model := types.NetworkZones{Zones: []types.NetworkZone{
		{ID: "private", Name: "Privat", Users: []types.UserID{1}, Exits: []string{"tag:exit-a", "tag:exit-b"}},
		{ID: "server", Name: "Server", Nodes: []types.NodeID{2}},
	}, Links: []types.ZoneLink{{From: "private", To: "server", Services: []string{"tcp:443"}}}, ExitLimits: []types.ZoneExitLimit{
		{User: 1, Exits: []string{"tag:exit-a"}}, {Node: 1, Exits: []string{"tag:exit-a"}},
	}}
	nodes := make(types.Nodes, 0, 5)
	nodes = append(nodes, alice, server, exitA, exitB)
	pm, err := NewPolicyManager(zoneTestPolicy(t, model), []types.User{user}, nodes.ViewSlice())
	require.NoError(t, err)
	require.NotEmpty(t, pm.ViaRoutesForPeer(alice.View(), exitA.View()).Include)
	require.ElementsMatch(t, defaults, pm.ViaRoutesForPeer(alice.View(), exitB.View()).Exclude)
	require.ElementsMatch(t, defaults, pm.ViaRoutesForPeer(server.View(), exitA.View()).Exclude)
	require.NoError(t, evaluateTests(pm.pol, []types.User{user}, nodes.ViewSlice()))
	fresh := node("fresh", "100.64.1.2", "fd7a:115c:a1e0::6440:102", user)
	fresh.ID = 5
	nodes = append(nodes, fresh)
	changed, err := pm.SetNodes(nodes.ViewSlice())
	require.NoError(t, err)
	require.True(t, changed)
	require.NotEmpty(t, pm.ViaRoutesForPeer(fresh.View(), exitA.View()).Include)
	require.Contains(t, pm.pol.NetworkZones.ExitsForNode(fresh.View()), "tag:exit-a")
	require.NotContains(t, pm.pol.NetworkZones.ExitsForNode(fresh.View()), "tag:exit-b")
	tagged := fresh.Clone()
	tagged.Tags = []string{"tag:exit-b"}
	nodes[len(nodes)-1] = tagged
	_, err = pm.SetNodes(nodes.ViewSlice())
	require.NoError(t, err)
	require.Nil(t, pm.pol.NetworkZones.ZoneForNode(tagged.View()))
	require.ElementsMatch(t, defaults, pm.ViaRoutesForPeer(tagged.View(), exitA.View()).Exclude)
}

func TestNetworkZonesDenyDefaultAndPolicyTests(t *testing.T) {
	pm, err := NewPolicyManager([]byte(`{"networkZones":{"zones":[]}}`), nil, types.Nodes{}.ViewSlice())
	require.NoError(t, err)

	filter, _ := pm.Filter()
	require.Empty(t, filter)

	a := node("a", "100.64.1.1", "fd7a:115c:a1e0::6440:101", types.User{})
	a.ID = 1
	b := node("b", "100.64.2.1", "fd7a:115c:a1e0::6440:201", types.User{})
	b.ID = 2
	data := []byte(`{
 "networkZones":{"zones":[{"id":"a","name":"A","nodes":[1]},{"id":"b","name":"B","nodes":[2]}],"links":[{"from":"a","to":"b","services":["tcp:443"]}]},
 "tests":[{"src":"100.64.1.1","accept":["100.64.2.1:443"],"deny":["100.64.2.1:22"]},{"src":"100.64.2.1","deny":["100.64.1.1:443"]}]
 }`)
	pm, err = NewPolicyManager(data, nil, types.Nodes{a, b}.ViewSlice())
	require.NoError(t, err)
	_, err = pm.SetPolicy(data)
	require.NoError(t, err)
	_, err = pm.SetPolicy([]byte(`{"networkZones":{"zones":[]},"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}]}`))
	require.ErrorContains(t, err, "cannot be combined")
	require.Equal(t, string(data), pm.PolicySnapshot().Text)
}

func TestHiddenUserZonesAndExitProviderBlocks(t *testing.T) {
	user := types.User{ID: 1, Name: "alice"}
	other := types.User{ID: 2, Name: "bob"}
	a := node("a", "100.64.1.1", "fd7a:115c:a1e0::1", user)
	a.ID = 1
	b := node("b", "100.64.1.2", "fd7a:115c:a1e0::2", user)
	b.ID = 2
	c := node("c", "100.64.2.1", "fd7a:115c:a1e0::3", other)
	c.ID = 3
	defaults := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
	a.ApprovedRoutes = defaults
	a.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: defaults}
	nodes := types.Nodes{a, b, c}
	data := []byte(`{"networkZones":{"zones":[],"exitProviderUsers":[1]},"tests":[{"src":"100.64.1.1","accept":["100.64.1.2:443"],"deny":["100.64.2.1:443"]},{"src":"100.64.2.1","deny":["100.64.1.1:443"]}]}`)
	pm, err := NewPolicyManager(data, []types.User{user, other}, nodes.ViewSlice())
	require.NoError(t, err)
	_, err = pm.SetPolicy(data)
	require.NoError(t, err)
	require.Empty(t, pm.pol.NetworkZones.Zones, "default zones must not be stored as visible zones")
	require.False(t, pm.NodeCanProvideExit(a.View()))
	require.False(t, pm.NodeCanApproveRoute(a.View(), defaults[0]))
	require.ElementsMatch(t, defaults, pm.ViaRoutesForPeer(b.View(), a.View()).Exclude)
	tagged := a.Clone()
	tagged.Tags = []string{"tag:server"}
	require.True(t, pm.NodeCanProvideExit(tagged.View()), "creator tracking is not user ownership")

	data = []byte(`{"networkZones":{"zones":[],"exitProviderNodes":[1]}}`)
	_, err = pm.SetPolicy(data)
	require.NoError(t, err)
	require.True(t, pm.NodeCanProvideExit(tagged.View()), "personal-zone device block does not govern tagged identity")

	data = []byte(`{"networkZones":{"zones":[{"id":"managed","name":"Managed","nodes":[1]}],"exitProviderUsers":[1],"exitProviderNodes":[1]}}`)
	_, err = pm.SetPolicy(data)
	require.NoError(t, err)
	require.True(t, pm.NodeCanProvideExit(a.View()), "personal restrictions do not carry into named zones")

	data = []byte(`{"networkZones":{"zones":[{"id":"managed","name":"Managed","nodes":[1],"blockOwnExits":true}]}}`)
	_, err = pm.SetPolicy(data)
	require.NoError(t, err)
	require.False(t, pm.NodeCanProvideExit(a.View()))
	require.False(t, pm.NodeCanProvideExit(tagged.View()), "named zone restriction also covers assigned tagged devices")
}

func TestZoneProviderBlockRemovesInternetFilters(t *testing.T) {
	user := types.User{ID: 1, Name: "alice"}
	client := node("client", "100.64.1.1", "fd7a:115c:a1e0::1", user)
	client.ID = 1
	gateway := node("gateway", "100.64.2.1", "fd7a:115c:a1e0::2", types.User{})
	gateway.ID = 2
	gateway.Tags = []string{"tag:exit-a"}
	defaults := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
	gateway.ApprovedRoutes = defaults
	gateway.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: defaults}
	model := types.NetworkZones{Zones: []types.NetworkZone{{ID: "clients", Name: "Clients", Users: []types.UserID{1}, Exits: []string{"tag:exit-a"}}, {ID: "providers", Name: "Providers", Nodes: []types.NodeID{2}}}}
	pm, err := NewPolicyManager(zoneTestPolicy(t, model), []types.User{user}, types.Nodes{client, gateway}.ViewSlice())
	require.NoError(t, err)
	before, err := pm.FilterForNode(gateway.View())
	require.NoError(t, err)
	require.NotEmpty(t, before)

	model.Zones[1].BlockOwnExits = true
	_, err = pm.SetPolicy(zoneTestPolicy(t, model))
	require.NoError(t, err)
	after, err := pm.FilterForNode(gateway.View())
	require.NoError(t, err)
	require.Empty(t, after)
	require.ElementsMatch(t, defaults, pm.ViaRoutesForPeer(client.View(), gateway.View()).Exclude)
}

func TestNetworkZoneBlockedModesDoNotGrantTraffic(t *testing.T) {
	a := node("a", "100.64.1.1", "fd7a:115c:a1e0::1", types.User{})
	a.ID = 1
	b := node("b", "100.64.2.1", "fd7a:115c:a1e0::2", types.User{})
	b.ID = 2

	for _, mode := range []string{"return", "deny"} {
		t.Run(mode, func(t *testing.T) {
			data, err := json.Marshal(map[string]any{
				"networkZones": types.NetworkZones{Zones: []types.NetworkZone{{ID: "a", Name: "A", Nodes: []types.NodeID{1}}, {ID: "b", Name: "B", Nodes: []types.NodeID{2}}}, Links: []types.ZoneLink{{From: "a", To: "b", Mode: mode}, {From: "b", To: "a", Services: []string{"tcp:443"}}}},
				"tests":        []map[string]any{{"src": "100.64.1.1", "deny": []string{"100.64.2.1:443", "100.64.2.1:22"}}, {"src": "100.64.2.1", "accept": []string{"100.64.1.1:443"}, "deny": []string{"100.64.1.1:22"}}},
			})
			require.NoError(t, err)
			pm, err := NewPolicyManager(data, nil, types.Nodes{a, b}.ViewSlice())
			require.NoError(t, err)
			_, err = pm.SetPolicy(data)
			require.NoError(t, err)
		})
	}
}
