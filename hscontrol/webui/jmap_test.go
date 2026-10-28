package webui

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/require"
)

func TestJMAPDelivery(t *testing.T) {
	for _, scenario := range []string{"success", "basic", "identity", "capability", "readonly", "drafts", "create", "submission", "cross-origin", "redirect", "untrusted", "cancelled", "oversized"} {
		t.Run(scenario, func(t *testing.T) {
			var server *httptest.Server

			f := &jmapFixture{scenario: scenario}
			server = httptest.NewTLSServer(http.HandlerFunc(f.serveHTTP))

			f.server = server
			defer server.Close()

			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())

			cfg := types.MailConfig{Transport: "jmap", From: "meshora@example.test", JMAPSessionURL: server.URL + "/session", JMAPToken: "test-token"}
			if scenario == "basic" {
				cfg.JMAPUsername = "mailer"
			}

			if scenario == "untrusted" {
				roots = nil
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			if scenario == "cancelled" {
				cancel()
			}

			err := deliverJMAPMail(ctx, cfg, "user@example.test", "confirmation test", roots)
			if scenario == "success" || scenario == "basic" {
				require.NoError(t, err)
				require.True(t, f.authenticated)
				require.True(t, f.submitted)
				require.True(t, f.cleaned)
			} else {
				require.ErrorIs(t, err, errAccountMail)
				require.False(t, f.submitted)

				if scenario == "submission" {
					require.True(t, f.cleaned)
				}
			}
		})
	}
}

func TestJMAPConfigurationRejectsUnsafeURLsAndSecrets(t *testing.T) {
	cfg := types.MailConfig{Transport: "jmap", From: "meshora@example.test", JMAPSessionURL: "https://mail.example.test/session", JMAPToken: "token"}
	require.True(t, validMailConfig(cfg))

	for _, raw := range []string{"http://mail.example.test/session", "https://user:pass@mail.example.test/session", "https://mail.example.test/session?token=secret", "https://mail.example.test/session#fragment"} {
		bad := cfg
		bad.JMAPSessionURL = raw
		require.False(t, validMailConfig(bad))
	}

	cfg.JMAPTokenPath = "/secret"
	require.False(t, validMailConfig(cfg))
	cfg.JMAPTokenPath = ""
	cfg.JMAPToken = "injected\r\nvalue"
	require.False(t, validMailConfig(cfg))
	cfg.JMAPToken = "token"
	cfg.Transport = "unknown"
	require.False(t, validMailConfig(cfg))
	require.ErrorIs(t, sendAccountMail(context.Background(), cfg, "user@example.test", "body"), errAccountMail)
}

type jmapFixture struct {
	scenario                          string
	server                            *httptest.Server
	authenticated, submitted, cleaned bool
}

func (f *jmapFixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if f.scenario == "basic" {
		user, secret, ok := r.BasicAuth()
		f.authenticated = ok && user == "mailer" && secret == "test-token"
	} else {
		f.authenticated = r.Header.Get("Authorization") == "Bearer test-token"
	}

	if !f.authenticated {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if r.URL.Path == "/session" {
		if f.scenario == "redirect" {
			http.Redirect(w, r, f.server.URL+"/api", http.StatusTemporaryRedirect)
			return
		}

		if f.scenario == "oversized" {
			_, _ = w.Write([]byte(strings.Repeat(" ", (1<<20)+1)))
			return
		}

		caps := map[string]any{jmapCore: map[string]any{}, jmapMail: map[string]any{}, jmapSubmission: map[string]any{}}
		if f.scenario == "capability" {
			delete(caps, jmapSubmission)
		}

		api := f.server.URL + "/api"
		if f.scenario == "cross-origin" {
			api = "https://other.invalid/api"
		}

		writeJMAPFixture(w, map[string]any{"apiUrl": api, "capabilities": caps, "primaryAccounts": map[string]string{jmapSubmission: "account"}, "accounts": map[string]any{"account": map[string]any{"isReadOnly": f.scenario == "readonly", "accountCapabilities": caps}}})

		return
	}

	var request struct{ MethodCalls [][]json.RawMessage }
	if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.MethodCalls) != 1 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var (
		method, callID string
		args           map[string]json.RawMessage
	)

	_ = json.Unmarshal(request.MethodCalls[0][0], &method)
	_ = json.Unmarshal(request.MethodCalls[0][1], &args)
	_ = json.Unmarshal(request.MethodCalls[0][2], &callID)

	var account string

	_ = json.Unmarshal(args["accountId"], &account)
	if account != "account" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	result := map[string]any{"accountId": account}

	switch method {
	case "Identity/get":
		email := "meshora@example.test"
		if f.scenario == "identity" {
			email = "someone-else@example.test"
		}

		result["list"] = []any{map[string]string{"id": "identity", "email": email}}
	case "Mailbox/get":
		result["list"] = []any{map[string]any{"id": "drafts", "role": "drafts", "myRights": map[string]bool{"mayAddItems": f.scenario != "drafts"}}}
	case "Email/set":
		if _, ok := args["destroy"]; ok {
			f.cleaned = true
			result["destroyed"] = []string{"email"}

			break
		}

		var creates map[string]struct {
			From       []struct{ Email string }
			To         []struct{ Email string }
			BodyValues map[string]struct{ Value string }
		}

		_ = json.Unmarshal(args["create"], &creates)

		message := creates["message"]
		if len(message.From) != 1 || message.From[0].Email != "meshora@example.test" || len(message.To) != 1 || message.To[0].Email != "user@example.test" || message.BodyValues["body"].Value != "confirmation test" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if f.scenario == "create" {
			result["notCreated"] = map[string]any{"message": map[string]string{"type": "forbidden"}}
		} else {
			result["created"] = map[string]any{"message": map[string]string{"id": "email"}}
		}
	case "EmailSubmission/set":
		var creates map[string]struct {
			IdentityID string
			EmailID    string
		}

		_ = json.Unmarshal(args["create"], &creates)

		var destroy []string

		_ = json.Unmarshal(args["onSuccessDestroyEmail"], &destroy)
		if creates["submission"].IdentityID != "identity" || creates["submission"].EmailID != "email" || len(destroy) != 1 || destroy[0] != "#submission" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if f.scenario == "submission" {
			method = "error"
			result = map[string]any{"type": "forbidden"}
		} else {
			f.submitted = true
			result["created"] = map[string]any{"submission": map[string]string{"id": "sent"}}
		}
	default:
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	writeJMAPFixture(w, map[string]any{"methodResponses": []any{[]any{method, result, callID}}})
}

func writeJMAPFixture(w http.ResponseWriter, value any) {
	err := json.NewEncoder(w).Encode(value)
	if err != nil {
		panic(err)
	}
}
