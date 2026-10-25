package templates

import (
	"html"
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

func TestDeviceSearchAndDetails(t *testing.T) {
	info := AdminPageInfo{Base: "/admin", Username: "alice"}
	nodes := []AdminNodeRow{{ID: 7, GivenName: "<script>", Owner: "a\"<b>", AnnouncedRoutes: []string{"10.0.0.0/24", "0.0.0.0/0"}, ApprovedRoutes: []string{"10.0.0.0/24"}, Expired: true, Expiry: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}}
	body := Localize(AdminFilteredNodes(info, nil, nodes, AdminNodeFilters{Query: "\"<script>"}).Render(), "de")
	assert.Contains(t, body, "Keine passenden Geräte")
	assert.Contains(t, body, "Filter anwenden")
	assert.Contains(t, body, `method="GET"`)
	assert.Contains(t, body, `for="f-status"`)
	assert.NotContains(t, body, `<script>`)
	assert.Contains(t, body, "&lt;script&gt;")

	detail := Localize(AdminNodeDetail(info, nodes[0], AdminNodeExtra{}).Render(), "de")
	assert.Contains(t, detail, "Angekündigte Routen")
	assert.Contains(t, detail, "Genehmigte Routen")
	assert.Contains(t, detail, "0.0.0.0/0")
	assert.Contains(t, detail, "2026-01-02 03:04:05 UTC")
	assert.NotContains(t, detail, "<script>")
}

func TestDeviceDiagnostics(t *testing.T) {
	online, offline := true, false
	for _, tc := range []struct {
		name         string
		node         AdminNodeRow
		want, absent string
	}{
		{"unknown", AdminNodeRow{}, "Connection state unknown.", "Offline:"},
		{"offline", AdminNodeRow{Online: &offline}, "The cause is unknown.", "Device authorization expired."},
		{"online expired", AdminNodeRow{Online: &online, Expired: true}, "Device authorization expired.", "Offline:"},
		{"offline approved", AdminNodeRow{Online: &offline, AnnouncedRoutes: []string{"10.0.0.0/24"}, ApprovedRoutes: []string{"10.0.0.0/24"}}, "Approved routes do not guarantee reachability.", "Routes awaiting approval"},
		{"approved no longer advertised", AdminNodeRow{ApprovedRoutes: []string{"10.0.0.0/24"}}, "Approved routes do not guarantee reachability.", "Routes awaiting approval"},
		{"unapproved", AdminNodeRow{AnnouncedRoutes: []string{"0.0.0.0/0"}}, "Routes awaiting approval", "Device authorization expired."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := Localize(AdminNodeDetail(AdminPageInfo{Base: "/admin"}, tc.node, AdminNodeExtra{}).Render(), "en")
			assert.Contains(t, body, tc.want)
			assert.NotContains(t, body, tc.absent)
			assert.NotContains(t, body, "style=")
		})
	}

	both := Localize(AdminNodeDetail(AdminPageInfo{Base: "/admin"}, AdminNodeRow{Online: &online, Expired: true}, AdminNodeExtra{}).Render(), "en")
	assert.Contains(t, both, "Online: an active connection")
	assert.Contains(t, both, "Connection state and expiry are separate")
}

func TestRouteDiagnosticsOnlyListUnapprovedAdvertisements(t *testing.T) {
	body := Localize(nodeDiagnostics(AdminNodeRow{
		AnnouncedRoutes: []string{"10.0.0.0/24", "0.0.0.0/0", "::/0", "<script>alert(1)</script>"},
		ApprovedRoutes:  []string{"10.0.0.0/24", "192.168.0.0/24"},
	}).Render(), "en")
	assert.Contains(t, body, "0.0.0.0/0")
	assert.Contains(t, body, "::/0")
	assert.NotContains(t, body, "10.0.0.0/24")
	assert.NotContains(t, body, "192.168.0.0/24")
	assert.Contains(t, body, "&lt;script&gt;")
	assert.NotContains(t, body, "<script>")
	assert.Contains(t, body, "The reason is unknown.")
}

