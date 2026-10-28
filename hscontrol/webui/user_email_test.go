package webui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"testing"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/require"
)

func emailUser(t *testing.T, env *testEnv) (*http.Client, types.UserID, string) {
	t.Helper()

	password := "email account password"
	_, createErr := env.state.CreateWebAdmin("email-user", password)
	require.NoError(t, createErr)

	account, err := env.state.AuthenticateUserAccount("email-user", password)
	require.NoError(t, err)

	id := types.UserID(account.UserID)
	require.NoError(t, env.state.SetUserAdmin(id, false))
	client := newClient(t)
	response, _ := env.login(client, "email-user", password)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)

	return client, id, password
}

func enableTestEmail(env *testEnv) {
	env.handler.mailConfig = types.MailConfig{Host: "smtp.example.test", Port: 465, From: "meshora@example.test", TLSMode: "tls"}
}

func TestAccountEmailRequiresProofAndKeepsUID(t *testing.T) {
	env := newTestEnv(t)
	enableTestEmail(env)
	client, id, password := emailUser(t, env)

	var sentBody, recipient string

	env.handler.emailSender = func(_ context.Context, _ types.MailConfig, to, body string) error {
		recipient, sentBody = to, body
		return nil
	}
	_, page := env.get(client, "/account/email")
	csrf := csrfFrom(t, page)
	require.Contains(t, page, "account-email")

	bad, _ := env.post(client, "/account/email", url.Values{"email": {"new@example.test"}, "current_password": {password}})
	require.Equal(t, http.StatusForbidden, bad.StatusCode)

	result, _ := env.post(client, "/account/email", url.Values{"csrf_token": {csrf}, "email": {"new@example.test"}, "current_password": {password}})
	require.Equal(t, http.StatusSeeOther, result.StatusCode)
	require.Equal(t, "new@example.test", recipient)

	match := regexp.MustCompile("#token=([A-Za-z0-9_-]+)").FindStringSubmatch(sentBody)
	require.Len(t, match, 2)
	token := match[1]
	pending, err := env.state.PendingUserEmailChange(id)
	require.NoError(t, err)
	require.NotNil(t, pending)
	require.NotEqual(t, token, pending.TokenHash)

	user, err := env.state.GetUserByID(id)
	require.NoError(t, err)
	require.Empty(t, user.Email)

	_, confirm := env.get(client, "/account/email/confirm")
	require.NotContains(t, confirm, token)
	require.Contains(t, confirm, "email-confirm-token")
	env.signedIn()
	_, adminPage := env.get(env.client, "/account")
	cross, _ := env.post(env.client, "/account/email/confirm", url.Values{"csrf_token": {csrfFrom(t, adminPage)}, "token": {token}})
	require.Equal(t, http.StatusSeeOther, cross.StatusCode)

	user, err = env.state.GetUserByID(id)
	require.NoError(t, err)
	require.Empty(t, user.Email)

	response, _ := env.post(client, "/account/email/confirm", url.Values{"csrf_token": {csrf}, "token": {token}})
	require.Equal(t, http.StatusSeeOther, response.StatusCode)

	user, err = env.state.GetUserByID(id)
	require.NoError(t, err)
	require.Equal(t, "new@example.test", user.Email)
	require.Equal(t, uint(id), user.ID)

	expired, _ := env.get(client, "/account")
	require.Equal(t, "/", expired.Header.Get("Location"))

	login, _ := env.login(client, "new@example.test", password)
	require.Equal(t, http.StatusSeeOther, login.StatusCode)
}

var errTestSMTPDelivery = errors.New("sensitive smtp failure")

