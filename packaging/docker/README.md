# Meshora starten und im Browser einrichten

## Meshora v0.2.2 (30. Oktober 2026)

Stabile Veröffentlichung des geprüften Main-Stands einschließlich der
Korrekturen aus dem aktualisierten RC.6. Die Container-Tags `latest` und
`RC` werden auf v0.2.2 aktualisiert.

- OIDC mit vorhandenem Passkey führt direkt zur gebundenen Faktorbestätigung.
- Mailseite: Umschaltung unter der produktiven Sicherheitsrichtlinie repariert
  und Feldabstände korrigiert.

Stabile Version des aktuellen main-Stands. WireGuard-Gateway-Code ist
nicht enthalten; der Prototyp bleibt auf dem separaten lokalen Branch.

- Mail-Einstellungen zeigen nur die gewählte Versandart. Optionale JMAP-IDs
  und technische Details sind einklappbar; Eingaben bleiben beim Wechsel erhalten.
- OIDC-Login mit lokalem Testanbieter erneut geprüft, einschließlich PKCE,
  Replay-Schutz, Tokenprüfung, Rollen/Gruppen und Zwei-Faktor-Vorgaben.
  Redirect-URLs unverändert; echte Authentik-Abnahme steht noch aus.
- Persönliches Menü mit Profil, Geräten, E-Mail-Adresse, Sicherheit und Passwort.
  Admins finden die Kontoseiten im bestehenden Hauptmenü.
- Lokale Nutzer können eine Anmeldeadresse ergänzen oder ändern. Ein
  einmaliger Bestätigungslink wird an die neue Adresse versendet; bis zur
  Bestätigung bleibt die bisherige Anmeldung gültig.
- Admins können E-Mail-Änderungen je Benutzer sperren. Gruppenrechte folgen,
  sobald eine Benutzergruppenverwaltung vorhanden ist.
- Mailversand wahlweise über SMTP (STARTTLS/TLS) oder JMAP (HTTPS), mit
  getrennten Zugangsdaten und Unterstützung für Geheimnisdateien.
- OIDC-verwaltete Adressen bleiben beim Identitätsanbieter. UID und Geräte
  bleiben erhalten; Bestätigung und Freigabeänderungen beenden Sitzungen.
- Enthält die Absicherungen aus RC.4; Tailscale SSH bleibt standardmäßig aus.

Vor dem Update Konfiguration und Datenbank sichern. Zum Update auf diese Version:

```sh
MESHORA_VERSION=v0.2.2 docker compose pull
MESHORA_VERSION=v0.2.2 docker compose up -d
```

v0.2.2 aktualisiert sowohl latest als auch RC. Die neue Datenbankmigration
`202610281800-user-email-changes` wird beim Start angewandt. Vor einem
Downgrade ist die Sicherung wiederherzustellen.

Lokale Paket-/Race-Tests, SQLite-/PostgreSQL-Bestätigung, SMTP-/JMAP-Protokolltests,
Browserprüfung und Gesamtbuild sind bestanden; Lint der Änderungen meldet
0 Befunde. Die vollständige bestehende Lint-Baseline enthält weiterhin
bekannte Befunde. Echter Mailversand bei einem produktiven Anbieter und
ARM64-Laufzeitabnahme stehen noch aus. Die folgenden Abschnitte beschreiben
die bestehende stabile v0.2.0-Version.


## Neu in v0.2.0

- Überarbeitete responsive Oberfläche mit Hell-/Dunkelansicht und dezenter Glasoptik für Navigation und Dialoge.
- Kontomenü mit kompaktem DE/EN-Umschalter, GitHub-Link und geschütztem Abmelden.
- Versionsanzeige und Repository-Links im Footer.
- Zonenpaare im Popup bearbeiten; Routenfreigaben direkt bei den Geräten verwalten.
- Vererbbare Regeln für eigene Exits mit Benutzer-/Geräteausnahmen und verbindlicher Zonensperre.

- Zonen mit Benutzern, Geräten, gerouteten Subnetzen und gerichteten Dienstfreigaben.
- Klickbare Zonenmatrix mit farbigen Freigaben, Filterung nach Zonenpaar und direkter Regelbearbeitung.
- Aufklappbare Einstellungen, funktionierende Mehrfachauswahl und eine mobil verschiebbare Matrix mit festen Quellzonen.
- Exit-Berechtigungen je Zone mit zusätzlichen Benutzer- und Geräteeinschränkungen.
- Eigene IPv4-/IPv6-Mesh-Pools für neue Registrierungen; bestehende Adressen bleiben erhalten.
- Öffentliche IPv6-Präfixe und feste Reservierungen als Inventar mit ausdrücklich erforderlicher Gateway-/Client-Einrichtung.
- Geprüfte Entwürfe, gesonderte Aktivierung und Schutz vor konkurrierenden Policy-Änderungen.

