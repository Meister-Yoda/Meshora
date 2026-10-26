package webui

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/require"
)

func TestUnifiedUserFirstLoginAndPrivilegeIsolation(t *testing.T) {
	env := newTestEnv(t)
	adminCSRF := env.signedIn()
	user, _, err := env.state.CreateUser(types.User{Name: "personal", Email: "person@example.test"})
	require.NoError(t, err)

	other := env.state.CreateUserForTest("other")
	own := env.state.CreateRegisteredNodeForTest(user, "my-device")
	env.state.PutNodeInStoreForTest(*own)
	foreign := env.state.CreateRegisteredNodeForTest(other, "foreign-device")
	env.state.PutNodeInStoreForTest(*foreign)

	path := fmt.Sprintf("/admin/users/%d", user.ID)
	response, body := env.action(adminCSRF, path+"/password", nil)
	require.Equal(t, path, response)

	match := regexp.MustCompile(`<pre class="codeblock secret"><code>([^<]+)</code></pre>`).FindStringSubmatch(body)
	require.Len(t, match, 2)
	password := html.UnescapeString(match[1])
	_, again := env.get(env.client, path)
	require.NotContains(t, again, password)
	client := newClient(t)
	login, _ := env.login(client, user.Email, password)
	require.Equal(t, http.StatusSeeOther, login.StatusCode)
	require.Equal(t, "/admin/account/password", login.Header.Get("Location"))

	blocked, _ := env.get(client, "/admin/users")
	require.Equal(t, "/admin/account/password", blocked.Header.Get("Location"))

	_, page := env.get(client, "/admin/account/password")
	token := csrfFrom(t, page)
	bad, _ := env.post(client, "/admin/account/password", url.Values{"password": {"new personal password"}, "password_confirm": {"new personal password"}})
	require.Equal(t, http.StatusForbidden, bad.StatusCode)

	changed, _ := env.post(client, "/admin/account/password", url.Values{"csrf_token": {token}, "current_password": {password}, "password": {"new personal password"}, "password_confirm": {"new personal password"}})
	require.Equal(t, http.StatusSeeOther, changed.StatusCode)

	rejected, _ := env.login(client, user.Email, password)
	require.Equal(t, http.StatusUnauthorized, rejected.StatusCode)

	login, _ = env.login(client, user.Email, "new personal password")
	require.Equal(t, "/admin/account", login.Header.Get("Location"))

	_, page = env.get(client, "/admin/account")
	require.Contains(t, page, "my-device")
	require.NotContains(t, page, "foreign-device")
	require.NotContains(t, page, `href="/admin/users"`)

	for _, adminPath := range []string{"/admin/users", "/admin/nodes", "/admin/zones", "/admin/keys", "/admin/settings"} {
		res, _ := env.get(client, adminPath)
		require.Equal(t, http.StatusForbidden, res.StatusCode, adminPath)
	}

	hidden, _ := env.get(client, fmt.Sprintf("/admin/account/devices/%d", foreign.ID))
	require.Equal(t, http.StatusNotFound, hidden.StatusCode)

	_, _ = env.action(adminCSRF, path+"/admin", url.Values{"is_admin": {"on"}})
	expired, _ := env.get(client, "/admin/account")
	require.Equal(t, "/", expired.Header.Get("Location"))

	login, _ = env.login(client, user.Email, "new personal password")
	require.Equal(t, "/admin/", login.Header.Get("Location"))

	allowed, _ := env.get(client, "/admin/users")
	require.Equal(t, http.StatusOK, allowed.StatusCode)
}

func TestUnifiedMergePreservesDeviceOwnership(t *testing.T) {
	env := newTestEnv(t)
	source := env.state.CreateUserForTest("legacy-user")
	target, _, err := env.state.CreateUser(types.User{Name: "canonical-user", Email: "canonical@example.test"})
	require.NoError(t, err)

	node := env.state.CreateRegisteredNodeForTest(source, "preserved-device")
	env.state.PutNodeInStoreForTest(*node)
	_, err = env.state.MergeUserAccounts(types.UserID(source.ID), types.UserID(target.ID), false)
	require.NoError(t, err)

	transferred, ok := env.state.GetNodeByID(node.ID)
	require.True(t, ok)
	require.Equal(t, types.UserID(target.ID), transferred.TypedUserID())
	require.Equal(t, node.ID, transferred.ID())
	_, err = env.state.GetUserByID(types.UserID(source.ID))
	require.Error(t, err)
}