func TestAccountEmailBlockedAndDeliveryFailure(t *testing.T) {
	env := newTestEnv(t)
	enableTestEmail(env)
	client, id, password := emailUser(t, env)
	env.handler.emailSender = func(context.Context, types.MailConfig, string, string) error {
		return errTestSMTPDelivery
	}
	_, page := env.get(client, "/account/email")
	csrf := csrfFrom(t, page)
	_, _ = env.post(client, "/account/email", url.Values{"csrf_token": {csrf}, "email": {"new@example.test"}, "current_password": {password}})
	pending, err := env.state.PendingUserEmailChange(id)
	require.NoError(t, err)
	require.Nil(t, pending)

	_, page = env.get(client, "/account/email")
	require.NotContains(t, page, "sensitive smtp failure")

	adminCSRF := env.signedIn()
	_, adminPage := env.get(env.client, fmt.Sprintf("/admin/users/%d", id))
	require.Contains(t, adminPage, "allow_email_change")
	// A plain administrative session proves itself using its current password.
	response, _ := env.post(env.client, fmt.Sprintf("/admin/users/%d/email-policy", id), url.Values{"csrf_token": {adminCSRF}, "current_password": {testPassword}})
	require.Equal(t, http.StatusSeeOther, response.StatusCode)

	policy, err := env.state.UserAuthPolicy(id)
	require.NoError(t, err)
	require.True(t, policy.EmailChangeBlocked)

	login, _ := env.login(client, "email-user", password)
	require.Equal(t, http.StatusSeeOther, login.StatusCode)

	_, page = env.get(client, "/account/email")
	require.NotContains(t, page, `name="email"`)
	csrf = csrfFrom(t, page)
	response, _ = env.post(client, "/account/email", url.Values{"csrf_token": {csrf}, "email": {"next@example.test"}, "current_password": {password}})
	require.Equal(t, http.StatusSeeOther, response.StatusCode)

	pending, err = env.state.PendingUserEmailChange(id)
	require.NoError(t, err)
	require.Nil(t, pending)
}

func TestMailSettingsAreAdminOnlyAndDoNotExportPassword(t *testing.T) {
	env, path := settingsTestEnv(t, "server_url: http://localhost:8080\nweb_admin:\n  enabled: true\n")
	_ = path
	csrf := env.signedIn()
	response, _ := env.post(env.client, "/admin/settings/mail", url.Values{"csrf_token": {csrf}, "host": {"smtp.example.test"}, "port": {"465"}, "tls_mode": {"tls"}, "from": {"meshora@example.test"}, "username": {"mailer"}, "password": {"private-mail-password"}, "current_password": {testPassword}})
	require.Equal(t, http.StatusSeeOther, response.StatusCode)

	_, page := env.get(env.client, "/admin/settings/mail")
	require.Contains(t, page, "smtp.example.test")
	require.NotContains(t, page, "private-mail-password")
	client, _, _ := emailUser(t, env)
	forbidden, _ := env.get(client, "/admin/settings/mail")
	require.Equal(t, http.StatusForbidden, forbidden.StatusCode)
}

func TestJMAPMailSettingsPreserveSeparateSecrets(t *testing.T) {
	env, path := settingsTestEnv(t, "server_url: http://localhost:8080\nweb_admin:\n  enabled: true\n")
	csrf := env.signedIn()
	smtp := url.Values{"csrf_token": {csrf}, "transport": {"smtp"}, "host": {"smtp.example.test"}, "port": {"465"}, "tls_mode": {"tls"}, "from": {"meshora@example.test"}, "username": {"mailer"}, "password": {"smtp-private"}, "current_password": {testPassword}}
	response, _ := env.post(env.client, "/admin/settings/mail", smtp)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.Equal(t, "smtp-private", env.handler.mailConfig.Password)
	_, page := env.get(env.client, "/admin/settings/mail")
	jmap := url.Values{"csrf_token": {csrfFrom(t, page)}, "transport": {"jmap"}, "jmap_session_url": {"https://mail.example.test/session"}, "jmap_token": {"jmap-private"}, "host": {"smtp.example.test"}, "port": {"465"}, "tls_mode": {"tls"}, "from": {"meshora@example.test"}, "username": {"mailer"}, "current_password": {testPassword}}
	response, _ = env.post(env.client, "/admin/settings/mail", jmap)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.Equal(t, "jmap", env.handler.mailConfig.Transport)
	require.Equal(t, "smtp-private", env.handler.mailConfig.Password)
	require.Equal(t, "jmap-private", env.handler.mailConfig.JMAPToken)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "jmap_token: jmap-private")
	require.Contains(t, string(data), "password: smtp-private")

	_, page = env.get(env.client, "/admin/settings/mail")
	require.NotContains(t, page, "smtp-private")
	require.NotContains(t, page, "jmap-private")
	jmap.Set("csrf_token", csrfFrom(t, page))
	jmap.Set("jmap_token", "")
	response, _ = env.post(env.client, "/admin/settings/mail", jmap)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.Equal(t, "jmap-private", env.handler.mailConfig.JMAPToken)
	_, page = env.get(env.client, "/admin/settings/mail")
	jmap.Set("csrf_token", csrfFrom(t, page))
	jmap.Set("jmap_session_url", "http://mail.example.test/session")
	response, _ = env.post(env.client, "/admin/settings/mail", jmap)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	require.Equal(t, "https://mail.example.test/session", env.handler.mailConfig.JMAPSessionURL)
}
