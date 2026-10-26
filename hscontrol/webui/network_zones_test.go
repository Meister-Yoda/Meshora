package webui

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

func TestZoneMatrixSelectionAndQuickActions(t *testing.T) {
	env := newTestEnv(t)
	csrf := env.signedIn()
	source, loaded, err := env.state.PolicyEditorState()
	require.NoError(t, err)

	text := `{"networkZones":{"zones":[{"id":"private","name":"Privat"},{"id":"server","name":"Server"},{"id":"guest","name":"Gäste"}],"links":[{"from":"private","to":"server","services":["tcp:443","tcp:22"]},{"from":"server","to":"guest","services":["*"]}]}}`
	_, err = env.state.SavePolicyDraft(text, source.Revision, loaded.Revision)
	require.NoError(t, err)

	response, body := env.get(env.client, "/admin/zones?from=private&to=server")
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Contains(t, body, "Zonenmatrix")
	require.Contains(t, body, "zone-matrix-allow")
	require.Contains(t, body, "zone-matrix-return")
	require.Contains(t, body, "zone-matrix-blocked")
	require.Contains(t, body, "zone-matrix-services")
	require.Contains(t, body, `aria-current="true"`)
	require.Contains(t, body, `selected value="private"`)
	require.NotContains(t, body, `id="zone-nodes"`)
	_, policySection, found := strings.Cut(body, `id="zone-policy-list"`)
	require.True(t, found)

	policySection, _, _ = strings.Cut(policySection, "</table>")
	require.NotContains(t, policySection, "Gäste", "the policy list must be filtered without hiding matrix zones")

	require.Contains(t, body, "tcp:443, tcp:22")
	require.NotContains(t, body, "<script>")
	require.Contains(t, body, `src="/admin/assets/zones.js"`)
	require.NotContains(t, body, "style=")
	// A quick allow updates only the selected pair and retains its filter.
	form := zonesForm(t, env, csrf, "link")
	form.Set("from", "private")
	form.Set("to", "server")
	form.Set("services", "*")
	response, _ = env.post(env.client, "/admin/zones", form)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.Equal(t, "/admin/zones?from=private&to=server#zone-policy-list", response.Header.Get("Location"))
	model := zoneModel(t, env)
	require.Len(t, model.Links, 2)
	require.Equal(t, []string{"*"}, model.Links[1].Services)
	// Unknown zone IDs cannot enter the editor as selected options.
	_, body = env.get(env.client, "/admin/zones?from=unknown&to=server")
	require.NotContains(t, body, `value="unknown"`)
}

func TestZoneFormPreservesScreenshotInputAndRetries(t *testing.T) {
	env := newTestEnv(t)
	csrf := env.signedIn()
	first := env.state.CreateUserForTest("alice")
	second := env.state.CreateUserForTest("bob")
	node := env.state.CreateRegisteredNodeForTest(first, "phone")
	env.state.PutNodeInStoreForTest(*node)
	form := zonesForm(t, env, csrf, "zone")
	form.Set("name", "Privat")
	form.Set("nodes", strconv.FormatUint(node.ID.Uint64(), 10))
	form["users"] = []string{first.StringID(), second.StringID()}
	form.Set("pool4", "10.3.1.0/24")
	form.Set("replace_network", "on")

	before, loaded, err := env.state.PolicyEditorState()
	require.NoError(t, err)

	response, body := env.post(env.client, "/admin/zones", form)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.Contains(t, body, "Der Mesh-IPv4-Pool muss innerhalb von 100.64.0.0/10 liegen")
	require.NotContains(t, body, "zone IDs must be unique")
	require.Contains(t, body, `id="zone-name"`)
	require.Contains(t, body, `value="Privat"`)
	require.Contains(t, body, `value="10.3.1.0/24"`)
	require.NotContains(t, body, `id="zone-users"`)

	require.Contains(t, body, `checked name="replace_network"`)
	require.Contains(t, body, `name="zone" type="hidden" value=""`)
	require.Contains(t, body, "IP-Adressbereich für die Vergabe")
	require.NotContains(t, body, `id="zone-subnets"`)

	after, _, err := env.state.PolicyEditorState()
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, loaded, env.state.PolicySnapshot())
	// Correct the field instead of inventing an address or changing policy rules.
	form.Del("pool4")
	form.Set("subnets", "10.3.1.0/24")
	response, _ = env.post(env.client, "/admin/zones", form)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	model := zoneModel(t, env)
	require.Len(t, model.Zones, 1)
	require.Equal(t, "Privat", model.Zones[0].Name)
	require.Equal(t, []string{"10.3.1.0/24"}, model.Zones[0].Subnets)
	require.NotEmpty(t, model.Zones[0].ID)
	require.Equal(t, loaded, env.state.PolicySnapshot())
}

