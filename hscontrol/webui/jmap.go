package webui

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/juanfont/headscale/hscontrol/db"
	"github.com/juanfont/headscale/hscontrol/types"
)

const (
	jmapCore       = "urn:ietf:params:jmap:core"
	jmapMail       = "urn:ietf:params:jmap:mail"
	jmapSubmission = "urn:ietf:params:jmap:submission"
)

func jmapURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	return u, err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Opaque == ""
}

func validJMAPConfig(cfg types.MailConfig) bool {
	_, ok := jmapURL(cfg.JMAPSessionURL)
	from, err := db.NormalizeAccountEmail(cfg.From)

	return ok && err == nil && from != "" && !strings.ContainsAny(cfg.From+cfg.JMAPUsername+cfg.JMAPToken, "\r\n") &&
		(cfg.JMAPToken != "" || cfg.JMAPTokenPath != "") && (cfg.JMAPToken == "" || cfg.JMAPTokenPath == "") &&
		len(cfg.JMAPToken) <= 4096
}

type jmapConnection struct {
	client   *http.Client
	endpoint string
	username string
	token    string
	account  string
}

func (c *jmapConnection) request(ctx context.Context, endpoint string, payload any, result any) error {
	var body io.Reader

	method := http.MethodGet

	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return errAccountMail
		}

		body, method = bytes.NewReader(encoded), http.MethodPost
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return errAccountMail
	}

	req.Header.Set("Accept", "application/json")

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.username != "" {
		req.SetBasicAuth(c.username, c.token)
	} else {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.client.Do(req)
	if err != nil {
		return errAccountMail
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return errAccountMail
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 || json.Unmarshal(data, result) != nil {
		return errAccountMail
	}

	return nil
}

func (c *jmapConnection) call(ctx context.Context, method string, args map[string]any, result any) error {
	args["accountId"] = c.account

	var response struct{ MethodResponses [][]json.RawMessage }

	err := c.request(ctx, c.endpoint, map[string]any{
		"using":       []string{jmapCore, jmapMail, jmapSubmission},
		"methodCalls": []any{[]any{method, args, "meshora"}},
	}, &response)
	if err != nil {
		return err
	}
	// Submission can append an implicit Email/set response. Match both method and call ID.
	for _, item := range response.MethodResponses {
		if len(item) != 3 {
			return errAccountMail
		}

		var name, id string
		if json.Unmarshal(item[0], &name) != nil || json.Unmarshal(item[2], &id) != nil {
			return errAccountMail
		}

		if id != "meshora" {
			continue
		}

		if name == "error" {
			return errAccountMail
		}

		if name == method {
			var account struct{ AccountID string }
			if json.Unmarshal(item[1], &account) != nil || account.AccountID != c.account || json.Unmarshal(item[1], result) != nil {
				return errAccountMail
			}

			return nil
		}
	}

	return errAccountMail
}

// Root overrides are private to local TLS tests; production uses system trust.
func deliverJMAPMail(ctx context.Context, cfg types.MailConfig, recipient, body string, roots *x509.CertPool) error {
	if !validJMAPConfig(cfg) || strings.ContainsAny(recipient, "\r\n") || strings.ContainsRune(body, 0) {
		return errAccountMail
	}

	to, err := db.NormalizeAccountEmail(recipient)
	if err != nil {
		return errAccountMail
	}

	from, err := db.NormalizeAccountEmail(cfg.From)
	if err != nil {
		return errAccountMail
	}

	token := cfg.JMAPToken
	if cfg.JMAPTokenPath != "" {
		secret, readErr := os.ReadFile(cfg.JMAPTokenPath)
		if readErr != nil || len(secret) > 4096 {
			return errAccountMail
		}

		token = strings.TrimSpace(string(secret))
		clear(secret)
	}

	if token == "" || strings.ContainsAny(token, "\r\n") {
		return errAccountMail
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}}
	defer transport.CloseIdleConnections()

	connection := &jmapConnection{
		client:   &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		username: cfg.JMAPUsername, token: token,
	}
	if connection.discover(ctx, cfg) != nil {
		return errAccountMail
	}

	var identities struct {
		List []struct {
			ID    string
			Email string
		}
	}
	if connection.call(ctx, "Identity/get", map[string]any{}, &identities) != nil {
		return errAccountMail
	}

	identityID := ""

	for _, identity := range identities.List {
		email, normalizeErr := db.NormalizeAccountEmail(identity.Email)
		if normalizeErr == nil && email == from && (cfg.JMAPIdentityID == "" || cfg.JMAPIdentityID == identity.ID) {
			if identityID != "" {
				return errAccountMail
			}

			identityID = identity.ID
		}
	}

	if identityID == "" {
		return errAccountMail
	}

	var mailboxes struct {
		List []struct {
			ID       string
			Role     string
			MyRights struct{ MayAddItems bool }
		}
	}
	if connection.call(ctx, "Mailbox/get", map[string]any{"properties": []string{"id", "role", "myRights"}}, &mailboxes) != nil {
		return errAccountMail
	}

	drafts := ""

	for _, mailbox := range mailboxes.List {
		if mailbox.Role == "drafts" && mailbox.MyRights.MayAddItems {
			drafts = mailbox.ID
			break
		}
	}

	if drafts == "" {
		return errAccountMail
	}

	var created struct {
		Created map[string]struct{ ID string }
	}

	message := map[string]any{
		"mailboxIds": map[string]bool{drafts: true}, "keywords": map[string]bool{"$draft": true},
		"from": []any{map[string]string{"email": from}}, "to": []any{map[string]string{"email": to}},
		"subject":    "Meshora: E-Mail-Adresse bestätigen",
		"textBody":   []any{map[string]string{"partId": "body", "type": "text/plain"}},
		"bodyValues": map[string]any{"body": map[string]string{"value": body}},
	}
	if connection.call(ctx, "Email/set", map[string]any{"create": map[string]any{"message": message}}, &created) != nil {
		return errAccountMail
	}

	emailID := created.Created["message"].ID
	if emailID == "" {
		return errAccountMail
	}
	// Remove the temporary draft even if submission fails. This never retries submission.
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cleanupCancel()

		var result json.RawMessage

		_ = connection.call(cleanupCtx, "Email/set", map[string]any{"destroy": []string{emailID}}, &result)
	}()

	var submitted struct {
		Created map[string]struct{ ID string }
	}

	err = connection.call(ctx, "EmailSubmission/set", map[string]any{
		"create": map[string]any{"submission": map[string]any{
			"identityId": identityID, "emailId": emailID,
			"envelope": map[string]any{"mailFrom": map[string]string{"email": from}, "rcptTo": []any{map[string]string{"email": to}}},
		}},
		"onSuccessDestroyEmail": []string{"#submission"},
	}, &submitted)
	if err != nil || submitted.Created["submission"].ID == "" {
		return errAccountMail
	}

	return nil
}

