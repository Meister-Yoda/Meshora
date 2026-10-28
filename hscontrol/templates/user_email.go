package templates

import (
	"html"
	"time"

	"github.com/chasefleming/elem-go"
	"github.com/chasefleming/elem-go/attrs"
)

type UserEmailState struct {
	Allowed      bool
	Managed      bool
	Available    bool
	NeedPassword bool
	NeedCode     bool
	Pending      string
	ExpiresAt    time.Time
}

func userEmailPanel(info AdminPageInfo, user AdminUserRow, email UserEmailState) elem.Node {
	hint := "Diese Adresse wird für deine Anmeldung verwendet. Eine neue Adresse gilt erst nach Bestätigung."

	var content []elem.Node

	switch {
	case email.Managed:
		content = append(content, elem.P(nil, i18nText("Deine E-Mail-Adresse wird vom Anmeldeanbieter verwaltet.")))
	case !email.Allowed:
		content = append(content, elem.P(nil, i18nText("Dein Administrator hat eigene E-Mail-Änderungen gesperrt.")))
	case !email.Available:
		content = append(content, elem.P(nil, i18nText("Der Administrator muss zuerst den Mailversand einrichten.")))
	default:
		fields := []elem.Node{elem.Div(attrs.Props{attrs.Class: "field"},
			elem.Label(attrs.Props{attrs.For: "account-email"}, i18nText("Neue E-Mail-Adresse")),
			elem.Input(attrs.Props{attrs.ID: "account-email", attrs.Name: "email", attrs.Type: "email", attrs.Required: "true", attrs.Autocomplete: "email", "maxlength": "254", attrs.Value: html.EscapeString(user.Email)}))}
		if email.NeedPassword {
			fields = append(fields, securityProof(email.NeedCode, "account-email")...)
		}

		fields = append(fields, submitButton("Bestätigungslink senden", "btn-primary"))
		content = append(content, accountPostForm(info, "/account/email", fields...))
	}

	if email.Pending != "" {
		content = append(content, elem.P(attrs.Props{attrs.Role: statusClass}, elem.Text("Bestätigung ausstehend: "+email.Pending+" - gültig bis "+email.ExpiresAt.UTC().Format("15:04 UTC"))))
	}

	return cardSection("E-Mail-Adresse", hint, content...)
}

func UserEmailPage(info AdminPageInfo, user AdminUserRow, email UserEmailState) *elem.Element {
	return personalPage(info, "E-Mail-Adresse",
		pageHead("E-Mail-Adresse", "Verwalte deine bestätigte Anmeldeadresse."),
		userEmailPanel(info, user, email))
}

func UserEmailConfirmation(info AdminPageInfo) *elem.Element {
	return personalPage(info, "E-Mail bestätigen",
		pageHead("Neue E-Mail-Adresse bestätigen", "Nach der Bestätigung enden alle bisherigen Sitzungen. Melde dich anschließend mit der neuen Adresse an."),
		cardSection("Bestätigung", "",
			accountPostForm(info, "/account/email/confirm",
				elem.Div(attrs.Props{"data-email-confirm": "true"},
					elem.Input(attrs.Props{attrs.Type: inputTypeHidden, attrs.Name: "token", attrs.ID: "email-confirm-token"}),
					elem.Button(attrs.Props{attrs.Type: "submit", attrs.Class: "btn btn-primary", attrs.ID: "email-confirm-submit", attrs.Disabled: "true"}, i18nText("E-Mail-Adresse bestätigen")),
					elem.P(attrs.Props{attrs.ID: "email-confirm-status", attrs.Role: statusClass}, i18nText("Bestätigungslink aus der E-Mail erforderlich."))))),
		elem.Script(attrs.Props{attrs.Src: info.RootPath("/assets/security.js"), "defer": "true"}))
}