### Hinweise vor dem Update

Konfiguration und Datenbank bei gestopptem Dienst sichern; vorhandene Volumes
beibehalten. Das Image gezielt auf `v0.2.0` setzen. Der interne Proxy-Port
bleibt 8080. Es wird keine neue Datenbankmigration eingeführt.

Der erste Wechsel in den Zonenmodus ersetzt vorhandene ACLs und Grants,
einschließlich App-Freigaben, nach ausdrücklicher Bestätigung. SSH und andere
Policy-Einstellungen bleiben erhalten. Ohne Freigabe ist auch innerhalb einer
Zone kein Zugriff erlaubt. Neue Entwürfe werden erst nach Aktivierung wirksam.
Vor einem Downgrade auf eine Version ohne Zonenunterstützung die vorherige
Policy wiederherstellen; ältere Versionen verstehen `networkZones` nicht.

Diese stabile Version wird als `v0.2.0` und `latest` für AMD64 und ARM64
veröffentlicht und als neueste stabile GitHub-Version markiert. Compose bleibt
für reproduzierbare Installationen auf `v0.2.0` festgelegt. Mit
`MESHORA_VERSION=latest` lässt sich ausdrücklich der jeweils aktuelle stabile
Stand verwenden.

### Abnahme und Grenzen

Die betroffenen Pakettests und vollständigen Race-Checks, Lint sowie der
vollständige Build sind bestanden. Geprüft sind unter anderem gerichtete
Freigaben, Exit-Ausblendung, Benutzer-/Geräteschnittmengen, Registrierungen,
Neustarts, parallele Adressvergabe, IPv6-Reservierungen, CSRF und Versionskonflikte.
Matrix-Darstellung, Kontrast, Fokus und mobiles Scrollen sind mit Chromium und
Beispieldaten in Hell- und Dunkelansicht geprüft. Echter Tailscale-Client-Verkehr
für die neuen Zonen ist noch nicht geprüft. Der Release-Workflow ergänzt einen
Container-Starttest.