func TestZoneFormRequiresNameAndRetainsConflictTokens(t *testing.T) {
	env := newTestEnv(t)
	csrf := env.signedIn()
	form := zonesForm(t, env, csrf, "zone")
	form.Set("name", "   ")
	form.Set("subnets", "192.168.12.0/24")
	form.Set("replace_network", "on")
	response, body := env.post(env.client, "/admin/zones", form)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.Contains(t, body, "Bitte gib einen Namen für die Zone ein")
	require.Contains(t, body, `aria-invalid="true"`)
	require.Contains(t, body, `required`)
	require.Contains(t, body, `value="   "`)
	require.NotContains(t, body, "zone IDs must be unique")
	// A later save by another form must not refresh the rejected form's lease.
	other := zonesForm(t, env, csrf, "zone")
	other.Set("name", "Server")
	other.Set("replace_network", "on")
	response, _ = env.post(env.client, "/admin/zones", other)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	form.Set("name", "Privat")
	response, body = env.post(env.client, "/admin/zones", form)
	require.Equal(t, http.StatusConflict, response.StatusCode)
	require.Contains(t, body, `value="Privat"`)
	require.Contains(t, body, `name="source_revision" type="hidden" value="`+form.Get("source_revision")+`"`)
	response, _ = env.post(env.client, "/admin/zones", form)
	require.Equal(t, http.StatusConflict, response.StatusCode)
	require.Len(t, zoneModel(t, env).Zones, 1)
	require.Equal(t, "Server", zoneModel(t, env).Zones[0].Name)
}

func TestZoneFormKeepsNewIDEmptyAfterModelValidation(t *testing.T) {
	env := newTestEnv(t)
	csrf := env.signedIn()
	form := zonesForm(t, env, csrf, "zone")
	form.Set("name", "Privat")
	form.Set("subnets", "0.0.0.0/0")
	form.Set("replace_network", "on")
	response, body := env.post(env.client, "/admin/zones", form)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.Contains(t, body, `name="zone" type="hidden" value=""`)
	require.Contains(t, body, `value="Privat"`)
	require.Contains(t, body, `value="Privat"`)
	form.Set("subnets", "10.3.1.0/24")
	response, _ = env.post(env.client, "/admin/zones", form)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.Len(t, zoneModel(t, env).Zones, 1)
}

