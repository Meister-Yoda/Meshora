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
