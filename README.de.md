# Meshora

[English](README.md) · **Deutsch**

Meshora ist ein selbst gehosteter Tailscale-Control-Server auf Basis von
[Headscale](https://github.com/juanfont/headscale). Meshora übernimmt alles,
was Headscale kann, und ergänzt ein Web-Dashboard. Für die tägliche
Verwaltung braucht es damit keine Kommandozeile mehr:

- **Endnutzer** verbinden den normalen Tailscale-Client und melden sich über
  euren Identity-Provider an (OIDC).
- **Admins** melden sich mit einem lokalen Admin-Account am Web-Dashboard
  unter `/admin` an und verwalten dort Nutzer, Geräte und wartende
  Registrierungen.

> Meshora ist ein unabhängiger Fork und gehört weder zu Tailscale Inc. noch
> zum Headscale-Projekt. Probleme mit Meshora bitte nicht bei Headscale melden.

## Funktionen

Alles, was Headscale bietet (Geräte-Registrierung, IP-Vergabe, ACL-Policy,
MagicDNS, DERP, OIDC, …), dazu:

- **Admin-Dashboard** unter `/admin`
  - Übersicht über Nutzer, Geräte und Online-Geräte
  - Listen der Nutzer, Geräte und wartenden Registrierungen
- **Lokale Admin-Accounts**, getrennt von den Tailnet-Nutzern
  - Passwort-Hashes mit Argon2id
  - Sessions im Arbeitsspeicher. Sie enden bei Passwortänderung, beim Löschen
    des Accounts und beim Neustart.
  - CSRF-Schutz auf jedem Formular und ein Login-Rate-Limit pro IP und
    Benutzername
  - strenge Content Security Policy: keine Skripte, keine Inline-Styles,
    keine Anfragen an Dritte
- **Meshora-Design** für das Dashboard und alle Endnutzer-Seiten
  (Registrierung, Anmeldeergebnis, Client-Einrichtung), mit Hell- und
  Dunkelmodus und Handy-Layout
- **CLI und API** für Admin-Accounts (`headscale webadmins …`,
  `/api/v1/webadmin`)

## Stand

| Phase | Inhalt                                                                                                         | Stand     |
| ----- | -------------------------------------------------------------------------------------------------------------- | --------- |
| 1     | Admin-Accounts, Login, Sessions, Meshora-Design                                                                | fertig    |
| 2     | Dashboard-Listen: Nutzer, Geräte, wartende Registrierungen                                                     | fertig    |
| 3     | Dashboard-Aktionen: Registrierungen zuweisen, SSH-Checks freigeben, Geräte, Nutzer und Pre-Auth-Keys verwalten | in Arbeit |
| 4     | Komfort für Endnutzer: Meshora-Startseite vor OIDC, optional gemerkte Session                                  | geplant   |
| 5     | Dokumentation und ein Docker-Compose-Beispiel (Meshora + Reverse Proxy + Keycloak)                             | geplant   |

## Schnellstart

Meshora wird wie Headscale konfiguriert. Ausgangspunkt ist
[`config-example.yaml`](config-example.yaml), dort das Dashboard aktivieren:

```yaml
server_url: https://vpn.example.com

web_admin:
  enabled: true
  # Wie lange ein Admin angemeldet bleibt. Sessions liegen im Arbeitsspeicher.
  session_ttl: 12h

# Optional, aber so gedacht: Endnutzer melden sich über euren
# Identity-Provider an.
oidc:
  issuer: https://idp.example.com/realms/example
  client_id: meshora
  client_secret_path: /etc/meshora/oidc_client_secret
```

Bauen und starten:

```bash
make build
./headscale serve -c /etc/meshora/config.yaml
```

Den ersten Admin-Account anlegen. Das Passwort wird zweimal abgefragt oder
aus `--password-file` bzw. von stdin gelesen. Als Flag-Wert wird es nie
angenommen.

```bash
./headscale webadmins create admin
```

Danach `https://vpn.example.com/admin` öffnen.

### Ein Gerät verbinden

Auf dem Gerät den Tailscale-Client auf euren Server zeigen lassen:

```bash
tailscale login --login-server https://vpn.example.com
```

Mit OIDC meldet sich der Nutzer beim Identity-Provider an und bestätigt das
Gerät. Ohne OIDC weist ein Admin das Gerät einem Nutzer zu: nach Phase 3 im
Dashboard, bis dahin mit `headscale auth register --auth-id … --user …`.

## Admin-Accounts verwalten

```bash
headscale webadmins list
headscale webadmins create <benutzername> [--password-file DATEI]
headscale webadmins set-password <benutzername> [--password-file DATEI]
headscale webadmins delete <benutzername>
```

Passwörter brauchen mindestens 12 Zeichen. Eine Passwortänderung oder das
Löschen eines Accounts beendet die offenen Sessions dieses Admins.

## Entwicklung

Meshora wird wie Headscale entwickelt. Code-Konventionen, Regeln für
Datenbank-Migrationen und Testabläufe stehen in [`AGENTS.md`](AGENTS.md).

- Go 1.27.2 oder neuer. `nix develop` liefert die komplette Toolchain.
- `make build`, `make test`, `make lint`
- Nach API-Änderungen erzeugt `make client` den Code in `gen/client/` neu.
  Generierten Code separat committen.
- Unter Windows in WSL arbeiten: Eine Test-Fixture aus Headscale hat ein `>`
  im Dateinamen, das ein Windows-Checkout nicht anlegen kann.

Der Dashboard-Code liegt hier:

| Pfad                             | Zweck                                         |
| -------------------------------- | --------------------------------------------- |
| `hscontrol/webui/`               | Dashboard-Handler, Sessions, CSRF, Rate-Limit |
| `hscontrol/templates/admin.go`   | Dashboard-Seiten                              |
| `hscontrol/assets/admin.css`     | Stylesheet des Dashboards                     |
| `hscontrol/assets/style.css`     | Stylesheet der Endnutzer-Seiten               |
| `hscontrol/db/web_admin.go`      | Admin-Accounts und Passwort-Hashing           |
| `hscontrol/api/v1/webadmins.go`  | API für Admin-Accounts                        |
| `cmd/headscale/cli/web_admin.go` | Befehl `headscale webadmins`                  |

## Basiert auf Headscale

Meshora baut auf [Headscale](https://github.com/juanfont/headscale) von
Juan Font und Mitwirkenden auf. Die gesamte Kernfunktionalität stammt aus
Headscale, und die [Dokumentation von Headscale](https://headscale.net/stable/)
gilt für alles außer dem Dashboard. Danke an alle, die an Headscale arbeiten.

## Lizenz

BSD 3-Clause, wie Headscale. Siehe [`LICENSE`](LICENSE).
