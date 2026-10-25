package webui

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

// signedIn logs the env's client in and returns the session CSRF token.
func (e *testEnv) signedIn() string {
	e.t.Helper()

	resp, _ := e.login(e.client, testAdmin, testPassword)
	require.Equal(e.t, http.StatusSeeOther, resp.StatusCode)

	_, body := e.get(e.client, "/admin/")

	return csrfFrom(e.t, body)
}

// action POSTs form (plus the CSRF token) to path, expects a 303 and returns
// the page it redirects to.
func (e *testEnv) action(csrf, path string, form url.Values) (string, string) {
	e.t.Helper()

	if form == nil {
		form = url.Values{}
	}

	form.Set("csrf_token", csrf)

	resp, _ := e.post(e.client, path, form)
	require.Equal(e.t, http.StatusSeeOther, resp.StatusCode, path)

	location := resp.Header.Get("Location")
	_, body := e.get(e.client, location)

	return location, body
}

func newRegistration(hostname string) (types.AuthID, *types.AuthRequest) {
	id := types.MustAuthID()
	req := types.NewRegisterAuthRequest(&types.RegistrationData{
		MachineKey: key.NewMachine().Public(),
		NodeKey:    key.NewNode().Public(),
		DiscoKey:   key.NewDisco().Public(),
		Hostname:   hostname,
		Hostinfo:   &tailcfg.Hostinfo{Hostname: hostname, OS: "windows"},
	})

	return id, req
}

func waitVerdict(t *testing.T, req *types.AuthRequest) types.AuthVerdict {
	t.Helper()

	select {
	case v := <-req.WaitForAuth():
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("no verdict for the auth request")

		return types.AuthVerdict{}
	}
}

func TestActionsRequireCSRF(t *testing.T) {
	env := newTestEnv(t)
	user := env.state.CreateUserForTest("bob")
	node := env.state.CreateRegisteredNodeForTest(user, "laptop")
	env.state.PutNodeInStoreForTest(*node)

	id, req := newRegistration("winpc")
	env.state.SetAuthCacheEntry(id, req)
	key, err := env.state.CreatePreAuthKey(user.TypedID(), true, false, &time.Time{}, nil)
	require.NoError(t, err)

	env.signedIn()

	nodePath := fmt.Sprintf("/admin/nodes/%d", node.ID)
	for _, path := range []string{
		"/admin/users",
		fmt.Sprintf("/admin/users/%d/rename", user.ID),
		fmt.Sprintf("/admin/users/%d/delete", user.ID),
		nodePath + "/rename",
		nodePath + "/tags",
		nodePath + "/expire",
		nodePath + "/delete",
		"/admin/pending/" + id.String() + "/assign",
		"/admin/pending/" + id.String() + "/reject",
		"/admin/pending/" + id.String() + "/approve",
		"/admin/keys",
		fmt.Sprintf("/admin/keys/%d/expire", key.ID),
	} {
		for _, token := range []string{"", "forged"} {
			resp, _ := env.post(env.client, path, url.Values{
				"csrf_token": {token},
				"name":       {"mallory"},
				"user_id":    {strconv.FormatUint(uint64(user.ID), 10)},
			})
			assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%s token=%q", path, token)
		}
	}

	_, ok := env.state.GetNodeByID(node.ID)
	assert.True(t, ok, "node must survive forged requests")
	assert.False(t, req.IsFinished(), "pending request must stay undecided")

	_, err = env.state.GetUserByName("mallory")
	require.Error(t, err)
	keys, err := env.state.ListPreAuthKeys()
	require.NoError(t, err)

	for _, stored := range keys {
		if stored.ID == key.ID {
			require.NotNil(t, stored.Expiration)
			assert.True(t, stored.Expiration.IsZero(), "forged requests must not expire keys")
		}
	}

	assert.Zero(t, env.changeCount())
}