func (c *jmapConnection) discover(ctx context.Context, cfg types.MailConfig) error {
	var session struct {
		APIURL          string
		Capabilities    map[string]json.RawMessage
		PrimaryAccounts map[string]string
		Accounts        map[string]struct {
			IsReadOnly          bool
			AccountCapabilities map[string]json.RawMessage
		}
	}
	if c.request(ctx, cfg.JMAPSessionURL, nil, &session) != nil {
		return errAccountMail
	}

	origin, _ := jmapURL(cfg.JMAPSessionURL)
	api, ok := jmapURL(session.APIURL)
	// Never forward credentials to a different origin discovered in an untrusted response.
	if !ok || !strings.EqualFold(origin.Host, api.Host) {
		return errAccountMail
	}

	for _, capability := range []string{jmapCore, jmapMail, jmapSubmission} {
		if _, exists := session.Capabilities[capability]; !exists {
			return errAccountMail
		}
	}

	accountID := cfg.JMAPAccountID
	if accountID == "" {
		accountID = session.PrimaryAccounts[jmapSubmission]
	}

	account, exists := session.Accounts[accountID]
	if !exists || accountID == "" || account.IsReadOnly {
		return errAccountMail
	}

	for _, capability := range []string{jmapMail, jmapSubmission} {
		if _, exists := account.AccountCapabilities[capability]; !exists {
			return errAccountMail
		}
	}

	c.endpoint, c.account = api.String(), accountID

	return nil
}
