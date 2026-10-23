package templates

import (
	"github.com/chasefleming/elem-go"
)

func Windows(url string) *elem.Element {
	return page(
		"headscale - Windows",
		H1(i18nText("Windows configuration")),
		P(
			i18nText("Download "),
			externalLink("https://tailscale.com/download/windows", "Tailscale for Windows"),
			i18nText(" and install it."),
		),
		P(
			i18nText("Open a Command Prompt or PowerShell and use Tailscale's login command to connect with headscale:"),
		),
		codeBlockText("tailscale login --login-server "+url),
	)
}