func TestAssignPendingRegistration(t *testing.T) {
	env := newTestEnv(t)

	response, guide := env.get(env.client, "/connect?platform=windows&lang=en")
	require.Equal(t, http.StatusOK, response.StatusCode)
	assert.Contains(t, guide, "An administrator must review the request")
	assert.NotContains(t, guide, "csrf_token")
	user := env.state.CreateUserForTest("sebastian")

	id, req := newRegistration("winpc")
	env.state.SetAuthCacheEntry(id, req)

	csrf := env.signedIn()

	_, list := env.get(env.client, "/admin/pending")
	assert.Contains(t, list, `href="/admin/pending/`+id.String()+`"`)

	_, review := env.get(env.client, "/admin/pending/"+id.String())
	assert.Contains(t, review, ">Assign to a user<")
	assert.Contains(t, review, fmt.Sprintf(`value="%d"`, user.ID))

	location, body := env.action(csrf, "/admin/pending/"+id.String()+"/assign",
		url.Values{"user_id": {strconv.FormatUint(uint64(user.ID), 10)}})

	verdict := waitVerdict(t, req)
	require.True(t, verdict.Accept(), "the waiting client gets an accepting verdict")
	require.True(t, verdict.Node.Valid())
	assert.Equal(t, "winpc", verdict.Node.Hostname())
	assert.Equal(t, types.UserID(user.ID), verdict.Node.TypedUserID())
	assert.False(t, verdict.Node.IsTagged())

	assert.Equal(t, fmt.Sprintf("/admin/nodes/%d", verdict.Node.ID()), location)
	assert.Contains(t, body, "Registered winpc for sebastian.")
	assert.Contains(t, body, "Status and next steps")
	assert.Contains(t, body, "Offline: no active connection")
	assert.Contains(t, body, "Last activity (UTC)")

	_, devices := env.get(env.client, "/admin/nodes?q=winpc&lang=en")
	assert.Contains(t, devices, "1 / 1 devices shown")
	assert.Contains(t, devices, location)

	_, translated := env.get(env.client, location+"?lang=de")
	assert.Contains(t, translated, "Zustand und n\u00e4chste Schritte")
	assert.Positive(t, env.changeCount(), "connected nodes must be told about the new node")

	_, list = env.get(env.client, "/admin/pending?lang=en")
	assert.Contains(t, list, "Nothing pending")

	// The flash is shown once.
	_, again := env.get(env.client, location)
	assert.NotContains(t, again, "Registered winpc")
}