func TestZoneAssignmentsOnEntityDetails(t *testing.T) {
	env := newTestEnv(t)
	csrf := env.signedIn()
	user := env.state.CreateUserForTest("alice-zone")
	node := env.state.CreateRegisteredNodeForTest(user, "laptop-zone")
	env.state.PutNodeInStoreForTest(*node)
	source, loaded, err := env.state.PolicyEditorState()
	require.NoError(t, err)
	_, err = env.state.SavePolicyDraft(`{"networkZones":{"zones":[{"id":"private","name":"Privat"},{"id":"server","name":"Server"}]}}`, source.Revision, loaded.Revision)
	require.NoError(t, err)

	userPath := "/admin/users/" + user.StringID()

	nodePath := "/admin/nodes/" + strconv.FormatUint(node.ID.Uint64(), 10)
	for _, path := range []string{userPath, nodePath} {
		response, body := env.get(env.client, path)
		require.Equal(t, http.StatusOK, response.StatusCode)
		require.Contains(t, body, `id="assignment-zone"`)
		require.Contains(t, body, "Zone hinzufügen")
		require.NotContains(t, body, "Zonenzuordnung entfernen")

		response, _ = env.post(env.client, path+"/zone", url.Values{"zone": {"private"}})
		require.Equal(t, http.StatusForbidden, response.StatusCode)
	}

	form := zonesForm(t, env, csrf, "")
	form.Set("zone", "private")
	response, _ := env.post(env.client, userPath+"/zone", form)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.Equal(t, "private", zoneModel(t, env).ZoneForNode(node.View()).ID)

	stale := form
	form = zonesForm(t, env, csrf, "")
	form.Set("zone", "server")
	response, _ = env.post(env.client, nodePath+"/zone", form)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.Equal(t, "server", zoneModel(t, env).ZoneForNode(node.View()).ID)

	for _, path := range []string{userPath, nodePath} {
		_, page := env.get(env.client, path)
		require.Contains(t, page, "Zone ändern")
		require.Contains(t, page, "Zonenzuordnung entfernen")
	}

	// Editing a zone through the reduced form must retain entity assignments.
	edit := zonesForm(t, env, csrf, "zone")
	edit.Set("zone", "server")
	edit.Set("name", "Server neu")
	edit.Set("simple_zone", "on")
	response, _ = env.post(env.client, "/admin/zones", edit)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.Equal(t, "server", zoneModel(t, env).ZoneForNode(node.View()).ID)
	response, _ = env.post(env.client, nodePath+"/zone", stale)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.Equal(t, "server", zoneModel(t, env).ZoneForNode(node.View()).ID)
	form = zonesForm(t, env, csrf, "")
	form.Set("zone", "")
	response, _ = env.post(env.client, nodePath+"/zone", form)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.Equal(t, "private", zoneModel(t, env).ZoneForNode(node.View()).ID)
	_, page := env.get(env.client, nodePath)
	require.Contains(t, page, "Zone hinzufügen")
	require.NotContains(t, page, "Zonenzuordnung entfernen")
	removeUser := zonesForm(t, env, csrf, "")
	removeUser.Set("zone", "")
	response, _ = env.post(env.client, userPath+"/zone", removeUser)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.Nil(t, zoneModel(t, env).ZoneForNode(node.View()))
	_, page = env.get(env.client, userPath)
	require.Contains(t, page, "Zone hinzufügen")
	require.NotContains(t, page, "Zonenzuordnung entfernen")
	require.Equal(t, loaded, env.state.PolicySnapshot(), "assignments must remain drafts")
	form = zonesForm(t, env, csrf, "zone")
	form.Set("name", "Neue Zone")
	form.Set("simple_zone", "on")
	form.Set("address_range", "10.3.1.0/24")
	response, body := env.post(env.client, "/admin/zones", form)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.Contains(t, body, `value="10.3.1.0/24"`)
	require.Contains(t, body, "100.64.0.0/10")
}