func TestPendingRegistrationGuidance(t *testing.T) {
	info := AdminPageInfo{Base: "/admin"}
	for _, lang := range []string{"en", "de"} {
		registration := Localize(AdminPendingReview(info, AdminPendingRow{AuthID: "request"}, nil).Render(), lang)
		ssh := Localize(AdminPendingReview(info, AdminPendingRow{AuthID: "request", SSHCheck: true}, nil).Render(), lang)

		if lang == "de" {
			assert.Contains(t, registration, "Registrierung wartet")
			assert.Contains(t, registration, "starte die Anmeldung auf dem Gerät erneut")
			assert.Contains(t, registration, "Lege zuerst einen unter Benutzer an.")
			assert.NotContains(t, ssh, "Registrierung wartet")
		} else {
			assert.Contains(t, registration, "Registration pending")
			assert.Contains(t, registration, "Reject unexpected requests.")
			assert.NotContains(t, ssh, "Registration pending")
			assert.Contains(t, ssh, "Approving lets this SSH connection through once.")
		}
	}
}

func TestDiagnosticTranslations(t *testing.T) {
	messages := []string{
		"Status and next steps",
		"Connection state unknown. Check the Tailscale client on the device; no cause can be determined from the available data.",
		"Offline: no active connection to the control server is recorded. The cause is unknown. Check whether the device is running, has network access and its Tailscale client is connected.",
		"Online: an active connection to the control server is recorded. This does not prove that services or routes are reachable.",
		"Device authorization expired. Sign in again through the Tailscale client on this device. Connection state and expiry are separate; the data does not explain why authorization expired.",
		"Routes awaiting approval",
		"These advertised routes are not approved for this device. The reason is unknown. An administrator should verify the intended networks and approve them through route management (CLI or API), or check the policy's autoApprovers. This page does not change route approvals.",
		"Approved routes do not guarantee reachability. If a connection fails, check the router's connection state, route advertisement, IP forwarding, access policy and the client's route settings.",
		"Registration pending",
		"A registration request is open. The available data does not show why it has not finished. Verify the device and intended owner before assigning it, or ask the user to complete sign-in. Reject unexpected requests. If the request expires, start sign-in again on the device.",
		"Understanding device states",
		"Offline and expired describe different states. Offline does not reveal a cause; check the device, network and Tailscale client. Expired authorization requires signing in again on the device. Open a device for recorded times, route approvals and next steps.",
		"Review pending requests",
		"An open request is not proof of an error. Review its type, device and intended user. Device registration assigns an owner; an SSH check approves one connection. The reason for waiting is unknown. If a request expires, start sign-in or the SSH connection again on the device.",
	}
	for _, message := range messages {
		german := Localize(i18nText(message).Render(), "de")
		assert.NotEqual(t, message, german, "missing German translation: %s", message)
		assert.NotContains(t, german, "<!--meshora-i18n-->")
		assert.Equal(t, html.EscapeString(message), Localize(i18nText(message).Render(), "en"))
	}
}

func TestPublicEnrollment(t *testing.T) {
	info := AdminPageInfo{Base: "/mesh/admin"}

	for _, platform := range []string{"", "windows", "macos", "linux", "android", "ios"} {
		for _, oidc := range []bool{true, false} {
			body := Localize(AdminEnrollment(info, "https://mesh.example/mesh", platform, oidc).Render(), "en")
			assert.Contains(t, body, `action="/mesh/connect"`)
			assert.NotContains(t, body, "Sign out")
			assert.NotContains(t, body, "csrf_token")
			assert.NotContains(t, body, "style=")

			if platform == "" {
				assert.NotContains(t, body, "enrollment-value")
				continue
			}

			assert.Contains(t, body, "https://mesh.example/mesh")
			assert.Contains(t, body, `src="/mesh/admin/assets/enrollment.js"`)

			if platform == "android" || platform == "ios" {
				assert.NotContains(t, body, "tailscale login")
			} else {
				assert.Contains(t, body, "tailscale login --login-server=")
			}

			if oidc {
				assert.Contains(t, body, "confirm registration")
				assert.NotContains(t, body, "must review the request")
			} else {
				assert.Contains(t, body, "must review the request")
			}

			assert.Contains(t, Localize(AdminEnrollment(info, "https://mesh.example/mesh", platform, oidc).Render(), "de"), "Registrierung abschließen")
		}
	}

	body := AdminEnrollment(info, `https://mesh.example/'"><script>alert(1)</script>`, "linux", true).Render()
	assert.NotContains(t, body, "<script>alert(1)</script>")
}
