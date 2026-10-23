package templates

import (
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/assets"
	"github.com/stretchr/testify/assert"
)

func TestMeterWidthClass(t *testing.T) {
	for pct, want := range map[int]string{
		-5: "w-0", 0: "w-0", 4: "w-0", 5: "w-10", 33: "w-30", 95: "w-100", 100: "w-100", 150: "w-100",
	} {
		got := meterWidthClass(pct)
		assert.Equal(t, want, got, "pct=%d", pct)
		assert.Contains(t, string(assets.AdminCSS), "."+got+" {", "class %s missing from admin.css", got)
	}
}

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	for d, want := range map[time.Duration]string{
		0:                 "just now",
		-30 * time.Second: "just now",
		-5 * time.Minute:  "5 min ago",
		-time.Hour:        "1 hour ago",
		-3 * time.Hour:    "3 hours ago",
		-50 * time.Hour:   "2 days ago",
		72 * time.Hour:    "in 3 days",
		90 * time.Minute:  "in 1 hour",
	} {
		assert.Equal(t, want, relativeTime(now.Add(d), now), "d=%s", d)
	}
}

func TestAdminPagesHaveNoInlineStyles(t *testing.T) {
	info := AdminPageInfo{Base: "/admin", Username: "alice", CSRFToken: "t"}
	online, offline := true, false
	now := time.Now()

	pages := []string{
		AdminLogin(AdminLoginInfo{Base: "/admin", CSRFToken: "t", Error: "nope"}).Render(),
		AdminOverview(AdminOverviewInfo{Base: "/admin", Username: "alice", NodeCount: 3, OnlineCount: 2}).Render(),
		AdminUsers(info, nil).Render(),
		AdminUsers(info, []AdminUserRow{
			{Name: "bob", DisplayName: "Bob", Email: "bob@example.com", Provider: "OIDC", NodeCount: 2, CreatedAt: now},
		}).Render(),
		AdminNodes(info, nil).Render(),
		AdminNodes(info, []AdminNodeRow{
			{GivenName: "laptop", Hostname: "Laptop", Owner: "bob", IPv4: "100.64.0.1", Online: &online, LastSeen: now},
			{Hostname: "server", Tags: []string{"tag:server"}, IPv6: "fd7a::1", Online: &offline, Expired: true},
			{Hostname: "unknown", Expiry: now.Add(time.Hour)},
		}).Render(),
		AdminPending(info, nil).Render(),
		AdminPending(info, []AdminPendingRow{
			{Hostname: "laptop", OS: "linux", MachineKey: "mkey:abcd1234…", CreatedAt: now},
			{SSHCheck: true, Hostname: "a → b", CreatedAt: now},
		}).Render(),
		AdminUsers(AdminPageInfo{Base: "/admin", Flash: &AdminFlash{Error: true, Message: "nope"}}, nil).Render(),
		AdminUserDetail(info, AdminUserRow{ID: 1, Name: "bob", Provider: "CLI"}, nil).Render(),
		AdminUserDetail(info, AdminUserRow{ID: 1, Name: "bob", Provider: "OIDC"}, []AdminNodeRow{
			{ID: 2, GivenName: "laptop", Owner: "bob"},
		}).Render(),
		AdminNodeDetail(info, AdminNodeRow{ID: 2, GivenName: "laptop", Online: &online}, AdminNodeExtra{}).Render(),
		AdminNodeDetail(info, AdminNodeRow{ID: 3, Tags: []string{"tag:a"}}, AdminNodeExtra{CreatedAt: now}).Render(),
		AdminPendingReview(info, AdminPendingRow{AuthID: "x", Hostname: "pc"}, nil).Render(),
		AdminPendingReview(info, AdminPendingRow{AuthID: "x", Hostname: "pc"},
			[]AdminChoice{{Value: "1", Label: "bob"}}).Render(),
		AdminPendingReview(info, AdminPendingRow{AuthID: "x", SSHCheck: true}, nil).Render(),
		AdminKeys(info, nil, nil, []AdminChoice{{Value: "1h", Label: "1 hour"}}).Render(),
		//nolint:gosec // G101: test fixture, not a credential
		AdminKeys(AdminPageInfo{Base: "/admin", Flash: &AdminFlash{Message: "new", Secret: "hskey-auth-x"}},
			[]AdminKeyRow{
				{ID: 1, Prefix: "hskey-auth-a-***", User: "bob", Reusable: true, Status: "Active"},
				{ID: 2, Prefix: "hskey-auth-b-***", Tags: []string{"tag:ci"}, Ephemeral: true, Status: "Expired"},
			}, nil, nil).Render(),
		AdminConfirm(info, AdminConfirmInfo{Section: "Nodes", Title: "Delete", Action: "/nodes/1/delete"}).Render(),
	}

	for _, page := range pages {
		assert.NotContains(t, page, "style=", "the admin CSP forbids inline styles")
		assert.NotContains(t, page, "<style")
	}
}

func TestLocalizedDocumentsPreserveRecordsAndSecrets(t *testing.T) {
	info := AdminPageInfo{Base: "/admin", Username: "Settings", CSRFToken: "secret-token"}
	page := Localize(AdminUsers(info, []AdminUserRow{{Name: "Overview", DisplayName: "Settings", Email: "Users"}}).Render(), "de")
	assert.Contains(t, page, ">Benutzer<")
	assert.Contains(t, page, ">Settings<")
	assert.Contains(t, page, ">Users<")
	assert.Contains(t, page, `value="secret-token"`)
	assert.NotContains(t, page, "<!--meshora-i18n-->")

	login := Localize(AdminLogin(AdminLoginInfo{Base: "/admin", OIDCEnabled: true}).Render(), "de")
	assert.Contains(t, login, ">Mit Authentik anmelden</button>")

	setup := Localize(InitialSetup(SetupInfo{ServerURL: "http://localhost:8080"}).Render(), "en")
	assert.Contains(t, setup, ">Welcome to Meshora<")
	assert.Contains(t, setup, ">Complete setup<")
	assert.NotContains(t, setup, "<!--meshora-i18n-->")
	// The user's raw text must stay escaped even when it resembles a localization marker.
	hostile := Localize(AdminLogin(AdminLoginInfo{Base: "/admin", OIDCEnabled: true, OIDCButtonText: `<script>alert(1)</script><!--meshora-i18n-->Settings`}).Render(), "de")
	assert.NotContains(t, hostile, "<script>")
	assert.Contains(t, hostile, "&lt;script&gt;")
}