func TestExitProviderControls(t *testing.T) {
	env := newTestEnv(t)
	csrf := env.signedIn()
	user := env.state.CreateUserForTest("provider-user")
	node := env.state.CreateRegisteredNodeForTest(user, "provider-node")
	env.state.PutNodeInStoreForTest(*node)
	source, loaded, err := env.state.PolicyEditorState()
	require.NoError(t, err)
	_, err = env.state.SavePolicyDraft(`{"networkZones":{"zones":[]}}`, source.Revision, loaded.Revision)
	require.NoError(t, err)

	userPath := "/admin/users/" + user.StringID()

	nodePath := "/admin/nodes/" + strconv.FormatUint(node.ID.Uint64(), 10)
	for _, path := range []string{userPath, nodePath} {
		_, body := env.get(env.client, path)
		require.Contains(t, body, "Eigene Exits in der Benutzerzone sperren")

		response, _ := env.post(env.client, path+"/exit-provider", url.Values{"blocked": {"true"}})
		require.Equal(t, http.StatusForbidden, response.StatusCode)
		form := zonesForm(t, env, csrf, "")
		form.Set("blocked", "true")
		response, _ = env.post(env.client, path+"/exit-provider", form)
		require.Equal(t, http.StatusSeeOther, response.StatusCode)
	}

	require.True(t, zoneModel(t, env).ExitProviderBlocked(node.View()))
	form := zonesForm(t, env, csrf, "")
	form.Set("blocked", "false")
	response, _ := env.post(env.client, nodePath+"/exit-provider", form)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.True(t, zoneModel(t, env).ExitProviderBlocked(node.View()), "user block survives device unblock")
	form = zonesForm(t, env, csrf, "")
	form.Set("blocked", "false")
	response, _ = env.post(env.client, userPath+"/exit-provider", form)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.False(t, zoneModel(t, env).ExitProviderBlocked(node.View()))
	require.Equal(t, loaded, env.state.PolicySnapshot(), "provider controls must be drafts until activation")
}

func TestNamedZoneExitProviderSetting(t *testing.T) {
	env := newTestEnv(t)
	csrf := env.signedIn()
	form := zonesForm(t, env, csrf, "zone")
	form.Set("name", "Privat")
	form.Set("replace_network", "on")
	form.Set("simple_zone", "on")
	form.Set("block_own_exits", "on")
	response, _ := env.post(env.client, "/admin/zones", form)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	model := zoneModel(t, env)
	require.True(t, model.Zones[0].BlockOwnExits)
	_, body := env.get(env.client, "/admin/zones?zone="+model.Zones[0].ID)
	require.Contains(t, body, `checked name="block_own_exits"`)
	form = zonesForm(t, env, csrf, "zone")
	form.Set("zone", model.Zones[0].ID)
	form.Set("name", "Privat")
	form.Set("simple_zone", "on")
	response, _ = env.post(env.client, "/admin/zones", form)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.False(t, zoneModel(t, env).Zones[0].BlockOwnExits)
}

func TestZoneEditorModalRoutesAndErrors(t *testing.T) {
	env := newTestEnv(t)
	csrf := env.signedIn()
	response, body := env.get(env.client, "/admin/zones")
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Contains(t, body, "Neue Zone")
	require.Contains(t, body, `href="/admin/zones?new=1"`)
	require.Contains(t, response.Header.Get("Content-Security-Policy"), "script-src 'self'")
	require.NotContains(t, body, `id="zone-editor" open`)
	_, body = env.get(env.client, "/admin/zones?new=1")
	require.Contains(t, body, `id="zone-editor" open`)
	require.Contains(t, body, `aria-labelledby="zone-editor-title"`)
	form := zonesForm(t, env, csrf, "zone")
	form.Set("replace_network", "on")
	form.Set("name", "")
	response, body = env.post(env.client, "/admin/zones", form)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.Contains(t, body, `data-zone-error="true"`)
	require.Contains(t, body, `id="zone-editor" open`)
	form.Set("name", "Privat")
	response, _ = env.post(env.client, "/admin/zones", form)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	_, body = env.get(env.client, response.Header.Get("Location"))
	require.NotContains(t, body, `id="zone-editor" open`, "successful save returns to the overview")
	_, body = env.get(env.client, "/admin/zones?zone="+zoneModel(t, env).Zones[0].ID)
	require.Contains(t, body, "Zone bearbeiten: Privat")
	require.Contains(t, body, `id="zone-editor" open`)

	scriptResponse, script := env.get(env.client, "/admin/assets/zones.js")
	require.Equal(t, http.StatusOK, scriptResponse.StatusCode)
	require.Contains(t, scriptResponse.Header.Get("Content-Type"), "javascript")
	require.Contains(t, script, "showModal")
}