Öffentliche IPv6-Reservierungen konfigurieren weder Gateway noch Client.
Subnetzrouten müssen weiterhin angekündigt und genehmigt werden; erlaubte
Exits werden am Client gewählt. Die ausführliche
[Zonen-Anleitung](https://github.com/Meister-Yoda/Meshora/blob/v0.2.0/docs/network-zones.md)
beschreibt die Voraussetzungen und verbleibenden Grenzen.

Für bestehende OIDC-Einstellungen gilt weiterhin die
[Admin-OIDC-Anleitung](https://github.com/Meister-Yoda/Meshora/blob/v0.2.0/README.de.md#admin-anmeldung-mit-authentik).
Der lokale Admin-Login ist unter `/admin/login?local=1` erreichbar.

Benötigt werden Docker und Docker Compose v2 mit Linux-Containern
(AMD64 oder ARM64).

## Start

Die Datei [compose.yaml](compose.yaml) in ein eigenes Verzeichnis kopieren:

```bash
docker compose up -d
```

Es sind keine Pflichtvariablen, Zugangsdaten oder zusätzlichen
Konfigurationsdateien in Compose erforderlich. Das Image enthält eine
Grundkonfiguration, die Docker beim ersten Start in ein eigenes Volume kopiert.

## Ersteinrichtung im Browser

`http://SERVER-IP:8080` öffnen. Bei einer neuen Installation erscheint
automatisch der Meshora-Einrichtungsassistent:

1. Die öffentliche Server-URL eingeben, zum Beispiel
   `http://192.168.1.100:8080` im LAN oder `https://mesh.example.com`
   hinter einem Reverse Proxy.
2. Eigenen Admin-Benutzernamen und ein Passwort mit mindestens 12 Zeichen
   eingeben und das Passwort bestätigen.
3. Optional OIDC einrichten: Issuer-URL, Client-ID und Client-Secret.
   Zum Beispiel `https://auth.example.com/application/o/meshora/` als Issuer.
   Die Redirect-URI ist die öffentliche Server-URL plus `/oidc/callback`.
4. **Einrichtung abschließen** wählen. Meshora speichert die Angaben,
   startet den Kontrollserver automatisch und leitet zur Anmeldung weiter.

Es gibt kein Standardpasswort und es ist kein CLI-Befehl zur Kontoerstellung
nötig. Die Ersteinrichtung ist nur auf einer neuen, leeren Installation
verfügbar. Nach der Einrichtung können keine weiteren Admin-Konten über
diesen Assistenten angelegt werden.

OIDC kann auch später unter `/admin/settings` eingetragen werden. Nach
Änderungen dort die Komponente neu starten:

```bash
docker compose restart meshora
```

Tailscale-Clients mit der eigenen Server-URL verbinden:

```bash
tailscale up --login-server=https://mesh.example.com
```

## Seiten und URLs

- `/` und `/login/*`: Anmeldung, OIDC-Start und Bestätigung vorhandener Faktoren
- `/account`: eigenes Profil und unveränderliche UID
- `/account/devices` und `/account/devices/*`: eigene Geräte
- `/account/email`: Anmeldeadresse hinzufügen oder ändern
- `/account/security` und `/account/password`: Anmeldesicherheit und Passwort
- `/admin/*`: Verwaltung der Instanz, nur mit Admin-Zugang
- `/assets/*`: gemeinsame Stylesheets und Browser-Skripte
- `/logout`: Abmeldung per geschütztem Formular

Alte Konto-, Login- und Asset-Links unter `/admin` werden auf die kanonischen
Pfade weitergeleitet. Formular-POSTs behalten dabei ihre Methode und ihren
Inhalt. Die registrierte WebUI-OIDC-Callback-URL `/admin/oidc/callback` bleibt
aus Kompatibilitätsgründen unverändert; die Provider-Konfiguration muss nicht
angepasst werden. Die OIDC-Callback-URL für Tailnet-Registrierungen bleibt
separat unter `/oidc/callback`.

## Lokale Konten: 2FA und Passkeys

Unter **Mein Konto → Sicherheit** können lokale Nutzer eine Authenticator-App
und Passkeys einrichten. Für Änderungen werden das aktuelle lokale Passwort
und bei aktiver 2FA ein Authenticator- oder Wiederherstellungscode verlangt.

Die Authenticator-App wird per QR-Code verbunden und erst nach Bestätigung
eines Codes aktiviert. Danach zeigt Meshora zehn Wiederherstellungscodes
**einmalig** an. Jeden Code sicher aufbewahren; er kann genau eine Anmeldung
oder Sicherheitsänderung bestätigen. Bereits verwendete Authenticator-Codes
werden ebenfalls nicht erneut akzeptiert. Ein administrativer Passwort-Reset
entfernt die eingerichteten Faktoren nicht.

Passkeys benötigen eine öffentliche HTTPS-Adresse; für lokale Entwicklung
ist `http://localhost` zulässig. Die Domain aus `server_url` bindet die Passkeys
an diese Installation. Ein Passkey mit verpflichtender Gerätebestätigung
(PIN oder Biometrie) kann direkt ohne Passwort und Authenticator-Code anmelden.
Nach der Einrichtung eines Passkeys oder einer Authenticator-App reicht das
Passwort allein nicht mehr zur Anmeldung. Bis zur Bestätigung eines vorhandenen
Faktors bleibt der Nutzer auf der Login-Seite und kann weder das Passwort noch
Anmeldemethoden ändern. Das gilt zusätzlich zu den Sicherheitsregeln des
OIDC-Anbieters auch für OIDC-Anmeldungen. Nur eine ausdrücklich gesetzte,
befristete Admin-Ausnahme erlaubt die dokumentierte Wiederherstellung.

Administratoren können auf der Nutzerdetailseite **2FA oder Passkey verpflichtend
verlangen**. Ohne bestätigten Faktor erhält der Nutzer nach der Anmeldung nur
Zugriff auf Passwort und Sicherheitseinrichtung. Bereits vorhandene Faktoren
müssen auf der Login-Seite bestätigt werden; sie öffnen keine neue Einrichtung. Jede Änderung der Anmeldesicherheit beendet
alle bestehenden WebUI-Sitzungen des betroffenen Nutzers, auch OIDC-Sitzungen.

Für Supportfälle gibt es getrennte Ausnahmen: die 2FA-Prüfung aussetzen und die
Passkey-Anmeldung sperren, jeweils für 15 Minuten, 1 Stunde, 4 Stunden oder
24 Stunden. Vorhandene Schlüssel werden beibehalten. Die 2FA-Ausnahme erlaubt
während der Frist eine Anmeldung ohne zweiten Faktor, auch bei bestehender
Pflicht. Solche Sitzungen enden spätestens mit der Frist. Das Verbinden einer
neuen Authenticator-App beendet die 2FA-Ausnahme sofort. Die Pausen lassen sich
jederzeit administrativ aufheben; die letzte Änderung und der verantwortliche
Admin werden gespeichert und Änderungen im Serverprotokoll erfasst. Der Admin
bestätigt die Änderung mit seiner eigenen Anmeldung.

TOTP-Geheimnisse werden mit einem aus dem bestehenden Noise-Privatschlüssel
abgeleiteten Schlüssel verschlüsselt gespeichert. Datenbank **und** die unter
`noise.private_key_path` konfigurierte Schlüsseldatei gemeinsam sichern. Die
normalen Daten- und Konfigurationsvolumes enthalten diese Dateien. Bei
Sicherheitsänderungen beendet Meshora bestehende Sitzungen. Konten mit aktiven
Faktoren können nicht als Quellkonto zusammengeführt werden; die Faktoren
müssen vorher ausdrücklich entfernt werden.

## Aktueller Build

Mit `MESHORA_VERSION=RC` wird das zuletzt veröffentlichte Build für AMD64 und
ARM64 verwendet, unabhängig davon, ob es eine stabile Version oder eine
Vorabversion ist. Der Tag `RC` wird bei jeder Veröffentlichung aktualisiert.
Für eine festgelegte Version den vollständigen Tag wie `v0.2.1-rc.3` verwenden.
Vorabversionen ändern den stabilen Tag `latest` nicht.

## Daten und Updates

Die Volumes `meshora-config` und `meshora-data` enthalten die
Konfiguration, die Datenbank und die Schlüssel. Bei Neustarts und Updates
beibehalten; vorhandene Einstellungen werden nicht durch die
Grundkonfiguration überschrieben. Vor Updates beide Volumes bei gestopptem
Meshora sichern. `docker compose down -v` löscht diese Daten.

Bei bisherigen Installationen mit Bind-Mounts wie `./config` und `./data`
diese Mounts weiterverwenden oder die Daten gezielt in die Volumes übertragen.
Bestehende Installationen werden durch den Einrichtungsassistenten nicht
zurückgesetzt.

Optional können Image, Version und veröffentlichter Port über die vorhandenen
Compose-Variablen angepasst werden. Standardmäßig wird Port 8080 verwendet.

## Image-Archive und Selbstbau

Auf der Release-Seite gibt es zusätzlich Images für AMD64 und ARM64 als
Archive mit Prüfsummen. Beispiel:

```bash
sha256sum --ignore-missing -c SHA256SUMS
docker load -i meshora_v0.2.0_linux_amd64.docker.tar.gz
```

Für das lokal geladene Image `MESHORA_IMAGE=meshora` setzen.

```bash
docker build -f Dockerfile.meshora --build-arg VERSION=dev -t meshora:dev .
```

Beim Selbstbau `MESHORA_IMAGE=meshora` und `MESHORA_VERSION=dev` verwenden.

### Temporary Tailscale SSH restriction

Meshora disables Tailscale SSH by default (`tailscale_ssh_enabled: false`).
Existing SSH rules remain stored, but clients receive an explicit empty SSH
policy and pending or previously approved SSH checks are rejected. This does
not disable normal OpenSSH connections allowed by the network policy.
Keep this setting disabled while the Tailscale SSH security fixes are rolled
out to clients. Enabling it is an explicit server-configuration opt-in;
editing SSH rules in the WebUI cannot override the disabled setting.

The restriction takes effect when this Meshora build is deployed and clients
receive its network map. It cannot stop a disconnected client from using an
old cached policy, or guarantee termination of an already established SSH
session. For immediate protection on an affected host, disable Tailscale SSH
there with `tailscale set --ssh=false`, after verifying an alternative admin
access path. Apply the upstream client fixes when available.

## Reproduzierbare Container-Basis

Regulaeres Image und Debug-Image sind auf die Multiarch-Indizes des
Headscale-0.29.6-Release-Stands festgelegt:

- regulaer: sha256:f5e95195e998742ff590b7569bae479accce1226e51ae6c3118954030121cdcb
- Debug: sha256:864c3a9f9e4718b65712bbf0ebc2c1a001ff1a3c3b80cece822532d8dc67b0bf

Die Pins stehen in Dockerfile.meshora und .goreleaser.yml. Bei einem Update
die Digests mit docker buildx imagetools inspect fuer base-debian13 und
base-debian13:debug ermitteln, beide Architekturen und das CA-Bundle pruefen
und die Pins gemeinsam aktualisieren. Ein neuer Tag allein aktualisiert die
festgelegten Images nicht. Die Builder-Go-Version bleibt separat festgelegt.

Pruefung am 28. Oktober 2026: Alle vier Basisvarianten (regulaer/Debug,
AMD64/ARM64) enthalten 148 CA-Zertifikate. Das von Headscale 0.29.6 benannte
emSign ECC Root CA - G3 ist nicht enthalten. Bundle-SHA256:
e9c7b63ee2623386e9b0a3ee32f499d01a8ea23dc83b8383ddff9f8e749543b7.

Lokale Multiarch-Builds beider Varianten und die ELF-Architekturen wurden
geprueft. Version und HTTP-Ersteinrichtung starten in beiden AMD64-Images.
Der Debug-Test verwendet denselben Dockerfile mit der in GoReleaser
festgelegten Debug-Basis; dies ist kein vollstaendiger GoReleaser-Release-Lauf.
ARM64 wurde korrekt crosskompiliert; ein ARM64-Laufzeittest steht aus, da
diese lokale Umgebung weder ARM64-Hardware noch einen ARM64-Emulator hat.

Dockerfile.meshora uebernimmt TARGETOS und TARGETARCH von BuildKit. Keine
amd64-Vorbelegung setzen: Sie wuerde ein abweichendes Multiarch-Ziel
ueberschreiben. Ein lokaler Multiarch-Build ist beispielsweise:

```sh
docker buildx build --platform linux/amd64,linux/arm64 --load \
  -t meshora-local:validation -f Dockerfile.meshora .
```

Upstream-Referenz:
[Headscale 0.29.6](https://github.com/juanfont/headscale/blob/v0.29.6/.goreleaser.yml)
und [Sicherheitshinweise](https://github.com/juanfont/headscale/blob/v0.29.6/CHANGELOG.md).

## Eigene Anmeldeadresse ergänzen oder ändern

Diese Funktion ist ab v0.2.1-rc.5 verfügbar.

Admins richten unter **Einstellungen → Mailversand** die Versandart
**SMTP oder JMAP**, Absender und Zugangsdaten ein. SMTP unterstützt STARTTLS
und direktes TLS mit Zertifikatsprüfung. Für JMAP werden die HTTPS-Session-URL
(z. B. `https://mail.example.org/.well-known/jmap`) und ein API-Token
eingetragen. Ohne Benutzername wird Bearer, mit Benutzername HTTP Basic
verwendet. Konto- und Identitäts-ID sind optional; automatisch werden das
primäre Versandkonto und die passende Absenderidentität verwendet.

Der JMAP-Server muss Mail- und Submission-Funktionen, eine zum Absender
passende Identität und einen beschreibbaren Entwürfe-Ordner anbieten.
Session-URL und gemeldete API-URL müssen denselben Host einschließlich Port
verwenden. Weiterleitungen werden abgelehnt. Temporäre Versandentwürfe
werden entfernt; bei Verbindungsabbrüchen ist die Bereinigung nur bestmöglich.
SMTP-Passwort und JMAP-Token bleiben beim Wechsel der Versandart getrennt
erhalten und werden nicht im Formular angezeigt. Als Dateigeheimnisse stehen
`mail.password_path` und `mail.jmap_token_path` zur Verfügung.

Unter **Benutzer → Benutzer auswählen → Anmeldesicherheit** lässt sich
**E-Mail selbst hinzufügen oder ändern erlauben** je Benutzer abschalten.
Das Sperren oder Ändern dieser Freigabe verwirft ausstehende Links und
beendet bestehende Sitzungen dieses Nutzers.

Nutzer tragen unter **Mein Konto → E-Mail-Adresse** ihre neue Adresse ein
und bestätigen die Aktion mit ihrem Passwort und gegebenenfalls ihrem
zweiten Faktor. Eine kürzlich bestätigte starke Anmeldung kann diese
erneute Eingabe ersetzen. Der per E-Mail versandte Link ist 20 Minuten
gültig und muss im angemeldeten Konto ausdrücklich bestätigt werden.
Falls nötig: zuerst anmelden, danach den Link erneut öffnen.

Bis zur Bestätigung bleibt die bisherige Anmeldung gültig. Danach enden
alle bisherigen Sitzungen; die UID und Gerätezuordnung bleiben erhalten.
Vom OIDC-Anbieter verwaltete Adressen werden weiterhin dort geändert.

Das persönliche Menü bündelt Profil, eigene Geräte, E-Mail, Sicherheit und
Passwort. Admins finden diese Einträge im bestehenden Hauptmenü unter
**Mein Konto**; normale Nutzer sehen ausschließlich ihr Kontomenü.
Auf kleinen Bildschirmen ist das Menü unter **Navigation** aufklappbar.