func TestRejectPending(t *testing.T) {
	env := newTestEnv(t)
	src := env.state.CreateRegisteredNodeForTest(env.state.CreateUserForTest("carol"), "src")
	env.state.PutNodeInStoreForTest(*src)

	regID, reg := newRegistration("intruder")
	env.state.SetAuthCacheEntry(regID, reg)

	sshID := types.MustAuthID()
	ssh := types.NewSSHCheckAuthRequest(src.ID, src.ID)
	env.state.SetAuthCacheEntry(sshID, ssh)

	approveID := types.MustAuthID()
	approve := types.NewSSHCheckAuthRequest(src.ID, src.ID)
	env.state.SetAuthCacheEntry(approveID, approve)

	csrf := env.signedIn()

	_, body := env.action(csrf, "/admin/pending/"+regID.String()+"/reject", nil)
	assert.Contains(t, body, "Rejected the registration.")
	require.ErrorIs(t, waitVerdict(t, reg).Err, errAuthRejected)
	assert.NotContains(t, body, "intruder", "decided requests are hidden")

	_, body = env.action(csrf, "/admin/pending/"+sshID.String()+"/reject", nil)
	assert.Contains(t, body, "Rejected the SSH check.")
	assert.False(t, waitVerdict(t, ssh).Accept())

	_, body = env.action(csrf, "/admin/pending/"+approveID.String()+"/approve", nil)
	assert.Contains(t, body, "Approved the SSH check.")
	assert.True(t, waitVerdict(t, approve).Accept())
	assert.Contains(t, body, "Nothing pending")

	// A decided request cannot be decided again.
	_, body = env.action(csrf, "/admin/pending/"+approveID.String()+"/reject", nil)
	assert.Contains(t, body, "That request is no longer pending.")

	resp, _ := env.get(env.client, "/admin/pending/not-an-auth-id")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestNodeActions(t *testing.T) {
	env := newTestEnv(t)
	user := env.state.CreateUserForTest("bob")
	node := env.state.CreateRegisteredNodeForTest(user, "laptop")
	node.User = user
	env.state.PutNodeInStoreForTest(*node)

	_, err := env.state.SetPolicy([]byte(`{"tagOwners":{"tag:server":["bob@"]}}`))
	require.NoError(t, err)

	csrf := env.signedIn()
	path := fmt.Sprintf("/admin/nodes/%d", node.ID)

	_, detail := env.get(env.client, path)
	assert.Contains(t, detail, `action="`+path+`/rename"`)

	_, body := env.action(csrf, path+"/rename", url.Values{"name": {"office-pc"}})
	assert.Contains(t, body, "Renamed the node to office-pc.")

	renamed, _ := env.state.GetNodeByID(node.ID)
	assert.Equal(t, "office-pc", renamed.GivenName())

	_, body = env.action(csrf, path+"/tags", url.Values{"tags": {"Tag:Bad"}})
	assert.Contains(t, body, "Could not set the tags")

	_, body = env.action(csrf, path+"/tags", url.Values{"tags": {"tag:server"}})
	assert.Contains(t, body, "Set tags to tag:server.")

	tagged, _ := env.state.GetNodeByID(node.ID)
	assert.True(t, tagged.IsTagged(), "tags take ownership of the node")

	_, body = env.action(csrf, path+"/tags", url.Values{"tags": {""}})
	assert.Contains(t, body, "at least one tag")

	resp, confirm := env.get(env.client, path+"/delete")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, confirm, ">Delete node<")
	assert.Contains(t, confirm, `action="`+path+`/delete"`)

	_, ok := env.state.GetNodeByID(node.ID)
	assert.True(t, ok, "the confirmation page itself deletes nothing")

	location, body := env.action(csrf, path+"/delete", nil)
	assert.Equal(t, "/admin/nodes", location)
	assert.Contains(t, body, "Deleted office-pc.")

	_, ok = env.state.GetNodeByID(node.ID)
	assert.False(t, ok)
	assert.Positive(t, env.changeCount())

	resp, _ = env.get(env.client, path)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestExpireNode(t *testing.T) {
	env := newTestEnv(t)
	user := env.state.CreateUserForTest("bob")
	node := env.state.CreateRegisteredNodeForTest(user, "laptop")
	node.User = user
	env.state.PutNodeInStoreForTest(*node)

	csrf := env.signedIn()
	path := fmt.Sprintf("/admin/nodes/%d", node.ID)

	_, confirm := env.get(env.client, path+"/expire")
	assert.Contains(t, confirm, ">Expire node<")

	_, body := env.action(csrf, path+"/expire", nil)
	assert.Contains(t, body, "Expired laptop.")

	expired, _ := env.state.GetNodeByID(node.ID)
	assert.True(t, expired.IsExpired())
}

func TestUserActions(t *testing.T) {
	env := newTestEnv(t)
	csrf := env.signedIn()

	_, list := env.get(env.client, "/admin/users")
	assert.Contains(t, list, ">Add a user<")

	_, body := env.action(csrf, "/admin/users", url.Values{"name": {""}})
	assert.Contains(t, body, "Enter a username.")

	location, body := env.action(csrf, "/admin/users", url.Values{
		"name":         {"dave"},
		"display_name": {xssPayload},
		"email":        {"dave@example.com"},
	})
	assert.Contains(t, body, "Created user dave.")
	assertEscaped(t, body)

	user, err := env.state.GetUserByName("dave")
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("/admin/users/%d", user.ID), location)
	assert.Contains(t, body, "&lt;script&gt;alert(1)&lt;/script&gt;")

	_, body = env.action(csrf, "/admin/users", url.Values{"name": {"dave"}})
	assert.Contains(t, body, "already exists")

	_, body = env.action(csrf, location+"/rename", url.Values{"name": {"david"}})
	assert.Contains(t, body, "Renamed the user to david.")

	// A user who owns a device cannot be deleted.
	node := env.state.CreateRegisteredNodeForTest(user, "phone")
	env.state.PutNodeInStoreForTest(*node)

	_, confirm := env.get(env.client, location+"/delete")
	assert.Contains(t, confirm, ">Delete user<")
	assertEscaped(t, confirm)

	_, body = env.action(csrf, location+"/delete", nil)
	assert.Contains(t, body, "still owns devices")

	changes, err := env.state.DeleteNode(env.state.ListNodes().At(0))
	require.NoError(t, err)
	require.NotNil(t, changes)

	location, body = env.action(csrf, location+"/delete", nil)
	assert.Equal(t, "/admin/users", location)
	assert.Contains(t, body, "Deleted user")

	_, err = env.state.GetUserByName("david")
	require.Error(t, err)
}