func TestOwnExitMatrixAndEntityInheritance(t *testing.T) {
	env := newTestEnv(t)
	csrf := env.signedIn()
	user := env.state.CreateUserForTest("own-exit-user")
	node := env.state.CreateRegisteredNodeForTest(user, "own-exit-client")
	env.state.PutNodeInStoreForTest(*node)
	source, loaded, err := env.state.PolicyEditorState()
	require.NoError(t, err)
	raw, err := json.Marshal(map[string]any{"networkZones": types.NetworkZones{Zones: []types.NetworkZone{{ID: "private", Name: "Privat", Users: []types.UserID{types.UserID(user.ID)}}}}, "groups": map[string][]string{"group:family": {"own-exit-user@"}}})
	require.NoError(t, err)
	_, err = env.state.SavePolicyDraft(string(raw), source.Revision, loaded.Revision)
	require.NoError(t, err)

	path := "/admin/zones?from=private&to=" + url.QueryEscape(types.OwnExitsTarget)
	response, body := env.get(env.client, path)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Contains(t, body, "Eigene Exits")
	require.Contains(t, body, "Nicht überschreibbare Sperre")
	form := zonesForm(t, env, csrf, "own-exits")
	form.Set("from", "private")
	form.Set("to", types.OwnExitsTarget)
	form.Set("own_mode", "deny")
	response, _ = env.post(env.client, "/admin/zones", form)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.Contains(t, response.Header.Get("Location"), "zone-policy-list")
	form = zonesForm(t, env, csrf, "own-exit-group")
	form.Set("group", "group:family")
	form.Set("own_mode", "allow")
	form.Set("own_services", "tcp:443-445")
	response, _ = env.post(env.client, "/admin/zones", form)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)

	userPath := "/admin/users/" + user.StringID()
	nodePath := "/admin/nodes/" + strconv.FormatUint(node.ID.Uint64(), 10)
	_, body = env.get(env.client, nodePath)
	require.Contains(t, body, "Gruppe: group:family")
	require.Contains(t, body, "tcp:443-445")

	for _, target := range []string{userPath, nodePath} {
		response, _ = env.post(env.client, target+"/own-exits", url.Values{"own_mode": {"allow"}, "own_services": {"*"}})
		require.Equal(t, http.StatusForbidden, response.StatusCode)
		form = zonesForm(t, env, csrf, "")
		form.Set("own_mode", "allow")
		form.Set("own_services", "tcp:22")
		response, _ = env.post(env.client, target+"/own-exits", form)
		require.Equal(t, http.StatusSeeOther, response.StatusCode)
	}

	_, body = env.get(env.client, nodePath)
	require.Contains(t, body, "Herkunft: Gerät")
	form = zonesForm(t, env, csrf, "own-exits")
	form.Set("from", "private")
	form.Set("to", types.OwnExitsTarget)
	form.Set("own_mode", "deny")
	form.Set("own_locked", "on")
	response, _ = env.post(env.client, "/admin/zones", form)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)

	_, body = env.get(env.client, nodePath)
	require.Contains(t, body, "Verbindlich gesperrt")
	require.Contains(t, body, "Herkunft: Zone: Privat")
	form = zonesForm(t, env, csrf, "")
	form.Set("own_mode", "inherit")
	response, _ = env.post(env.client, nodePath+"/own-exits", form)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)

	for _, override := range zoneModel(t, env).OwnExitOverrides {
		require.NotEqual(t, node.ID, override.Node)
	}
	// Erasing a device override must not erase a separate provider restriction.
	require.Empty(t, zoneModel(t, env).ExitProviderNodes)
	require.Equal(t, loaded, env.state.PolicySnapshot(), "all rule changes remain drafts")
}
