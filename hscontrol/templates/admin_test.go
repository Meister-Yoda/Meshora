package templates

import (
	"testing"

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

func TestAdminPagesHaveNoInlineStyles(t *testing.T) {
	pages := []string{
		AdminLogin(AdminLoginInfo{Base: "/admin", CSRFToken: "t", Error: "nope"}).Render(),
		AdminOverview(AdminOverviewInfo{Base: "/admin", Username: "alice", NodeCount: 3, OnlineCount: 2}).Render(),
	}

	for _, page := range pages {
		assert.NotContains(t, page, "style=", "the admin CSP forbids inline styles")
		assert.NotContains(t, page, "<style")
	}
}