func TestPreAuthKeyActions(t *testing.T) {
	env := newTestEnv(t)
	user := env.state.CreateUserForTest("bob")
	csrf := env.signedIn()

	_, body := env.action(csrf, "/admin/keys", url.Values{"expiry": {"24h"}})
	assert.Contains(t, body, "Choose a user or enter tags")

	_, body = env.action(csrf, "/admin/keys", url.Values{
		"user_id": {strconv.FormatUint(uint64(user.ID), 10)}, "expiry": {"999h"},
	})
	assert.Contains(t, body, "Choose how long the key stays valid.")

	_, body = env.action(csrf, "/admin/keys", url.Values{
		"user_id":  {strconv.FormatUint(uint64(user.ID), 10)},
		"expiry":   {"24h"},
		"reusable": {"on"},
	})
	assert.Contains(t, body, "it is not shown again")

	keys, err := env.state.ListPreAuthKeys()
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.True(t, keys[0].Reusable)
	assert.WithinDuration(t, time.Now().Add(24*time.Hour), *keys[0].Expiration, time.Minute)

	full := "hskey-auth-" + keys[0].Prefix + "-"
	assert.Equal(t, 2, strings.Count(body, full), "the new key in full, plus its masked list entry")
	secret := regexp.MustCompile(`hskey-auth-[a-f0-9]{12}-[a-f0-9]{64}`).FindString(body)
	require.NotEmpty(t, secret, "the newly generated key must be shown in full")

	_, again := env.get(env.client, "/admin/keys")
	assert.NotContains(t, again, secret, "subsequent requests must never reveal the secret")
	assert.NotContains(t, again, "it is not shown again", "the key is shown only once")
	assert.Equal(t, 1, strings.Count(again, full), "only the masked key remains")
	assert.Contains(t, again, full+"***")

	path := fmt.Sprintf("/admin/keys/%d/expire", keys[0].ID)
	userKeyID := keys[0].ID

	_, confirm := env.get(env.client, path)
	assert.Contains(t, confirm, ">Expire pre-auth key<")

	_, body = env.action(csrf, path, nil)
	assert.Contains(t, body, "Expired key")
	assert.Contains(t, body, ">Expired<")

	_, body = env.action(csrf, "/admin/keys", url.Values{"tags": {"tag:ci"}, "expiry": {"1h"}})
	assert.Contains(t, body, "it is not shown again")

	keys, err = env.state.ListPreAuthKeys()
	require.NoError(t, err)
	require.Len(t, keys, 2)

	for _, key := range keys {
		if key.ID != userKeyID {
			assert.Equal(t, []string{"tag:ci"}, key.Tags)
			assert.Nil(t, key.UserID, "tags own the key without a user")
		}
	}
}

func TestActionPagesUnknownIDs(t *testing.T) {
	env := newTestEnv(t)

	csrf := env.signedIn()
	for _, path := range []string{
		"/admin/users/999999", "/admin/users/999999/delete",
		"/admin/nodes/999999", "/admin/nodes/999999/delete", "/admin/nodes/999999/expire",
		"/admin/keys/999999/expire", "/admin/nodes/invalid",
	} {
		resp, _ := env.get(env.client, path)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, path)
	}

	for _, path := range []string{
		"/admin/users/999999/delete", "/admin/users/999999/rename", "/admin/nodes/999999/rename",
		"/admin/nodes/999999/tags", "/admin/nodes/999999/expire",
		"/admin/nodes/999999/delete", "/admin/keys/999999/expire",
	} {
		resp, _ := env.post(env.client, path, url.Values{"csrf_token": {csrf}, "name": {"missing-user"}})
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, path)
	}

	assert.Zero(t, env.changeCount())
}

func TestActionPagesEscapeUserData(t *testing.T) {
	env := newTestEnv(t)
	user := env.state.CreateUserForTest("eve")
	require.NoError(t, env.state.DB().DB.Model(user).Update("display_name", xssPayload).Error)

	node := env.state.CreateRegisteredNodeForTest(user, "laptop")
	node.User = user
	node.Hostname = xssPayload
	env.state.PutNodeInStoreForTest(*node)

	id, req := newRegistration(xssPayload)
	env.state.SetAuthCacheEntry(id, req)

	env.signedIn()

	for _, path := range []string{
		fmt.Sprintf("/admin/users/%d", user.ID),
		fmt.Sprintf("/admin/users/%d/delete", user.ID),
		fmt.Sprintf("/admin/nodes/%d", node.ID),
		fmt.Sprintf("/admin/nodes/%d/delete", node.ID),
		"/admin/pending/" + id.String(),
		"/admin/keys",
	} {
		resp, body := env.get(env.client, path)
		require.Equal(t, http.StatusOK, resp.StatusCode, path)
		assertEscaped(t, body)
	}
}
