package templates

import (
	"strconv"

	"github.com/chasefleming/elem-go"
	"github.com/chasefleming/elem-go/attrs"
)

const accountStatusLabel = "Status"

func personalPage(info AdminPageInfo, title string, content ...elem.Node) *elem.Element {
	return adminDocument(info.Base, title+" · Meshora", "app", adminTopbar(info.Base, info.Username, info.CSRFToken, false),
		elem.Main(attrs.Props{attrs.Class: "content personal-content", attrs.ID: "main-content"}, append([]elem.Node{flashBanner(info.Flash)}, content...)...),
		elem.Footer(attrs.Props{attrs.Class: "app-footer personal-content"}, poweredBy()...))
}

func UserAccountPage(info AdminPageInfo, user AdminUserRow, nodes []AdminNodeRow, admin bool) *elem.Element {
	rows := make([]elem.Node, 0, len(nodes))
	for _, node := range nodes {
		rows = append(rows, elem.Tr(nil,
			elem.Td(nil, elem.A(attrs.Props{attrs.Href: info.Base + "/account/devices/" + strconv.FormatUint(node.ID, 10)}, elem.Text(node.GivenName))),
			elem.Td(nil, addressCell(node.IPv4, node.IPv6)), elem.Td(nil, statusCell(node.Online))))
	}

	content := []elem.Node{
		pageHead("Mein Konto", "Deine Geräte und Kontoeinstellungen."),
		cardSection("Benutzeridentität", "Deine UID bleibt auch bei Änderungen an E-Mail oder Anmeldung erhalten.", detailList(detail("UID", elem.Text(strconv.FormatUint(uint64(user.ID), 10))), detail("Email", elem.Text(user.Email)))),
		cardSection("Meine Geräte", "Nur deine benutzereigenen Geräte werden angezeigt.", dataTable("Meine Geräte", []string{"Gerät", "Adressen", accountStatusLabel}, rows)),
		cardSection("Profil", "", postForm(info, "/account/profile", "action-form", textField("Display name", "display_name", user.DisplayName, false), submitButton("Profil speichern", "btn-primary"))),
		cardSection("Passwort", "Ein lokales Passwort und OIDC gehören zu derselben UID.", linkButton(info.Base+"/account/password", "Passwort ändern", "btn-secondary")),
	}
	if admin {
		content = append(content, linkButton(info.Base+"/", "Zur Administration", "btn-secondary"))
	}

	return personalPage(info, "Mein Konto", content...)
}

func UserPasswordPage(info AdminPageInfo, required, oidc bool) *elem.Element {
	hint := "Ändere dein lokales Passwort. Danach meldest du dich mit dem neuen Passwort erneut an."
	if required {
		hint = "Du verwendest ein Startpasswort. Lege ein eigenes Passwort fest, bevor du fortfährst."
	}

	fields := []elem.Node{}
	if !oidc {
		fields = append(fields, passwordInput("Aktuelles Passwort", "current_password", "current-password"))
	}

	fields = append(fields, passwordInput("Neues Passwort (mindestens 12 Zeichen)", "password", "new-password"), passwordInput("Neues Passwort wiederholen", "password_confirm", "new-password"), submitButton("Passwort ändern und neu anmelden", "btn-primary"))

	content := []elem.Node{pageHead("Passwort ändern", hint), cardSection("Lokales Passwort", "", postForm(info, "/account/password", "action-form", fields...))}
	if !required {
		content = append(content, linkButton(info.Base+"/account", "Zurück zum Konto", "btn-secondary"))
	}

	return personalPage(info, "Passwort ändern", content...)
}

func passwordInput(label, name, autocomplete string) elem.Node {
	return elem.Div(attrs.Props{attrs.Class: "field"}, elem.Label(attrs.Props{attrs.For: "account-" + name}, elem.Text(label)), elem.Input(attrs.Props{attrs.Type: "password", attrs.ID: "account-" + name, attrs.Name: name, attrs.Autocomplete: autocomplete, attrs.Required: "true"}))
}

func UserDevicePage(info AdminPageInfo, node AdminNodeRow) *elem.Element {
	return personalPage(info, "Mein Gerät", backLink(info.Base+"/account", "Mein Konto"), pageHead(node.GivenName, node.Hostname), cardSection("Gerät", "", detailList(detail("Adressen", addressCell(node.IPv4, node.IPv6)), detail(accountStatusLabel, statusCell(node.Online)), detail("Last seen", relCell(node.LastSeen, info.now())))))
}

func UserMergePage(info AdminPageInfo, sourceID, sourceName string, choices []AdminChoice) *elem.Element {
	return info.shell("Users", backLink(info.Base+"/users/"+sourceID, "Benutzer"), pageHead("Konten zusammenführen", sourceName+" · Quell-UID "+sourceID),
		cardSection("Gemeinsame Identität", "Die Ziel-UID bleibt unverändert. Die Quell-UID wird entfernt. Geräte und Schlüssel werden übertragen, Admin-Rechte bleiben erhalten und alle Sitzungen beider Konten enden. Vorhandene Policy-Verweise müssen vorher übertragen werden.",
			postForm(info, "/users/"+sourceID+"/merge", "action-form", zoneSelect("Zielkonto", "target", "merge-target", choices, nil, false),
				checkboxField("Falls beide Konten ein Passwort besitzen: Zielpasswort behalten und Quellpasswort verwerfen", "keep_password"),
				checkboxField("Diese beiden Konten gehören derselben Person. Zusammenführung bestätigen.", "confirm"), submitButton("Konten endgültig zusammenführen", "btn-danger"))))
}
