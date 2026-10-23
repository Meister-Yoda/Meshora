# Meshora

[English](README.md) · **Deutsch**

Meshora ist ein selbst gehosteter Tailscale-Control-Server auf Basis von
[Headscale](https://github.com/juanfont/headscale), mit einem Web-Dashboard
für Benutzer, Geräte, Registrierungen und Vorab-Authentifizierungsschlüssel.

> Meshora ist ein unabhängiger Fork und gehört weder zu Tailscale Inc. noch
> zum Headscale-Projekt. Bitte melde Meshora-Probleme in diesem Repository.

## v0.1.0-rc.6

- Seitennavigation mit einklappbarem Menü auf kleinen Bildschirmen.
- Getrennte Einstellungsseiten für Identitätsanbieter, Admin-Zugriff und Darstellung.
- OIDC-Rückleitungs-URLs direkt bei den Einstellungen des Identitätsanbieters.
- Kompakter Deutsch/English-Umschalter mit Flaggen-Dropdown und gespeicherter Wahl.
- Ausgewählte Headscale-Korrekturen für Datenbankverbindungen, Policy-Updates,
  Client-Verbindungen und Shutdown; Go-Builds mit Version 1.27.3.
- Authentik-Admin-Anmeldung, Rollen-/Gruppenbindung und lokaler Wiederherstellungszugang.

## Mit Docker starten

Verwende die [Compose-Datei](packaging/docker/compose.yaml) mit Docker Compose v2:

```bash
docker compose up -d
```

Öffne `http://localhost:8080/setup` beziehungsweise die Adresse deines Servers,
trage die öffentliche Server-URL ein und lege deinen ersten lokalen Admin an.
Passwörter benötigen mindestens 12 Zeichen. OIDC ist optional und kann später
unter **Einstellungen** eingerichtet werden. Der Assistent speichert die
Konfiguration und aktiviert den Server automatisch. Bestehende Installationen
behalten ihre Daten und überspringen die Einrichtung.

Das Release-Image lautet `ghcr.io/meister-yoda/meshora:v0.1.0-rc.6`, für Linux
AMD64 und ARM64. Volumes, Reverse Proxies und Updates beschreibt die
[Docker-Anleitung](packaging/docker/README.md). Ein Reverse Proxy muss den
**Container-Port 8080** ansprechen.

## Dashboard

Unter `/admin/` siehst du Benutzer, registrierte und verbundene Geräte sowie
wartende Anfragen. Du kannst Benutzer anlegen, umbenennen und löschen;
Geräte umbenennen, taggen, abmelden und löschen; Registrierungen zuweisen oder
ablehnen; SSH-Prüfungen freigeben oder ablehnen sowie Vorab-Schlüssel anlegen
und deaktivieren. Neue Schlüssel werden nur einmal vollständig angezeigt.

Wähle oben **Deutsch** oder **English**. Die Wahl wird in einem Cookie für
Login, Einrichtung, Dashboard und öffentliche Seiten gespeichert. Ohne eigene
Wahl gilt die Browsersprache; Englisch ist der Rückfall. Eigene Namen,
Schlüssel, Befehle und selbst gewählte Button-Texte werden nicht übersetzt.

## Admin-Anmeldung mit Authentik

Trage unter **Einstellungen** Issuer-URL, Client-ID und Client-Secret ein und
lege **Admin-Rollen** und/oder **Authentik-Gruppen mit Admin-Zugriff** fest.
Änderungen am Provider und an den Zugriffsbeschränkungen benötigen einen
Neustart. Der OIDC-Button-Text hat ein eigenes Formular und gilt sofort.
Ein leeres Feld verwendet den Standardtext der gewählten Oberflächensprache.

Beispielkonfiguration; lokale Konten bleiben verfügbar:

```yaml
server_url: https://vpn.example.com
web_admin:
  enabled: true
  session_ttl: 12h
  oidc:
    allowed_groups: []
    allowed_roles: [meshora-admin]
    roles_claim: roles
    button_text: ""  # übersetzter Standard

oidc:
  issuer: https://auth.example.com/application/o/meshora/
  client_id: meshora
  client_secret_path: /run/secrets/meshora-oidc
  scope: [openid, profile, email, meshora_roles]
```

Verwende in Authentik einen OAuth2/OIDC-Provider mit Signaturschlüssel und
aktiviere **Include claims in id_token**. Trage diese beiden URLs mit
**Strict-Abgleich** ein:

- `https://vpn.example.com/admin/oidc/callback` für Dashboard-Admins.
- `https://vpn.example.com/oidc/callback` für Geräteregistrierungen.

Erstelle ein OAuth2-Scope-Mapping `meshora_roles`, weise es dem Provider zu
und fordere denselben Scope unter `oidc.scope` an. Für aktuelle Authentik-Versionen
exportiert dieses Mapping die direkt und indirekt zugewiesenen Rollen:

```python
return {"roles": [role.name for role in request.user.all_roles()]}
```

Weise die Authentik-Rolle `meshora-admin` nur den vorgesehenen Administratoren
zu. Ein vorhandenes Mapping kann einen anderen obersten Claim liefern;
trage dessen Namen im Feld **Rollen-Claim** ein. Der Claim muss eine Liste von
Rollennamen enthalten; Groß- und Kleinschreibung sind relevant. Siehe die
[Authentik-Dokumentation zu Property Mappings](https://docs.goauthentik.io/add-secure-apps/providers/property-mappings/).

Ohne Gruppenfilter genügt eine erlaubte Rolle. Ohne Rollenfilter genügt eine
erlaubte Gruppe. **Sind beide Listen konfiguriert, müssen beide Bedingungen
erfüllt sein**; innerhalb einer Liste reicht jeweils ein Eintrag. Sind beide
Listen leer, ist Admin-OIDC deaktiviert. Die Gerätefilter `oidc.allowed_groups`,
`allowed_users` und `allowed_domains` vergeben keine Admin-Rechte. Zugelassene
SSO-Admins erhalten vollen Dashboard-Zugriff; einzelne Aktionen haben keine
zusätzlichen Rollenstufen.

### Automatische Anmeldung und Wiederherstellung

Bei eingerichtetem Admin-OIDC versucht `/admin/` einmalig eine Anmeldung mit
`prompt=none`. Besteht bereits eine passende Authentik-Sitzung, gelingt dies
ohne Benutzerinteraktion. Fehlen Anmeldung, Zustimmung oder Berechtigung,
erscheint die normale Login-Seite. Der OIDC-Button startet einen interaktiven
Versuch. Auch Discovery- und Tokenfehler führen zurück zum lokalen Formular.

Mit `/admin/login?local=1` umgehst du die automatische Anmeldung. Abmelden führt
ebenfalls auf diese Seite, statt sofort erneut anzumelden. Behalte ein lokales
Admin-Konto zur Wiederherstellung. Falls der Provider seinen Callback nicht
aufruft, öffne diese URL direkt.

SSO-Sessions liegen im Arbeitsspeicher und enden spätestens mit Ablauf des
ID-Tokens oder von `session_ttl`. Rollen- und Gruppenänderungen werden bei der
nächsten Anmeldung geprüft. Ein Neustart beendet alle Admin-Sessions.
Meshora-Abmelden beendet nicht die Authentik-Sitzung. Gleiche Benutzernamen
oder E-Mail-Adressen verknüpfen keine lokalen Konten mit SSO-Identitäten.

## Geräte verbinden

```bash
tailscale up --login-server=https://vpn.example.com
```

Mit OIDC melden sich Benutzer beim Identitätsanbieter an und bestätigen das
Gerät. Ohne OIDC weist ein Admin die Registrierung unter `/admin/pending` zu
oder verwendet `headscale auth register --auth-id … --user …`.

## Lokale Admin-Konten

```bash
headscale webadmins list
headscale webadmins create <username> [--password-file FILE]
headscale webadmins set-password <username> [--password-file FILE]
headscale webadmins delete <username>
```

Passwörter werden mit Argon2id gehasht. Passwortänderung oder Kontolöschung
beendet die zugehörigen Sessions. Formulare sind mit CSRF-Tokens geschützt,
Anmeldeversuche begrenzt. Das Dashboard verwendet eine strikte CSP ohne Skripte,
Inline-Styles oder externe Assets.

## Entwicklung

Go 1.27.3 oder neuer; `nix develop` liefert die vollständige Toolchain.
Konventionen und Testverfahren stehen in [AGENTS.md](AGENTS.md).
Verwende `make build`, `make test` und `make lint`. Unter Windows in WSL bauen.

Das Dashboard liegt in `hscontrol/webui/`, Vorlagen und Sprachkatalog in
`hscontrol/templates/`, Assets in `hscontrol/assets/`. OIDC-Tests prüfen mit
einem lokalen Provider und signierten Tokens Discovery, PKCE, Gruppen/Rollen,
Callback-Wiederholungen und den Rückfall der automatischen Anmeldung.

## Auf Basis von Headscale

Die Kernfunktionen stammen von Headscale, Juan Font und den Mitwirkenden.
Die [Headscale-Dokumentation](https://headscale.net/stable/) gilt für den
Control-Server. Meshora ergänzt das Dashboard und die hier beschriebenen Funktionen.

BSD-3-Clause-Lizenz; siehe [LICENSE](LICENSE).
