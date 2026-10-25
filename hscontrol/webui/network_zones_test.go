package webui

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/require"
)

func zonesForm(t *testing.T, env *testEnv, csrf, op string) url.Values {
	t.Helper()

	source, loaded, err := env.state.PolicyEditorState()
	require.NoError(t, err)

	return url.Values{"csrf_token": {csrf}, "op": {op}, "source_revision": {source.Revision}, "loaded_revision": {strconv.FormatUint(loaded.Revision, 10)}}
}

func zoneModel(t *testing.T, env *testEnv) *types.NetworkZones {
	t.Helper()

	source, _, err := env.state.PolicyEditorState()
	require.NoError(t, err)
	model, err := types.NetworkZonesFromPolicy(source.Text)
	require.NoError(t, err)
	require.NotNil(t, model)

	return model
}

func TestZonesDraftActivationAndConflict(t *testing.T) {
	env := newTestEnv(t)
	csrf := env.signedIn()
	source, loaded, err := env.state.PolicyEditorState()
	require.NoError(t, err)
	stale := zonesForm(t, env, csrf, "zone")
	form := zonesForm(t, env, csrf, "zone")
	form.Set("name", "Privat <script>alert(1)</script>")
	response, _ := env.post(env.client, "/admin/zones", form)
	require.Equal(t, http.StatusBadRequest, response.StatusCode, "initial takeover requires explicit acknowledgement")
	form.Set("replace_network", "on")
	response, _ = env.post(env.client, "/admin/zones", form)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.Equal(t, loaded, env.state.PolicySnapshot(), "saving must not activate")
	model := zoneModel(t, env)
	require.Len(t, model.Zones, 1)
	_, body := env.get(env.client, "/admin/zones?zone="+model.Zones[0].ID)
	require.Contains(t, body, "&lt;script&gt;")
	require.NotContains(t, body, "<script>alert")
	require.Contains(t, body, "Zonen-Policy aktivieren")
	stale.Set("name", "stale")
	response, _ = env.post(env.client, "/admin/zones", stale)
	require.Equal(t, http.StatusConflict, response.StatusCode)
	require.Len(t, zoneModel(t, env).Zones, 1)
	invalid := zonesForm(t, env, csrf, "zone")
	invalid.Set("zone", model.Zones[0].ID)
	invalid.Set("name", "Privat")
	invalid.Set("subnets", "0.0.0.0/0")

	before, _, err := env.state.PolicyEditorState()
	require.NoError(t, err)

	response, _ = env.post(env.client, "/admin/zones", invalid)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)

	after, _, err := env.state.PolicyEditorState()
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.NotEqual(t, source.Revision, after.Revision)
	activate := zonesForm(t, env, csrf, "")
	response, _ = env.post(env.client, "/admin/zones/activate", activate)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)

	active, err := types.NetworkZonesFromPolicy(env.state.PolicySnapshot().Text)
	require.NoError(t, err)
	require.Len(t, active.Zones, 1)
}

func TestZonesRequireSessionAndCSRF(t *testing.T) {
	env := newTestEnv(t)
	response, _ := env.get(env.client, "/admin/zones")
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	env.signedIn()

	for _, path := range []string{"/admin/zones", "/admin/zones/activate"} {
		for _, token := range []string{"", "forged"} {
			response, _ = env.post(env.client, path, url.Values{"csrf_token": {token}, "op": {"zone"}})
			require.Equal(t, http.StatusForbidden, response.StatusCode)
		}
	}
}

func TestZonePolicyPreservesUnrelatedHuJSON(t *testing.T) {
	source := `{
 // Keep the identity setup.
 "tagOwners":{"tag:exit":[]},
 "ssh":[],
 "acls":[{"action":"accept","src":["*"],"dst":["*:*"]}],
 "#note":"retain this metadata",
 }`
	text, err := zonePolicyText(source, &types.NetworkZones{Zones: []types.NetworkZone{{ID: "a", Name: "A"}}})
	require.NoError(t, err)
	require.Contains(t, text, "Keep the identity setup.")
	require.Contains(t, text, `"tagOwners"`)
	require.Contains(t, text, `"ssh"`)
	require.Contains(t, text, `"#note"`)
	require.NotContains(t, text, `"acls"`)
	model, err := types.NetworkZonesFromPolicy(text)
	require.NoError(t, err)
	require.Len(t, model.Zones, 1)
}

func TestZonesExitLimitsAndPublicIPv6(t *testing.T) {
	env := newTestEnv(t)
	csrf := env.signedIn()
	user := env.state.CreateUserForTest("bob")
	node := env.state.CreateRegisteredNodeForTest(user, "laptop")
	env.state.PutNodeInStoreForTest(*node)
	source, loaded, err := env.state.PolicyEditorState()
	require.NoError(t, err)
	_, err = env.state.SavePolicyDraft(`{"tagOwners":{"tag:exit-a":[],"tag:exit-b":[]}}`, source.Revision, loaded.Revision)
	require.NoError(t, err)
	form := zonesForm(t, env, csrf, "zone")
	form.Set("replace_network", "on")
	form.Set("name", "Privat")
	form.Set("users", user.StringID())
	form["exits"] = []string{"tag:exit-a", "tag:exit-b"}
	response, _ := env.post(env.client, "/admin/zones", form)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	model := zoneModel(t, env)
	id := model.Zones[0].ID
	limit := zonesForm(t, env, csrf, "limit")
	limit.Set("target", "user:"+user.StringID())
	limit.Set("exits", "tag:exit-a")
	response, _ = env.post(env.client, "/admin/zones", limit)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.Equal(t, []string{"tag:exit-a"}, zoneModel(t, env).ExitsForNode(node.View()))
	limit = zonesForm(t, env, csrf, "limit")
	limit.Set("target", "node:"+strconv.FormatUint(node.ID.Uint64(), 10))
	response, _ = env.post(env.client, "/admin/zones", limit)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.Empty(t, zoneModel(t, env).ExitsForNode(node.View()))
	pool := zonesForm(t, env, csrf, "pool")
	pool.Set("zone", id)
	pool.Set("prefix", "2001:db8:42::/64")
	pool.Set("gateway", "tag:exit-a")
	response, _ = env.post(env.client, "/admin/zones", pool)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	reservation := zonesForm(t, env, csrf, "reservation")
	reservation.Set("zone", id)
	reservation.Set("prefix", "2001:db8:42::/64")
	reservation.Set("node", strconv.FormatUint(node.ID.Uint64(), 10))
	reservation.Set("address", "2001:db8:42::10")
	response, _ = env.post(env.client, "/admin/zones", reservation)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	model = zoneModel(t, env)
	require.Len(t, model.Zones[0].IPv6Pools[0].Reservations, 1)
	require.Equal(t, "2001:db8:42::10", model.Zones[0].IPv6Pools[0].Reservations[0].Address)

	_, body := env.get(env.client, "/admin/zones?zone="+id)
	require.Contains(t, body, "nicht automatisch auf Clients eingerichtet")
	require.Contains(t, body, "2001:db8:42::10")

	invalid := zonesForm(t, env, csrf, "reservation")
	for _, key := range []string{"zone", "prefix", "node"} {
		invalid.Set(key, reservation.Get(key))
	}

	invalid.Set("address", "2001:db8:43::10")
	response, _ = env.post(env.client, "/admin/zones", invalid)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.Equal(t, "2001:db8:42::10", zoneModel(t, env).Zones[0].IPv6Pools[0].Reservations[0].Address)
}
