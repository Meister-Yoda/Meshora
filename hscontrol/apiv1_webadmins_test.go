package hscontrol

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIV1WebAdmins(t *testing.T) {
	h := newAPIV1Harness(t)

	res := h.callHuma(http.MethodGet, "/api/v1/webadmin", nil)
	require.Equal(t, http.StatusOK, res.status)
	assert.JSONEq(t, `{"webAdmins":[]}`, string(res.body))

	res = h.callHuma(http.MethodPost, "/api/v1/webadmin",
		[]byte(`{"username":"alice","password":"correct horse battery"}`))
	require.Equal(t, http.StatusOK, res.status, string(res.body))
	assert.NotContains(t, string(res.body), "correct horse battery")
	assert.NotContains(t, string(res.body), "argon2id")

	var created struct {
		WebAdmin struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"webAdmin"`
	}
	require.NoError(t, json.Unmarshal(res.body, &created))
	assert.Equal(t, "alice", created.WebAdmin.Username)
	assert.NotEmpty(t, created.WebAdmin.ID)

	for _, tc := range []struct {
		name, method, path, body string
		want                     int
	}{
		{"duplicate", http.MethodPost, "/api/v1/webadmin", `{"username":"alice","password":"correct horse battery"}`, http.StatusConflict},
		{"short password", http.MethodPost, "/api/v1/webadmin", `{"username":"bob","password":"short"}`, http.StatusBadRequest},
		{"invalid username", http.MethodPost, "/api/v1/webadmin", `{"username":"has space","password":"correct horse battery"}`, http.StatusBadRequest},
		{"set password", http.MethodPost, "/api/v1/webadmin/alice/password", `{"password":"another long password"}`, http.StatusOK},
		{"set password short", http.MethodPost, "/api/v1/webadmin/alice/password", `{"password":"short"}`, http.StatusBadRequest},
		{"set password unknown", http.MethodPost, "/api/v1/webadmin/nobody/password", `{"password":"another long password"}`, http.StatusNotFound},
		{"delete", http.MethodDelete, "/api/v1/webadmin/alice", "", http.StatusOK},
		{"delete again", http.MethodDelete, "/api/v1/webadmin/alice", "", http.StatusNotFound},
	} {
		var body []byte
		if tc.body != "" {
			body = []byte(tc.body)
		}

		res := h.callHuma(tc.method, tc.path, body)
		assert.Equal(t, tc.want, res.status, "%s: %s", tc.name, res.body)
	}
}
