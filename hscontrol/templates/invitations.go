package templates

import (
	"time"

	"github.com/chasefleming/elem-go"
	"github.com/chasefleming/elem-go/attrs"
	"github.com/juanfont/headscale/hscontrol/types"
)

const (
	invitationPasswordAutocomplete = "new-password"
	invitationCardClass            = "auth-card"
)

func InvitationsPage(info AdminPageInfo, invites []types.UserInvitation, proof *AdminUserSecurity) *elem.Element {
	fields := []elem.Node{textField("E-Mail-Adresse", "email", "", true), zoneSelect("Anmeldeart", "mode", "invite-mode", []AdminChoice{{Value: "local", Label: "Lokales Konto"}, {Value: "oidc", Label: "OIDC / Authentik"}}, []string{"local"}, false)}
	if proof.NeedPassword {
		fields = append(fields, securityProof(proof.NeedCode, "invitation-create")...)
	}

	fields = append(fields, submitButton("Einladung senden", "btn-primary"))
	rows := make([]elem.Node, 0, len(invites))

	for _, invite := range invites {
		status := "Offen"
		if !time.Now().Before(invite.ExpiresAt) {
			status = "Abgelaufen"
		}

		revoke := []elem.Node{elem.Input(attrs.Props{attrs.Type: inputTypeHidden, attrs.Name: "invitation_id", attrs.Value: invite.ID})}
		if proof.NeedPassword {
			revoke = append(revoke, securityProof(proof.NeedCode, "invitation-"+invite.ID)...)
		}

		revoke = append(revoke, submitButton("Widerrufen", "btn-secondary"))
		rows = append(rows, elem.Tr(nil, elem.Td(nil, elem.Text(invite.Email)), elem.Td(nil, elem.Text(invite.Mode)), elem.Td(nil, elem.Text(invite.ExpiresAt.UTC().Format(time.RFC3339))), elem.Td(nil, elem.Text(status)), elem.Td(nil, postForm(info, "/invitations/revoke", "action-form", revoke...))))
	}

	return info.shell("Einladungen", pageHead("Einladungen", "Neue Benutzer gezielt per E-Mail einladen."),
		cardSection("Neue Einladung", "Links sind 24 Stunden gültig und einmalig verwendbar. Erneutes Einladen derselben Adresse ersetzt den bisherigen Link. Neue Konten erhalten keine Adminrechte.", postForm(info, "/invitations", "action-form", fields...)),
		cardSection("Offene Einladungen", "OIDC-Einladungen benötigen eine bestätigte, übereinstimmende E-Mail-Adresse beim Anbieter. Bestehende allgemeine OIDC-Zugangsregeln werden nicht geändert.", dataTable("Einladungen", []string{"E-Mail", "Anmeldeart", "Gültig bis", accountStatusLabel, "Widerrufen"}, rows)))
}

func InvitationPage(base, action, csrf, message string) *elem.Element {
	fields := []elem.Node{brand(), elem.H1(nil, elem.Text("Einladung annehmen")), elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Bei einer lokalen Einladung legst du dein Passwort fest. Bei einer OIDC-Einladung meldest du dich beim Anbieter mit der eingeladenen, bestätigten E-Mail-Adresse an."))}
	if message != "" {
		fields = append(fields, elem.P(attrs.Props{attrs.Role: "alert"}, elem.Text(message)))
	}

	fields = append(fields, elem.Form(attrs.Props{attrs.Method: "POST", attrs.Action: action, attrs.Class: "stack"},
		adminCSRFInput(csrf), elem.Input(attrs.Props{attrs.Type: inputTypeHidden, attrs.Name: "token", attrs.ID: "invitation-token"}),
		elem.Details(attrs.Props{attrs.ID: "invitation-local-fields", "open": "true", attrs.Class: "stack"}, elem.Summary(nil, elem.Text("Passwort für lokales Konto festlegen")),
			elem.P(attrs.Props{attrs.Class: "muted"}, elem.Text("Mindestens 12 Zeichen. Für OIDC nicht erforderlich.")),
			elem.Div(attrs.Props{attrs.Class: "field"}, elem.Label(attrs.Props{attrs.For: "invite-password"}, elem.Text("Passwort")), elem.Input(attrs.Props{attrs.Type: accountPasswordField, attrs.Name: accountPasswordField, attrs.ID: "invite-password", attrs.Autocomplete: invitationPasswordAutocomplete})),
			elem.Div(attrs.Props{attrs.Class: "field"}, elem.Label(attrs.Props{attrs.For: "invite-password-confirm"}, elem.Text("Passwort wiederholen")), elem.Input(attrs.Props{attrs.Type: accountPasswordField, attrs.Name: "password_confirm", attrs.ID: "invite-password-confirm", attrs.Autocomplete: invitationPasswordAutocomplete}))),
		elem.Button(attrs.Props{attrs.Type: submitType, attrs.ID: "invitation-submit", attrs.Disabled: "true", attrs.Class: securityPrimaryButtonClass}, elem.Text("Einladung annehmen"))),
		elem.P(attrs.Props{attrs.ID: "invitation-status", attrs.Role: statusClass}, elem.Text("Einladungslink erforderlich.")),
		elem.Script(attrs.Props{attrs.Src: publicUIPath(base, "/assets/security.js"), "defer": "true"}))

	return adminDocument(base, "Einladung · Meshora", "auth", elem.Main(attrs.Props{attrs.Class: invitationCardClass}, fields...))
}
