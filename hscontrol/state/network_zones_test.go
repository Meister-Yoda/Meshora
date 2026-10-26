package state

import (
	"encoding/json"
	"net/netip"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

func TestNetworkZoneRegistrationAndRestart(t *testing.T) {
	v4 := netip.MustParsePrefix("100.64.0.0/16")
	cfg := &types.Config{
		ServerURL:    "http://localhost:8080",
		PrefixV4:     &v4,
		IPAllocation: types.IPAllocationStrategySequential,
		Database:     types.DatabaseConfig{Type: types.DatabaseSqlite, Sqlite: types.SqliteConfig{Path: filepath.Join(t.TempDir(), "zones.db")}},
		Policy:       types.PolicyConfig{Mode: types.PolicyModeDB},
		Tuning:       types.Tuning{RegisterCacheExpiration: time.Minute, RegisterCacheMaxEntries: 100},
	}
	st, err := NewState(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	user, _, err := st.CreateUser(types.User{Name: "alice"})
	require.NoError(t, err)
	other, _, err := st.CreateUser(types.User{Name: "bob"})
	require.NoError(t, err)

	model := &types.NetworkZones{Zones: []types.NetworkZone{{ID: "a", Name: "A", Users: []types.UserID{types.UserID(user.ID)}, Pool4: "100.64.0.0/24"}}}
	source, loaded, err := st.PolicyEditorState()
	require.NoError(t, err)
	data, err := json.Marshal(map[string]any{"networkZones": model})
	require.NoError(t, err)
	source, err = st.SavePolicyDraft(string(data), source.Revision, loaded.Revision)
	require.NoError(t, err)
	_, err = st.ActivatePolicySource(source.Revision, loaded.Revision)
	require.NoError(t, err)

	register := func(user *types.User, name string) types.NodeView {
		t.Helper()

		id := types.MustAuthID()
		req := types.NewRegisterAuthRequest(&types.RegistrationData{MachineKey: key.NewMachine().Public(), NodeKey: key.NewNode().Public(), DiscoKey: key.NewDisco().Public(), Hostname: name, Hostinfo: &tailcfg.Hostinfo{Hostname: name, OS: "linux"}})
		st.SetAuthCacheEntry(id, req)
		node, _, err := st.HandleNodeFromAuthPath(id, types.UserID(user.ID), nil, "cli")
		require.NoError(t, err)

		return node
	}
	first := register(user, "first")
	require.Len(t, first.IPs(), 1)

	pool := netip.MustParsePrefix("100.64.0.0/24")
	require.True(t, pool.Contains(first.IPs()[0]))

	unrelated := register(other, "unrelated")
	require.False(t, pool.Contains(unrelated.IPs()[0]))
	require.NoError(t, st.Close())
	st, err = NewState(cfg)
	require.NoError(t, err)

	second := register(user, "second")
	require.True(t, pool.Contains(second.IPs()[0]))
	require.NotEqual(t, first.IPs()[0], second.IPs()[0])

	exitIP := netip.MustParseAddr("100.64.200.1")
	defaults := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
	gateway := st.PutNodeInStoreForTest(types.Node{ID: 99, Hostname: "exit", GivenName: "exit", IPv4: &exitIP, Tags: []string{"tag:exit"}, ApprovedRoutes: defaults, Hostinfo: &tailcfg.Hostinfo{RoutableIPs: defaults}})
	require.NoError(t, st.UpdatePolicyManagerNodesForTest())
	matchers, err := st.MatchersForNode(second)
	require.NoError(t, err)
	require.Empty(t, st.RoutesForPeer(second, gateway, matchers), "disallowed exits must not reach the network map")

	source, loaded, err = st.PolicyEditorState()
	require.NoError(t, err)

	model.Zones[0].Pool4 = "100.65.0.0/24"
	data, err = json.Marshal(map[string]any{"networkZones": model})
	require.NoError(t, err)
	_, err = st.SavePolicyDraft(string(data), source.Revision, loaded.Revision)
	require.ErrorContains(t, err, "outside the configured mesh prefix")
}

func TestExitProviderRestrictionRevokesApprovals(t *testing.T) {
	cfg := &types.Config{ServerURL: "http://localhost:8080", Database: types.DatabaseConfig{Type: types.DatabaseSqlite, Sqlite: types.SqliteConfig{Path: filepath.Join(t.TempDir(), "provider.db")}}, Policy: types.PolicyConfig{Mode: types.PolicyModeDB}, Tuning: types.Tuning{RegisterCacheExpiration: time.Minute, RegisterCacheMaxEntries: 100}}
	st, err := NewState(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	user := st.CreateUserForTest("provider")
	node := st.CreateRegisteredNodeForTest(user, "provider-node")
	defaults := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
	subnet := netip.MustParsePrefix("192.168.42.0/24")
	routes := slices.Concat(defaults, []netip.Prefix{subnet})
	node.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: routes}
	st.PutNodeInStoreForTest(*node)
	_, _, err = st.SetApprovedRoutes(node.ID, routes)
	require.NoError(t, err)
	source, loaded, err := st.PolicyEditorState()
	require.NoError(t, err)
	raw, err := json.Marshal(map[string]any{"networkZones": types.NetworkZones{ExitProviderUsers: []types.UserID{types.UserID(user.ID)}}})
	require.NoError(t, err)
	source, err = st.SavePolicyDraft(string(raw), source.Revision, loaded.Revision)
	require.NoError(t, err)
	_, err = st.ActivatePolicySource(source.Revision, loaded.Revision)
	require.NoError(t, err)

	fresh, ok := st.GetNodeByID(node.ID)
	require.True(t, ok)
	require.Equal(t, []netip.Prefix{subnet}, fresh.ApprovedRoutes().AsSlice(), "activation revokes exits while retaining subnet routes")

	_, _, err = st.SetApprovedRoutes(node.ID, defaults)
	require.ErrorIs(t, err, ErrExitProviderBlocked)
	_, _, err = st.SetApprovedRoutes(node.ID, []netip.Prefix{subnet})
	require.NoError(t, err)
}
