package templates

import (
	"github.com/chasefleming/elem-go"
	"github.com/chasefleming/elem-go/attrs"
	"github.com/chasefleming/elem-go/styles"
)

func Apple(url string) *elem.Element {
	return page(
		"headscale - Apple",
		H1(i18nText("iOS configuration")),
		H2(i18nText("GUI")),
		Ol(
			elem.Li(
				nil,
				i18nText("Install the official Tailscale iOS client from the "),
				externalLink("https://apps.apple.com/app/tailscale/id1470499037", "App Store"),
			),
			elem.Li(
				nil,
				i18nText("Open the "),
				elem.Strong(nil, i18nText("Tailscale")),
				i18nText(" app"),
			),
			elem.Li(
				nil,
				i18nText("Click the account icon in the top-right corner and select "),
				elem.Strong(nil, i18nText("Log in…")),
			),
			elem.Li(
				nil,
				i18nText("Tap the top-right options menu button and select "),
				elem.Strong(nil, i18nText("Use custom coordination server")),
			),
			elem.Li(
				nil,
				i18nText("Enter your instance URL: "),
				Code(elem.Text(url)),
			),
			elem.Li(
				nil,
				elem.Text(
					"Enter your credentials and log in. Headscale should now be working on your iOS device",
				),
			),
		),
		H1(i18nText("macOS configuration")),
		H2(i18nText("Command line")),
		P(
			i18nText("Use Tailscale's login command to add your profile:"),
		),
		codeBlockText("tailscale login --login-server "+url),
		H2(i18nText("GUI")),
		Ol(
			elem.Li(
				nil,
				i18nText("Option + Click the "),
				elem.Strong(nil, i18nText("Tailscale")),
				i18nText(" icon in the menu and hover over the "),
				elem.Strong(nil, i18nText("Debug")),
				i18nText(" menu"),
			),
			elem.Li(
				nil,
				i18nText("Under "),
				elem.Strong(nil, i18nText("Custom Login Server")),
				i18nText(", select "),
				elem.Strong(nil, i18nText("Add Account...")),
			),
			elem.Li(
				nil,
				i18nText("Enter "),
				Code(elem.Text(url)),
				i18nText(" of the headscale instance and press "),
				elem.Strong(nil, i18nText("Add Account")),
			),
			elem.Li(
				nil,
				i18nText("Follow the login procedure in the browser"),
			),
		),
		H2(i18nText("Profiles")),
		P(
			elem.Text(
				"Headscale can be set to the default server by installing a Headscale configuration profile:",
			),
		),
		elem.Div(
			attrs.Props{attrs.Style: styles.Props{styles.MarginTop: spaceL, styles.MarginBottom: spaceL}.ToInline()},
			downloadButton("/apple/macos-app-store", "macOS AppStore profile"),
			downloadButton("/apple/macos-standalone", "macOS Standalone profile"),
		),
		Ol(
			elem.Li(
				nil,
				elem.Text(
					"Download the profile, then open it. When it has been opened, there should be a notification that a profile can be installed",
				),
			),
			elem.Li(
				nil,
				i18nText("Open "),
				elem.Strong(nil, i18nText("System Preferences")),
				i18nText(" and go to "),
				elem.Strong(nil, i18nText("Profiles")),
			),
			elem.Li(
				nil,
				i18nText("Find and install the "),
				elem.Strong(nil, i18nText("Headscale")),
				i18nText(" profile"),
			),
			elem.Li(
				nil,
				i18nText("Restart "),
				elem.Strong(nil, i18nText("Tailscale.app")),
				i18nText(" and log in"),
			),
		),
		orDivider(),
		P(
			elem.Text(
				"Use your terminal to configure the default setting for Tailscale by issuing one of the following commands:",
			),
		),
		P(i18nText("For app store client:")),
		codeBlockText("defaults write io.tailscale.ipn.macos ControlURL "+url),
		P(i18nText("For standalone client:")),
		codeBlockText("defaults write io.tailscale.ipn.macsys ControlURL "+url),
		P(
			i18nText("Restart "),
			elem.Strong(nil, i18nText("Tailscale.app")),
			i18nText(" and log in."),
		),
		warningBox("Caution", "You should always download and inspect the profile before installing it."),
		P(i18nText("For app store client:")),
		codeBlockText("curl "+url+"/apple/macos-app-store"),
		P(i18nText("For standalone client:")),
		codeBlockText("curl "+url+"/apple/macos-standalone"),
		H1(i18nText("tvOS configuration")),
		H2(i18nText("GUI")),
		Ol(
			elem.Li(
				nil,
				i18nText("Install the official Tailscale tvOS client from the "),
				externalLink("https://apps.apple.com/app/tailscale/id1470499037", "App Store"),
			),
			elem.Li(
				nil,
				i18nText("Open "),
				elem.Strong(nil, i18nText("Tailscale")),
			),
			elem.Li(
				nil,
				i18nText("Select "),
				elem.Strong(nil, i18nText("Install VPN configuration")),
			),
			elem.Li(
				nil,
				i18nText("Select "),
				elem.Strong(nil, i18nText("Allow")),
				i18nText(", but don't sign in yet"),
			),
			elem.Li(
				nil,
				i18nText("Open "),
				elem.Strong(nil, i18nText("Settings")),
				i18nText(" (the Apple tvOS settings) > "),
				elem.Strong(nil, i18nText("Apps")),
				i18nText(" > "),
				elem.Strong(nil, i18nText("Tailscale")),
			),
			elem.Li(
				nil,
				i18nText("Enter "),
				Code(elem.Text(url)),
				i18nText(" under "),
				elem.Strong(nil, i18nText("ALTERNATE COORDINATION SERVER URL")),
			),
			elem.Li(
				nil,
				i18nText("Return to the tvOS "),
				elem.Strong(nil, i18nText("Home")),
				i18nText(" screen and open "),
				elem.Strong(nil, i18nText("Tailscale")),
			),
			elem.Li(
				nil,
				i18nText("Scan the QR code and follow the login procedure"),
			),
			elem.Li(
				nil,
				i18nText("Headscale should now be working on your tvOS device"),
			),
		),
	)
}
