# Meshora Roadmap

Stand: 8. November 2026.

Diese Datei ist die zentrale Liste unserer Vorhaben. Neue vereinbarte Funktionen,
Verbesserungen und offene Folgearbeiten werden hier ergänzt. Detailkonzepte werden
verlinkt. Die Reihenfolge ist eine Priorisierung, keine Terminzusage.

## Pflege und Status

- Neue Vorhaben mit Ziel, Status und nächstem Schritt aufnehmen.
- Fortschritt, Prioritäten und Hindernisse im Zuge der Arbeit aktualisieren.
- Geplant: vereinbart, Umsetzung offen. In Prüfung: Nutzen oder Architektur offen.
- In Arbeit: Umsetzung läuft, Abnahme offen. Optional: keine Umsetzungsentscheidung.
- Erledigt: Abnahme erfüllt; Ergebnis und Commit oder Release unter Abgeschlossen
  festhalten. Implementierung allein ist keine Abnahme.

## 1. Sicherheit und bestehende Funktionen absichern

### Container-Basis reproduzierbar absichern

**Status:** In Arbeit · **Priorität:** Hoch

Geprüfte Distroless-Basisimages für reguläre und Debug-Images per Digest pinnen:
`Dockerfile.meshora` und `.goreleaser.yml`. Das bereinigte CA-Zertifikatsbundle
von Headscale 0.29.6 berücksichtigen und den tatsächlichen Image-Inhalt prüfen.

**Arbeitsstand:** Pins, vier CA-Bundles, Multiarch-Builds, ELF-Architekturen
und AMD64-Start geprueft. TARGETARCH-Vorbelegung korrigiert.
**Naechster Schritt:** ARM64-Laufzeitstart auf geeigneter Hardware oder Emulation
nachholen; Details unter Abschnitt 4.
**Abnahme:** Nachvollziehbare Pins, geprüftes CA-Bundle und erfolgreiche Builds
sowie Startprüfungen für AMD64 und ARM64. Veröffentlichung im jeweiligen
Release-Auftrag.

### Zonen, Exits und Kontosicherheit abschließend abnehmen

**Status:** Geplant · **Priorität:** Hoch

- Zonenverkehr mit echten Clients prüfen: erlaubte und verbotene Verbindungen,
  Benutzer-/Geräteausnahmen, verbindliche Sperren, IPv4 und IPv6.
- Exit-Auswahl und Subnetzrouten einschließlich Widerruf prüfen. Genehmigung,
  Ankündigung und Erreichbarkeit getrennt bewerten.
- Aktuellen Arbeitsstand für gemeinsame Konten, OIDC, Zwei-Faktor-Anmeldung und
  Passkeys prüfen; offene Fehler vor dem nächsten Release beheben.

**Nächster Schritt:** Vorhandene Änderungen und Abnahmeberichte zusammenführen,
verbleibende Prüfungen festlegen. Pakettests ersetzen keine Datenpfad-Abnahme.
**Abnahme:** Die festgelegten Prüfungen sind bestanden und ihre Grenzen dokumentiert.

## 2. Headscale gezielt weiter übernehmen

### Upstream regelmäßig bewerten

**Status:** Geplant · **Priorität:** Mittel

Bei künftigen Upstream-Prüfungen Sicherheits-, Stabilitäts- und Leistungsänderungen
mit Meshora vergleichen. Bereits übernommene Änderungen erkennen, eigene Funktionen
erhalten und sinnvolle neue Arbeitspakete hier aufnehmen.

**Ausgangsstand:** Headscale `main` bis `2d220b2e` vom 24. Oktober 2026 geprüft.
Wesentliche Kernfixes sind bereits in Meshora `acdcc420` enthalten. Die drei danach
hinzugekommenen Upstream-Commits betreffen nur Release-Hinweise und Dokumentation.
Daraus folgt kein weiterer funktionaler Merge-Auftrag. Eine automatische Überwachung
ist damit nicht eingerichtet.

### Knotenzertifikate, DNS-01 und CertMagic

**Status:** In Prüfung · **Priorität:** Mittel

**Ziel:** `tailscale cert` und HTTPS mit `tailscale serve` ermöglichen; das
zusammengehörige Upstream-Paket für DNS-Challenges und CertMagic bewerten.

- Öffentliche Domain unter eigener Kontrolle und unterstützten DNS-Anbieter klären.
- DNS-Provider, Challenge-Verwaltung, `/machine/set-dns`, Zertifikatsfähigkeiten und
  TLS-Konfiguration als zusammenhängendes Paket betrachten.
- Rückwärtskompatibilität, Meshora-Setup, Einstellungen und Reverse Proxy
  berücksichtigen; DNS-Zugangsdaten geschützt behandeln.

**Nächster Schritt:** Integrationsumfang und Konfigurationsübergang ausarbeiten.
**Abnahme bei Umsetzung:** Ausstellung und Erneuerung mit echter DNS-01-Challenge,
HTTPS über Serve und geprüftes Verhalten bei Fehlern sowie Neustarts.

## 3. WireGuard als zusätzliche Zugangsmöglichkeit

**Status:** In Arbeit · **Priorität:** Nach Absicherung bestehender Funktionen

Der WireGuard-Gateway-Entwurf (Datei docs/about/wireguard-gateway-plan.md auf Branch WireGuard-Gateway) beschreibt zwei
getrennte Hauptfunktionen auf einer gemeinsamen Linux-Gateway-Grundlage. Er ist ein
Architekturgrundlage des vereinbarten Implementierungsauftrags, keine bereits
verfügbare Produktfunktion.

1. **Gemeinsamer Prototyp:** Tailscale-Clients, zwei native WireGuard-Geräte und eine
   echte externe Gegenstelle verbinden. Identitätserhaltung, Rückrouten,
   Anti-Spoofing und Sperren bei Ausfällen nachweisen.
2. **WireGuard-Exits:** FRITZ!Box, Router oder geeigneten VPN-Anbieter als verwalteten
   Ausgang nutzen. Gateway-Agent, isolierter Datenpfad, sicherer Import und
   Zonenfreigaben. Tatsächliche externe Ausgangsadresse nachweisen; kein Ausweichen
   auf den lokalen Internetanschluss und keine pauschale Heimnetzfreigabe.
3. **WireGuard-Geräte:** Geräte mit normaler WireGuard-App aufnehmen.
   Benutzer-/Zonenzuordnung, reservierte Adressen, Schlüsselwiderruf und sichere
   Einrichtung per Profil oder QR-Code. Freigegebene Exits anbinden.
4. **Optionale Erweiterungen:** Weitere Gateway-/Exit-Kombinationen, Standortnetze,
   DNS und Hochverfügbarkeit nach Abnahme der Grundfunktionen separat planen.

**Arbeitsstand:** Separater Linux-Pruefbefehl, strikter Exit-Import und isolierter
WireGuard-Pruefpfad implementiert. Bestehende Abhaengigkeiten und Lizenzen bleiben
erhalten. Lokale echte IPv4-/IPv6-Pakettests, Anti-Spoofing, Schluesselwiderruf,
bestehende TCP-Verbindungen und Tunnelausfall sind bestanden. Race-Tests, Build,
Lint (0 Befunde), Import-Fuzzing (14.151 Ausfuehrungen) und echter Dual-Stack-Test
ohne Rootrechte/Capabilities bestanden. Dies ist keine
Abnahme des vollstaendigen Meshora-Datenpfads. Details und Grenzen:
Prototyp-Dokumentation auf dem separaten Branch WireGuard-Gateway. Lokal auf Branch WireGuard-Gateway in Commit 1c5f36dc gesichert.
Die Gateway-Dateien liegen ausschliesslich auf diesem Branch. Kein Push oder Release.

**Naechster Schritt:** Gemeinsamen Paketfluss mit echten Tailscale-Clients und
Bridge sowie zwei nativen WireGuard-Geraeten in einer isolierten lokalen
Testumgebung nachweisen. Hardware-Abnahme mit externer Gegenstelle folgt danach.
**Abnahme:** Positive und negative Datenpfadtests aus dem Konzept erfüllen.
Gateway-Rechte bleiben vom Webserver getrennt. WireGuard-Geräte erhalten keine
vorgetäuschte Tailscale-Identität; Tailscale-SSH wird dadurch nicht wieder aktiviert.

## Abgeschlossen

- **23. Oktober 2026 – Headscale-Stabilitätsabgleich:** `acdcc420` übernimmt
  ausgewählte Mapper-, Policy-, Datenbank-, Lifecycle- und Shutdown-Fixes bis
  `30ff8df9`, einschließlich Go 1.27.3 und abgestimmter Abhängigkeiten. Kern- und
  gezielte Race-/Servertests, Lint, Build sowie Docker-Ping-Test bestanden;
  keine vollständige Abnahme der breiten Servertestsuite. DNS-Zertifikate und
  CertMagic blieben ausgenommen.
- **27. Oktober 2026 – Erneute Upstream-Bewertung:** Bis `2d220b2e` verglichen;
  Container-Absicherung und Zertifikatspaket als Folgearbeiten erfasst.

## 4. Gemessene Optimierungen auf main

**Stand: 28. Oktober 2026.** In Commit 3b69cf17 auf main veroeffentlicht; Release v0.2.1-rc.4 und Registry-CI erfolgreich. Fremde Arbeitskopie-Aenderungen bleiben erhalten.

- **Routenabfrage:** Sortierter Index pro Node im unveraenderlichen Snapshot;
  Wiederverwendung bei unveraenderten Routen und Kopie bei Rueckgabe.
  Vergleichstest mit 512 Routern und je einer Route: etwa 2,8 us auf 23 ns
  pro Abfrage. Bei acht Routen etwa 75 auf 21 ns. Keine zusaetzlichen
  Allokationen bei Snapshot-Wiederverwendung; beim vollstaendigen Neuaufbau
  mit 512 Routen etwa 95 KB und 529 Allokationen mehr. Die gemessenen
  Neuaufbauzeiten liegen etwa bei 0,36-0,39 ms vor und 0,37-0,38 ms danach.
  Dies sind Mikrobenchmarks, keine Aussage ueber komplette MapResponse-Latenzen.
- **Zonen-Entwurf:** Doppelte Zonenanalyse entfernt. Direkte Ausnahme und
  effektive Entscheidung kommen aus demselben vollstaendig validierten
  PolicyManager; keine Aktiv-/Entwurf-Verwechslung und kein Revisionscache.
  Bei 64 Nodes etwa 60 Allokationen und 4 KB weniger pro Pruefung.
  Laufzeiten ueberlappen (etwa 0,57-0,59 ms); kein belegter Latenzgewinn.
- **Adressvergabe:** Nach Pruefung zurueckgestellt. Parsing liegt bei
  Registrierung, nicht im laufenden Datenpfad; kein belegter Engpass.
- **Sitzungspruefung:** Nach Pruefung zurueckgestellt. Keine belastbare
  produktive Lastmessung vorhanden. Sofortiger Widerruf und Faktorpruefung
  bleiben unveraendert; kein zeitbasierter Berechtigungscache.
- **Container:** Regulaere/Debug-Multiarch-Basen gepinnt und TARGETARCH-
  Vorbelegung korrigiert. Details und ARM64-Laufzeitgrenze in
  packaging/docker/README.md.

**Validierung:** Vollstaendige Tests von hscontrol/state und hscontrol/policy/v2,
gezielte Race-Tests fuer Routen, Snapshots, Zonen, eigene Exits und WebUI,
Lint fuer die neuen Aenderungen sowie git diff --check bestanden. Regulare
und Debug-Images fuer AMD64/ARM64 gebaut, ELF geprueft, Version und HTTP-
Start auf AMD64 bestanden. Vier Basis-CA-Bundles geprueft.
ARM64-Start bleibt mangels Hardware/Emulation offen.

**Naechster Schritt:** Bei realer Last End-to-End-Latenzen und Datenbankzugriffe
messen; ARM64-Start auf geeigneter Umgebung nachholen. Kein Push oder Release.

## Release v0.2.1-rc.4

28. Oktober 2026: Veroeffentlichung des aktuellen main-Stands autorisiert.
Gateway-Code bleibt im separaten lokalen Branch WireGuard-Gateway.
Release- und Registry-CI erfolgreich:
https://github.com/Meister-Yoda/Meshora/actions/runs/40724827474
GitHub-Vorabversion mit Architekturarchiven, Pruefsummen und Compose-Bundles:
https://github.com/Meister-Yoda/Meshora/releases/tag/v0.2.1-rc.4
GHCR v0.2.1-rc.4 und RC sind identisch, jeweils AMD64 und ARM64:
sha256:f992c89009e8de62d7790aa8d65757a0a91a39b9d6e92834d1475ff8cd01a620.
latest blieb auf sha256:f9ccb8f2b14fb691d517724f9669a059bdd2a6b4a33fa906ec65a53948bf37c0.
Separater Nix-Flake-Lint meldet 39 Stil-/Komplexitaetsbefunde (auch beim
vorherigen RC fehlgeschlagen); nicht als vollstaendig gruene CI bewerten.
Weitere Server-Tests laufen zum Zeitpunkt dieser Aufzeichnung noch.

## 5. Eigene E-Mail-Anmeldeadressen mit Bestätigung

**Status:** In Arbeit · **Priorität:** Hoch · **Stand:** 28. Oktober 2026.

Normalen Nutzern das Hinzufügen und Ändern ihrer Anmeldeadresse ermöglichen.
Admins können dies je Benutzer unter Anmeldesicherheit sperren. SMTP-
Einstellungen sind unter Einstellungen → Mailversand vorgesehen; TLS und
Zertifikatsprüfung sind verbindlich. Zugangsdaten werden nicht erneut angezeigt.

**Umsetzung:** Neue append-only Migration 202610281800-user-email-changes.
Neue Adressen bleiben bis zum einmaligen, 20 Minuten gültigen Nachweis
separat. Nur der Token-Digest wird gespeichert. UID und Geräte bleiben
erhalten; Bestätigung beendet bestehende Sitzungen. Sperren und
Kontorevisionsänderungen verhindern auch das Verwenden bereits versandter
Links. Fremde Tokens, Konflikte, Ablauf und parallele Bestätigungen werden
serverseitig behandelt. OIDC-Adressen bleiben beim Anbieter verwaltet.

**Validierung:** Lokale Datenbank-/HTTP-Tests, echter SMTP-Verkehr über
STARTTLS und direktes TLS, Ablehnung nicht vertrauenswürdiger Zertifikate,
echte PostgreSQL-Bestätigung sowie Chromium-Prüfung für Desktop/Mobil und
Dunkelansicht. Vollständige Tests der Pakete db, webui, templates, types
und state, gezielte Race-Tests, Gesamtbuild und Lint der Änderungen
(0 Befunde) bestanden. Upgrade einer bestehenden Sicherheitsrichtlinie
und parallele Bestätigungen sind ebenfalls geprüft.
Ein realer ausgehender Mailserver ist nicht konfiguriert; keine echten
Nutzeradressen angeschrieben.

**Folgepunkt:** Sobald eine Benutzergruppenverwaltung eingeführt wird,
E-Mail-Freigaben auch auf Gruppenebene ergänzen und Vorrangregeln festlegen.
Keine neue Gruppenverwaltung im Rahmen dieses Arbeitspakets.
**Veröffentlichung:** Veröffentlicht als v0.2.1-rc.5, Commit 565b3219.

## 6. Persönliche Seiten mit gemeinsamem Menü

**Status:** Veröffentlicht als v0.2.1-rc.5 · **Stand:** 28. Oktober 2026.

Die lange Kontoseite ist auf Profil, eigene Geräte, E-Mail-Adresse,
Sicherheit und Passwort verteilt. Normale Nutzer erhalten ausschließlich
ihr Kontomenü. Admins erhalten dieselben persönlichen Einträge als Gruppe
„Mein Konto“ im vorhandenen Menü neben Netzwerk und Verwaltung.
Die bestehende mobile Navigation und die vorhandenen Menü-Styles werden
wiederverwendet. Verpflichtende Passwort-/Faktoreinrichtung bleibt ohne
Bereichsmenü; Zugriffs- und Eigentumsprüfungen bleiben serverseitig erhalten.

**Validierung:** Vollständige Race-Tests für webui/templates und Gesamtbuild
bestanden. Gezielte Tests prüfen getrennte Seiten, aktive Einträge,
Admin-/Nutzerrollen, URL-Präfixe und eingeschränkte Ersteinrichtung.
Chromium-Prüfung für Desktop, Mobilgeräte und Dunkelansicht sowie
E-Mail-Bestätigung ist bestanden. Lint der Änderungen meldet 0 Befunde;
git diff --check und Tastaturzugriff auf alle Admin-Einträge sind bestanden.
Veröffentlicht als v0.2.1-rc.5, Commit 565b3219.

## 7. JMAP als Alternative zum SMTP-Mailversand

**Status:** Veröffentlicht als v0.2.1-rc.5 · **Stand:** 28. Oktober 2026.

Vereinbart: JMAP zusätzlich zu SMTP für Bestätigungslinks. Admins wählen die
Versandart unter Mailversand. Getrennte Zugangsdaten, HTTPS-Zertifikatsprüfung,
keine Weiterleitungen und keine Weitergabe an fremde API-Hosts. Versandkonto,
passende Absenderidentität und beschreibbarer Entwürfe-Ordner werden geprüft.
API-Token per Bearer oder Benutzername/Token per HTTP Basic; Geheimnisdateien
werden unterstützt. Keine zusätzliche Datenbankmigration oder Abhängigkeit.

**Validierung:** Lokale TLS-Protokolltests für Versand, Basic/Bearer,
fehlende Rechte/Funktionen, abweichenden Absender, Create-/Submission-Fehler,
fremde API-Hosts, Weiterleitungen, ungültiges Zertifikat, Abbruch und
Antwortgrößenlimit bestanden. Vollständige Race-Tests für webui/templates/types,
Gesamtbuild und Chromium-Prüfung auf Desktop/Mobil bestanden. Gespeicherte
SMTP-/JMAP-Geheimnisse bleiben beim Wechsel erhalten und werden nicht ausgegeben.
Kein realer JMAP-Anbieter konfiguriert. Veröffentlicht als v0.2.1-rc.5, Commit 565b3219.


## RC.5-Veröffentlichung (28. Oktober 2026)

v0.2.1-rc.5 auf main, Commit `565b3219`, ohne WireGuard-Gateway.
[Release](https://github.com/Meister-Yoda/Meshora/releases/tag/v0.2.1-rc.5)
und [Release-Workflow](https://github.com/Meister-Yoda/Meshora/actions/runs/40741999810).

GitHub-Release-Prüfungen (Race-Tests und Gesamtbuild), AMD64-/ARM64-Builds
sowie AMD64-Start-/Health-/Setup-Test bestanden. Release-Downloads,
Prüfsummen und Compose-Pakete sind öffentlich. GHCR-Tags
`v0.2.1-rc.5` und `RC` enthalten beide Architekturen und denselben Digest:
`sha256:ef220d989375a28ad1c658a75556e90b72d787da79c06343414b31518418cdce`.
`latest` bleibt unverändert auf dem stabilen v0.2.0-Digest
`sha256:f9ccb8f2b14fb691d517724f9669a059bdd2a6b4a33fa906ec65a53948bf37c0`.

Offen bleiben echter Mailanbieter-Versand, ARM64-Laufzeitabnahme und die
bereits dokumentierte vollständige Lint-Baseline. Datenbank und Konfiguration
vor dem Upgrade sichern; RC.5 ergänzt die Migration für E-Mail-Bestätigungen.

## 8. Übersichtlicher Mailversand

**Status:** Lokal umgesetzt und geprüft · Veröffentlichung offen.

Mailformular auf gewählte Versandart konzentrieren. SMTP-/JMAP-Felder
werden passend zur Auswahl angezeigt; Werte der anderen Versandart bleiben
beim Speichern erhalten. Optionale JMAP-IDs und Protokollvoraussetzungen
stehen unter „Erweiterte Einstellungen“. Ohne JavaScript bleiben beide
Bereiche bedienbar. Passwortbestätigung ist vom Mailzugang getrennt.

Validierung: Chromium auf Desktop und Mobilgerät; Umschalten der Versandart,
eingeklappte Optionen und Werterhalt geprüft. Mail-Einstellungstests bestanden,
Lint der Template-Änderung 0 Befunde, git diff --check bestanden.
Veröffentlicht als v0.2.1-rc.6, Commit 31daceef.
OIDC-Prüfung: Lokale Race-Tests für WebUI- und Geräte-Anmeldung bestanden.
PKCE, Browserbindung, Replay-Schutz, Tokenprüfung, Rollen/Gruppen und
Zwei-Faktor-Vorgaben geprüft; echte Authentik-Abnahme steht aus.


## RC.6-Veröffentlichung (28. Oktober 2026)

[Release v0.2.1-rc.6](https://github.com/Meister-Yoda/Meshora/releases/tag/v0.2.1-rc.6),
Commit `31daceef`, ohne WireGuard-Gateway.
[Release-Workflow](https://github.com/Meister-Yoda/Meshora/actions/runs/40745655901)
erfolgreich: Release-Race-Tests, Gesamtbuild, AMD64-/ARM64-Container,
AMD64-Start-/Health-/Setup-Test und öffentliche Downloads inklusive
Prüfsummen und Compose-Pakete.
GHCR `v0.2.1-rc.6` und `RC`:
`sha256:d22cbce6d1bf08fbdea40d70860e772d366e4c7868b0fccc188308c62717d76a`.
`latest` unverändert:
`sha256:f9ccb8f2b14fb691d517724f9669a059bdd2a6b4a33fa906ec65a53948bf37c0`.
Echte Authentik-/Mailanbieter-Abnahme und ARM64-Laufzeit bleiben offen.

## 9. OIDC mit bestehendem Passkey ohne Login-Rückfall

**Status:** Lokal umgesetzt und geprüft · unveröffentlicht.

OIDC-Konten mit vorhandenen Passkeys erhalten direkt die gebundene
Faktorbestätigung. Passkey und Authenticator werden passend zur Einrichtung
angeboten. Die Bestätigung muss dieselbe UID und Kontorevision betreffen;
OIDC-Ablaufzeit und Identität bleiben erhalten. Keine Kontositzung vor der
erfolgreichen Faktorprüfung, kein pauschales Vertrauen in externe MFA-Claims.


Validierung: Vollständige Race-Tests für webui/templates bestanden,
Gesamtbuild und Lint der Änderungen (0 Befunde) bestanden. Echte signierte
WebAuthn-Antworten prüfen den Erfolg ohne lokales Passwort sowie Ablehnung
falscher UID, geänderter Revision, abgelaufener OIDC-Anmeldung, falscher
Signatur, falschem CSRF und pausierten Passkeys; Replay wird abgewiesen.
Chromium prüft die mobile Faktoransicht, aktiven Passkey-Button und
Authenticator-Alternative. Bestehende OIDC-/TOTP-Abläufe bleiben getestet.
Für erneute RC.6-Veröffentlichung freigegeben; echte Authentik-Abnahme offen.

## 10. Mailformular: produktive Skriptrichtlinie und Abstände

**Status:** Lokal korrigiert und geprüft · unveröffentlicht.

RC.6 hatte die Seitenschaltung durch die Standard-CSP blockiert. Die
Mailseite erlaubt nun ausschließlich eigene Skripte. Die Browserprüfung
übernimmt die restriktive CSP; HTTP-Regression prüft den echten Header.
Verschachtelte Formularbereiche erhalten die vorhandenen Feldabstände.
Die frühere reine HTML-Vorschau hatte diesen Headerfehler nicht erfasst.

Validierung: HTTP-Header-Regression und Mail-Einstellungstests bestanden.
Chromium mit restriktiver CSP prüft aktive Versandart, Umschalten, Werterhalt
und mobile Darstellung. Lint der Änderungen: 0 Befunde; diff --check bestanden.
Erneute Veröffentlichung des bestehenden RC.6 abgeschlossen, Commit 9ba19b75.

RC.6-Update: Bestehenden Tag und Artefakte auf ausdrücklichen Nutzerwunsch
ersetzen; kein RC.7. Vorheriger Tag-Objektstand:
`72010a4fea77f5d630f51ab9e326179de4edfc2f`.


## RC.6-Update bestätigt (29. Oktober 2026)

Release-Workflow 40750492272 erfolgreich abgeschlossen. RC.6-Tag, Downloads
und Prüfsummen auf Commit 9ba19b75 aktualisiert. OIDC-Passkey-Fortsetzung
und CSP-/Abstandsfix der Mailseite enthalten. Release-Tests, Gesamtbuild,
AMD64-/ARM64-Build und AMD64-Starttest bestanden.
GHCR v0.2.1-rc.6 und RC zeigen auf:
`sha256:13f89fa346a8990ca23000772902b288173b3e5999a4b2579c5e8bcded87a7c2`.
latest bleibt unverändert. Bestehenden RC.6 erneut pullen und Container
neu erstellen. Echte Authentik-/Mailanbieter- und ARM64-Abnahme bleiben offen.


## v0.2.2 als stabile Veröffentlichung (30. Oktober 2026)

Vom Nutzer freigegeben: aktuellen Main-Stand als v0.2.2 veröffentlichen
und latest sowie RC auf denselben neuen Multiarch-Digest aktualisieren.
Enthält den aktualisierten RC.6 einschließlich OIDC-/Mailseiten-Fixes,
weiterhin ohne WireGuard-Gateway. Status: veröffentlicht.


## v0.2.2 bestätigt (30. Oktober 2026)

[Stabiles Release](https://github.com/Meister-Yoda/Meshora/releases/tag/v0.2.2),
Commit `46b0d74b`, als neuestes GitHub-Release markiert.
[Release-Workflow](https://github.com/Meister-Yoda/Meshora/actions/runs/40987158677)
erfolgreich: Race-Tests, Gesamtbuild, AMD64-/ARM64-Builds,
AMD64-Start-/Health-/Setup-Test, Downloads und Prüfsummen.

GHCR `v0.2.2`, `latest` und `RC` haben denselben Multiarch-Digest:
`sha256:de5af952c0ee4a9c80ca921e60ea5762d44d975317c58d4b2b16d8fc81e146b9`.

Separater Docs-Deploy 40987158590: Dokumentation gebaut, Push nach gh-pages
mit HTTP 403 abgewiesen (fehlende Schreibrechte für github-actions[bot]).
Nicht Teil des erfolgreichen Release-/Registry-Workflows; Folgepunkt offen.
Echte Authentik-/Mailanbieter-Abnahme, ARM64-Laufzeit und die bekannte
vollständige Lint-Baseline bleiben separat offen.

## 11. Testmail und eigene Sitzungen

**Status:** Lokal umgesetzt und geprüft (30. Oktober 2026), unveröffentlicht.

Admins können mit gespeicherten SMTP-/JMAP-Einstellungen eine feste Testmail
versenden: bestätigte Anmeldung, CSRF, begrenzte Versuche und keine
Providerantworten oder Geheimnisse in Fehleranzeigen. Übergabe und Zustellung
werden sprachlich unterschieden.

Eigene Sitzungen im Kontomenü mit Anmeldezeit, Art und Browserangabe anzeigen.
Einzeln, alle anderen oder alle eigenen Sitzungen beenden; fremde Konten
bleiben unzugänglich. Flüchtige Sitzungen bleiben im vorhandenen Servercache.
Keine Datenbankmigration. Unveröffentlicht.

Produktive Abnahme: Nutzer bestätigt Authentik-Login samt zweitem Faktor
unter mesh.langsebastian.eu. Mailversand aktuell nicht testbar; bleibt offen.

## 12. Einladungen für lokale und OIDC-Konten

**Status:** Lokal umgesetzt und geprüft, unveröffentlicht.
Admins wählen lokalen Passwortzugang oder OIDC je Einladung. Link 24 Stunden,
einmalig, Hash gespeichert; Versand per eingerichtetem SMTP/JMAP. OIDC muss
die eingeladene Adresse bestätigt liefern. Keine Kontenübernahme oder
Adminrechte, bestehende OIDC-Zulassung unverändert. Append-only Migration
202610301200-user-invitations. Unveröffentlicht.


Validierung für Testmail, Sitzungsverwaltung und Einladungen:
- Vollständige Race-Tests für webui, templates, db, state und types bestanden.
- Gesamtbuild, Lint der Änderungen (0 Befunde) und git diff --check bestanden.
- SQLite und isoliertes echtes PostgreSQL prüfen Einmaligkeit, Ablauf,
  erneute Einladung, Widerruf und konkurrierende Annahme.
- HTTP-Tests prüfen Admin-/Benutzerrechte, CSRF, Versandlimit, gespeicherte
  Mailkonfiguration, generische Fehlermeldungen und entfernte Sitzungen.
- Vollständiger OIDC-Testanbieter prüft Einladung mit passender bestätigter
  Adresse und Ablehnung falscher/unbestätigter Adressen.
- Chromium prüft Desktop/Mobil mit restriktiver CSP, bedienbare Sitzungen,
  Mailformular und Einladung; Linktoken wird aus der Browseradresse entfernt.

Veröffentlicht als v0.2.3-rc.1, Commit 53c8ace2. Keine produktiven Testmails versendet. Vor einem
späteren Upgrade Datenbank sichern (neue Einladungsmigration).

## v0.2.3-rc.1 (30. Oktober 2026)

Vom Nutzer freigegeben: Testmail, Sitzungsverwaltung und Einladungen
als v0.2.3-rc.1 veröffentlichen und RC aktualisieren. latest bleibt v0.2.2.
Status: veröffentlicht.


## v0.2.3-rc.1 bestätigt (30. Oktober 2026)

[Release](https://github.com/Meister-Yoda/Meshora/releases/tag/v0.2.3-rc.1),
Commit `53c8ace2`.
[Workflow](https://github.com/Meister-Yoda/Meshora/actions/runs/40991351841)
erfolgreich: Release-Race-Tests, Gesamtbuild, AMD64-/ARM64-Container,
AMD64-Start-/Health-/Setup-Test, öffentliche Downloads und Prüfsummen.

GHCR `v0.2.3-rc.1` und `RC`:
`sha256:96e45eab0de4a81d65b08e7a4d5215d027c39e2c9fa0ee6e515a3913c90d0ef4`.
`latest` bleibt auf v0.2.2:
`sha256:de5af952c0ee4a9c80ca921e60ea5762d44d975317c58d4b2b16d8fc81e146b9`.

Testmail, Sitzungsverwaltung und lokale/OIDC-Einladungen sind damit
als Vorabversion veröffentlicht. Vor Upgrade sichern
(Migration 202610301200-user-invitations).
Produktiver Mailversand bleibt offen; Gateway nicht enthalten.

## 13. Eigene Meshora-Exit-Node (30. Oktober 2026)

**Status:** Lokal vorbereitet, Paketfluss-Abnahme fehlgeschlagen; unveröffentlicht.
Optionale compose.exit.yaml stellt meshora-exit als eigenes Tailscale-Gerät
auf dem Meshora-Docker-Host bereit. Kontrollserver ohne zusätzliche Rechte,
gepinnte stabile Clientversion, eigener Zustand und Schlüsseldatei.
IPv4/IPv6-Exitfreigabe und Clientauswahl bleiben explizit. Keine automatische
ACL-/Zonenfreigabe, kein WireGuard-Gateway-Code. Releasepakete enthalten die
optionale Datei. Reale Paketflussprüfung vor Verifikation erforderlich.
Unveröffentlicht; kein produktives Deployment autorisiert oder vorgenommen.


Validierung der eigenen Exit-Node:
- Normale Compose-Kombination und Kombination mit Traefik validiert.
- Stabiles Tailscale v1.104.4 als Multiarch-Digest gepinnt.
- Tatsächlicher Compose-Dienst registriert sich mit Schlüsseldatei und
  read-only Root-Dateisystem an isoliertem Meshora v0.2.3-rc.1.
- Gerät erscheint, IPv4-/IPv6-Exit-Routen lassen sich freigeben; Testclient
  wählt den Exit und verbindet sich direkt.
- Paketfluss zum getrennten Testziel zweimal mit
  `packet flow through exit failed` fehlgeschlagen. Weitere Versuche
  gemäß Fail-fast-Regel eingestellt. Keine Freigabe als verifiziert.
- Neustart mit verbrauchtem Schlüssel, Abschaltverhalten und echter
  IPv6-Ausgang wurden dadurch noch nicht geprüft.
- Isolierte Container/Netzwerke bereinigt; Produktivinstanz unverändert.

Nächster Schritt: Ursache im Paketfilter/Exit-Datenpfad mit reproduzierbaren
Paketmitschnitten und expliziter Internetberechtigung eingrenzen. Keine
globale Lockerung von Zonen-/Nutzerregeln. Noch kein Commit, Push oder Release.

## 14. Integrierte Meshora-Exit-Funktion: vereinbarter Umfang

**Status:** Anforderungen vereinbart, noch nicht implementiert.
Diese Vorgabe ersetzt die separate Compose-Exit-Datei aus Abschnitt 13 als
gewünschte Produktlösung. Der dortige Versuchsstand bleibt unveröffentlicht.

- Meshora bietet eine eingebaute eigene Exit-Funktion; kein zusätzlich vom
  Betreiber einzurichtender Tailscale-Dienst.
- Die eigene Exit-Identität wird wie andere Exits einer Zone zugeordnet.
  Bestehende Zonen-, Benutzer- und Gerätefreigaben sowie explizite
  Exit-Sperren gelten unverändert; keine Sonderfreigabe durch Aktivierung.
- Separater, nur administrativ bedienbarer Ein-/Ausschalter. Standard aus.
  Der Zustand bleibt über Neustarts erhalten.
- Ausschalten muss die lokale Weiterleitung stoppen und die Ankündigung
  beider Default-Routen (IPv4/IPv6) zurücknehmen. Nur die Oberfläche oder
  eine Routenmarkierung umzuschalten reicht nicht.
- Zonenregeln und Zuordnung beim Abschalten erhalten. Erneutes Einschalten
  stellt keine zusätzlichen Berechtigungen her.
- Gewünschter und tatsächlich erreichter Betriebszustand getrennt anzeigen;
  fehlende Netzwerkvoraussetzungen sind ein Fehlerzustand.
- Keine Garantie für das Fallback-Verhalten fremder Clients; das Abschalten
  des Server-Exits darf nicht als clientseitiger Kill-Switch beworben werden.

Abnahme erforderlich: erlaubte und gesperrte Zonen, Benutzer-/Geräteausnahmen,
echter IPv4-/IPv6-Paketfluss, Abschalten unter laufendem Verkehr, Neustart
im ausgeschalteten Zustand und Wiederaktivierung ohne Rechteausweitung.
Die zweimal fehlgeschlagene Paketflussprüfung aus Abschnitt 13 ist weiterhin
offen und wird durch die geänderte Verpackung nicht als gelöst betrachtet.


## 15. Headscale-Upstream-Prüfung (8. November 2026)

**Status:** Review abgeschlossen, Übernahmen vorgeschlagen; keine Kernänderung
in diesem Review übernommen und kein Release gestartet.

**Vergleich:** Meshora main `44e87797` gegen Headscale main
[`48416ce0894ef7bef0ef9c064609b4aa76a0d9b6`](https://github.com/juanfont/headscale/commit/48416ce0894ef7bef0ef9c064609b4aa76a0d9b6)
(Upstream-Commit vom 6. November 2026). Seit dem zuletzt geprüften
`2d220b2e` liegen 360 Commits im Vergleichsbereich. Enthalten sind auch
ältere Autorendaten, später integrierte Änderungen, Tests, Mitschnitte,
generierte Dateien und Releasepflege; die Zahl ist keine Liste von
360 fehlenden Funktionen.

Vorgehen: aktueller GitHub- und Git-Stand abgeglichen, Commitinventar und
Upstream-Changelog ausgewertet, sicherheits- und integrationsrelevante
Originaldiffs mit Meshora verglichen. 17 ausgewählte Patches nur mit
`git apply --check` geprüft, ohne sie anzuwenden; 6 passen im geprüften
Code-/Testkontext direkt. Patch-Kontext ist kein Kompilierungs-,
Laufzeit- oder Sicherheitsnachweis. Keine neuen Laufzeittests im Review.

Bestehende lokale Exit-Compose-Arbeiten und der integrierte Exit-Plan
aus Abschnitt 14 bleiben erhalten und unveröffentlicht.

### P0: Sicherheit zuerst

1. **OIDC-E-Mail-Verifikation zusammenhalten**
   [208043c8](https://github.com/juanfont/headscale/commit/208043c88d2205c45f34b5b27439c04cb41b0f6b).
   Meshora `hscontrol/oidc.go` kombiniert noch separat
   `cmp.Or(userinfo2.Email, claims.Email)` und die Verifikationsflags.
   Dadurch kann die Bestätigung einer Adresse auf eine andere Adresse
   übertragen werden. Upstream-Logik übernehmen, Tests auf Meshora
   anpassen. Der betroffene Geräte-OIDC-Pfad ruft dieselbe
   `ResolveOIDCUser`-Kontoverknüpfung auf.
   WebUI liest derzeit direkt den ID-Token; trotzdem gemeinsame
   Kontoschutztests für WebUI, Geräte-Login und Einladungen ergänzen.
   Abnahme: wechselnde/leere UserInfo-Adresse und unabhängige Flags,
   unverifizierte Adresse darf kein bestehendes Konto verknüpfen.

2. **OIDC-Subject unverändert halten und Benutzerupdates konsistent machen**
   [499b58e7](https://github.com/juanfont/headscale/commit/499b58e77c81ef59205046f749515b23c0c30a90),
   [14baf554](https://github.com/juanfont/headscale/commit/14baf554216736519b02421d8c33499c8911da92).
   Subject-Prüfung fehlt im gemeinsamen Meshora-Pfad. Prüfung auch für
   Einladungsannahme und WebUI vornehmen. Cache-/Benutzeraktualisierung
   übernehmen, aber `EnsureUserAccount`, Login-E-Mail-Eindeutigkeit,
   UID, Sitzungsrevisionen und verwaltete OIDC-Adressen erhalten.
   Upstreams pauschales Leeren von E-Mails nicht isoliert kopieren.
   Das ist ein Anpassungspaket, kein Ersetzen der Kontoimplementierung.

3. **Öffnung der Metrics-Route im Noise-Tunnel entfernen**
   [cd66e241](https://github.com/juanfont/headscale/commit/cd66e2414a9a2add456459f21671765f4576888b).
   `r.Handle("/metrics", metrics.Handler())` ist noch vorhanden.
   Der Patch passt direkt. Den vorgesehenen Metrics-Listener erhalten.
   Keine Aussage, dass Meshoras produktiver Metrics-Port öffentlich wäre.

4. **Registrierung und Anfragen gegen Ressourcenverbrauch härten**
   Zusammenhängendes Paket: `2e563635`, `4ce92841`, `5a4d366c`,
   `28959cb2`, `7892a846`, `b833be28`, `c9e9f728`, `c0925962`,
   `15a8c758`, `e3c1bfd6`, `21c91d4b`.
   Hostinfo-/Endpoint-/Routenlimits, Ablaufzeitvalidierung, Idle-/Body-Timeouts,
   Prüfung des wartenden Geräteschlüssels, begrenzte Pending-Registrierungen
   und begrenzte Logfelder gemeinsam portieren. Der vorhandene Body-Limiter
   allein deckt diese Fälle nicht ab.
   Meshoras WebUI-Sitzungen, Passkey-/OIDC-Challenges und Einladungen
   dürfen nicht in einen gemeinsamen verdrängenden Cache gezogen werden.

5. **Schlüssel-, Eigentums- und Löschrennen**
   `e67ef861`, `e1de83cb`, `d67ee0bd`, `b1ab98bd`, `221a387b`,
   `b5bebf9b`, `bd74ce67`, `7721d8c2`, `0ecbeafb`,
   `e8461504`, `ba319ba8`.
   Einmalschlüssel serialisieren; MapRequests an den autorisierenden
   NodeKey binden; fremde Schlüssel auf getaggten Geräten dürfen keine
   fremde Identität übernehmen; IPs nur nach tatsächlicher Löschung
   freigeben. Meshora befreit IPs derzeit nach jedem erfolgreichen
   idempotenten Delete-Aufruf. Ephemeral-/Expiry-/Key-Rotation als
   zusammenhängendes Paket samt eigenen Datenbankmigrationen planen.
   SSH bleibt standardmäßig deaktiviert; das Paket aktiviert SSH nicht.

6. **Credentials, OAuth und API-Grenzen**
   [41067c7e](https://github.com/juanfont/headscale/commit/41067c7e6e49e3cc9e12d9060175985730d15d73)
   passt direkt: Wartezeit alter Hash-Prüfungen begrenzen, Credential nach
   Prüfung erneut laden, damit paralleler Widerruf wirksam bleibt.
   Zusätzlich `0bf5b0f0`, `408b9b77`, `77005fac`, `4e946edb`,
   `9979be14` für begrenzte API-Bodies, redigierte Fehler, frühe
   Scope-Prüfung und Tag-/Tokenberechtigungen prüfen und übernehmen.

### P1: Exits, Zonen und Datenbankverhalten angepasst integrieren

- **Exit-Routen nur bei tatsächlicher Internetberechtigung anbieten:**
  [256a67e8](https://github.com/juanfont/headscale/commit/256a67e86bcebd0287362a9cdbf6ef16c3ee82bb).
  Meshora addiert ExitRoutes in `RoutesForPeer` noch gesondert; Tests
  müssen lokale Ziele gegenüber Internetzielen unterscheiden.
  Freigabe, Sichtbarkeit und Datenverkehr weiterhin separat prüfen.

- **Mehrere via-Grants/HA richtig kombinieren:**
  [6a1f8d15](https://github.com/juanfont/headscale/commit/6a1f8d1579935f20ff7156c6a9f69f4998303153),
  `fc107292`, `4342883b`, `e0717bc1`, `672aa401`.
  Nicht beim ersten überlappenden Grant auf einen falschen globalen
  Primärrouter wechseln; Wahl nach gesunder, ältester Kontrollsitzung,
  zusammenhängende Failover-Updates. IPv4-/IPv6-Exits gemeinsam testen.

- **Regellose Policies und Sichtbarkeit:**
  [9f88e7dd](https://github.com/juanfont/headscale/commit/9f88e7ddcf67ad03e041b603a452c17fcff502ab),
  `d2a85b1d`, `49030541`, `99b2d949`.
  Meshoras globaler Compiler behandelt fehlende Regeln bereits anders als
  der knotenspezifische Compiler (`pol == nil` allein). Upstreams Korrektur
  ist relevant, aber fehlende Regeln, explizit leere Regeln und die
  Zonen-generierte Policy müssen getrennt behandelt werden.
  Gleiche Benutzeridentität darf keine verbindliche Zonensperre umgehen;
  zusätzliche Peer-Sichtbarkeit ist nicht automatisch erlaubter Verkehr.

- **Wichtige Einordnung zum eigenen Exit:** Diese Upstream-Fixes betreffen
  Steuerung/Filter. Sie belegen weder eine Ursache noch eine Lösung des
  zweimal fehlgeschlagenen Compose-Paketflusstests aus Abschnitt 13.
  Sie implementieren auch keinen eingebauten Exit-Datenpfad.
  `ExitNodeID` bleibt serverseitig
  (`9ab6a64e`, `99e8a922`); nicht als allgemein peer-sichtbare
  Information für den integrierten Exit verwenden.

- **Manuelle und automatische Freigaben unterscheiden:**
  `bdfe6fc3` + `6876bcfa` + `3e9195e4` +
  `6fe62dec`. Neue `auto_approved_routes`-Spalte mit passenden
  Tests und Anzeige im Meshora-Routen-UI einplanen. Ein Widerruf darf
  keine manuelle Freigabe versehentlich löschen.

- **Alte Datenbanken sicher ablehnen:**
  [059b95ba](https://github.com/juanfont/headscale/commit/059b95bacce36dba47b1e8dbfd06b8ac21fe88be)
  plus `aa8d73ea`. Endgültige Upstream-Variante übernehmen, nicht nur
  den vorherigen, später korrigierten `3bb5789d`-Zwischenschritt.
  Snapshot der abgelehnten SQLite-Datei muss unverändert bleiben.
  Meshoras Versionslogik und alle Konto-/Mail-/Einladungsmigrationen
  erhalten. Neue Migrationen ausschließlich hinten anhängen.

- **Nullable Schlüsselabläufe und Geräte-Lebensdauer:**
  `92bc0b62`, `1a32d031`, `f32fc9f1`, `59f9f78b`,
  `e99ad440`, `060f145a`, `5adc504f`.
  UI-/API-Serializer und Upgradepfad anpassen; keine fremde
  `schema.sql` oder Migrationsliste über Meshoras Schema kopieren.
  Letzte Kontrolladresse nur mit korrekter Trusted-Proxy-Behandlung.

### Direkt übernehmbare kleinere Bausteine

Im Patch-Kontext inklusive berücksichtigter Code-/Testdateien passend:
- `cd66e241`: Metrics nur über den vorgesehenen Listener.
- `41067c7e`: begrenzte alte Hash-Prüfung und Widerruf nachladen.
- `d0e10f2d`: JSON-Dateien als HuJSON einlesen.
- `dc9513f4` + `4a531ec7`: capver-Werkzeug bei Abruffehlern nicht
  still unvollständig erzeugen und raw.githubusercontent.com verwenden;
  dazu spätere Tests `48416ce0` und Stilkorrektur `9a199a7e`.
- `a63ee3e0`: ACL-Validierungsendpunkt, nützlich für Terraform;
  zugehöriges OpenAPI-/Client-Generat separat regenerieren.

Auch hier sind abhängige Commits, bestehende APIs und Tests vor Integration
zu prüfen. OIDC-Fix `208043c8` ist inhaltlich klein, passt als kompletter
Patch wegen der geänderten Meshora-Tests jedoch nicht direkt.

### P2: Stabilität, Leistung und optionale Funktionen

- DERP: `a25aff2c` (Nicht-200 ablehnen), `017ab6b2`
  (fehlerhafte Quellen überspringen, letzte gültige Map erhalten),
  `6f2f97ca` (HomeParams-Panic), `581ec5b3` (stabile Reihenfolge).
  Die aktuelle Meshora-DERP-Implementierung liegt vor deren Voraussetzungen;
  daher als Paket adaptieren, nicht Einzelpatches erzwingen.
  Scores/policy.derpMap (`502a47c7`, `b54570d3`) sind zusätzliche
  Funktionen und kein notwendiger Teil jedes Fehlerfixes.
- Leistung: `137a66d5`, `dea3af08`, `4b2d227f`,
  `91b920cc`, `800392ae`, `526daf08`.
  Kompilierte Matcher und autogroup:self wiederverwenden.
  Upstream-Benchmarks sind keine Meshora-Messung; Zonen-Workload messen
  und die vorhandene PrimaryRoutesForNode-Optimierung erhalten.
- SQLite `34653a1b`: Prepared-Statement-Cache abschalten und betroffene
  Konfigurations-/Transaktionstests übernehmen; nicht isoliert alte
  Konfigurationsannahmen übernehmen.
- Mapper/Lifecycle: `f37c6a69`, `a886c717`, `91168cc4`,
  `c2b4c97c`, `2d094f22` für begrenzte Streams/Wartezeiten,
  korrekte Initialmaps und weniger nutzlose Frames.
- Remote-Clientupdates: Paket `672340c0`, `1386772d`,
  `c8e5aa66`, `8230c58a`, `7af29e8c`, `f9992637`,
  `fda2a123`, `ac25916b` plus Clientgenerierung/Tests.
  Eigener Funktionsvorschlag: im Meshora-UI nur mit Adminberechtigung,
  Nachbestätigung und realem Status, keine pauschale Ausführung.
- Zertifikate/DNS-01/CertMagic bleiben separates Paket aus Abschnitt 2.
  `b3ffec42` (manuelle TLS-Zertifikate per SIGHUP nachladen) und
  `8bc7e793` (ACME-Retries behalten konfigurierte CA) benötigen
  Abgleich mit Meshoras TLS- und Proxy-Betrieb.

### Bewusst erhalten / nicht pauschal übernehmen

- Kein kompletter Upstream-Merge über WebUI, gemeinsame Konten, Passkeys,
  Einladungen, Sitzungen, SMTP/JMAP und den Zonencompiler.
- Kein Ersetzen von go.mod/go.sum: Das Upstream-Diff entfernt unter anderem
  Meshoras WebAuthn-/OTP-Abhängigkeiten und wechselt die Moby-/Docker-Basis.
  Einzelne Updates gezielt mit gekoppelten gVisor/Tailscale- und
  SQLite/libc-Pins sowie neuem Vendorhash planen; Go bleibt kompatibel
  mit dem Nix-Toolchain-Pin.
- Bereits vorhandene Basisfunktionen nicht als neu ausgeben: HTTP-v1/v2,
  OAuth/Credential-Grundlage, zentraler State/NodeStore und gesicherte
  Containerbasen sind schon vorhanden. Neu sind die jeweiligen Korrekturen.
- Upstream-Dokumentation, SaaS-Mitschnitte und Testdaten als Belege nutzen,
  aber Meshora-Texte/Release-Workflow nicht durch Upstream-Vorlagen ersetzen.
- Kein Zusammenhang aus Paketfilter-Änderungen und tatsächlichem
  Internet-Paketfluss behaupten, bevor der reale Test bestanden ist.

**Vorgeschlagene Reihenfolge:** P0 OIDC + Metrics + Credentials als erste
kleine Änderung, danach Registrierungs-/NodeKey-/Löschrennen; anschließend
Exit-/Zonen-Policy samt Tests und Migrationen; danach DERP/Leistung.
Optionale Remoteupdates und Zertifikate gesondert entscheiden.


## 16. Cloud-Umsetzung des Upstream-Reviews (8. November 2026)

**Status:** Auf Nutzerwunsch lokale Umsetzung begonnen; Cloud-Start entfällt.
Arbeitsauftrag: [Cloud-Uebergabe](docs/about/headscale-cloud-task.md).
Priorisierte Umsetzung aus Abschnitt 15 mit getrennten Commitpaketen,
gezielten Tests und Erhalt aller Meshora-Konto-/Zonenregeln. Kein
automatischer Merge, Release oder produktives Deployment.

Die lokale Codex-CLI ist angemeldet, liefert aber keine Cloud-Aufgaben
und im Umgebungswaehler keine eingerichtete Projektumgebung. Erforderliche
Meshora-Umgebungs-ID beim Nutzer angefragt. Kein Cloud-Task wurde erzeugt.
Uebergabebranch: codex/headscale-sync-2026-11-08. Nur Plan/Arbeitsauftrag;
lokale unfertige Exit-Compose-Aenderungen sind nicht Bestandteil der Uebergabe.


### Lokale Umsetzung: Sicherheitsgrundlage (8. November 2026)

Branch: codex/headscale-sync-2026-11-08. Noch kein Merge oder Release.

- Implementiert: E-Mail/Verifikationsflag als Paar (208043c8), Metrics aus
  Noise entfernt (cd66e241), begrenzte Legacy-Hash-Wartezeit und erneutes
  Laden nach Prüfung (41067c7e), unveränderte OIDC-Subjects (499b58e7).
  Meshora-Anpassung: Ablehnung zentral in ResolveOIDCUser und zusätzlich
  beim Annehmen von OIDC-Einladungen, ohne Identifier umzubenennen.
- Implementiert: Maschinenkennung vor wartender Folgeanfrage prüfen
  (7892a846), IPs nur nach tatsächlicher Löschung freigeben (b5bebf9b),
  clientbestimmte Logfelder begrenzen (21c91d4b).
- Implementiert: Hostinfo/Endpoints/Routen begrenzen, gespeicherte Altdaten
  vor NodeStore-Laden bereinigen, Body-/Idle-Timeouts im Noise-Tunnel
  (2e563635, 4ce92841, 28959cb2); unbenutzte Registrierungssperren
  aufräumen (c9e9f728).
- Nachweise bisher: gezielte Sicherheits-, Grenzwert-, Timeout- und
  Löschtests grün. Erster vollständiger Lauf für hscontrol, types, db,
  state und webui grün, vor den später ergänzten Grenzen/Sperren.
  Race-Prüfung aller oben genannten gezielten Sicherheits-/Grenzwerttests
  einschließlich konkurrierender Registrierungssperren ebenfalls grün.
- Noch offen: übrige Registrierungs-/NodeKey-/Ablauffixes und API-Rechte,
  Policy-/Migrationspaket, DERP/Leistung/Kompatibilität aus Abschnitt 15.
  Kein echter Exit-Paketfluss und kein produktiver Login damit abgenommen.

- Weiteres Registrierungs-/Wartepaket 5a4d366c/b833be28 nicht übernommen:
  wiederholter Kontextfehler in hscontrol/state/state.go:2097.
  Unvollständige Teiländerungen zurückgenommen; keine abgeschwächten Tests.
  Nächster Schritt ist eine fachliche Anpassung der Ablauf-/Metadatenlogik,
  kein weiterer Patchversuch mit veränderten Kontextoptionen.

- Abschließende Prüfung des bereinigten Blocks: vollständige Pakettests
  `go test -timeout 8m ./hscontrol ./hscontrol/types ./hscontrol/db ./hscontrol/state ./hscontrol/webui`
  grün (Serverpaket 184,756 s); gezielte Race-Tests grün.
  OIDC-Einladungen zusätzlich mit echtem PostgreSQL 17 im isolierten
  Container und SQLite bestanden. `go build ./cmd/headscale`,
  Diff-Lint über hscontrol (0 neue Meldungen) und `git diff --check` grün.
- OIDC-/Credentials-Commit: b5376173. Zweiter lokaler Commit enthält
  Noise-/Clientgrenzen, Logbegrenzung und Registrierungssperren.
  Keine Schema-/Moduländerungen, kein Push, Merge oder Release.

| Paket | Stand | Nachweis / verbleibender Umfang |
| --- | --- | --- |
| A: kleine Sicherheitsfixes | angepasst implementiert | b5376173; Metrics-Entfernung im zweiten Commit; Paket-/Race- und Einladungstests grün; separate Live-Noise-Metrics-Abnahme noch offen |
| B: Registrierung / Rechte | teilweise implementiert | Größen-/Zeit-/Loggrenzen, Maschinenprüfung, Löschung und Sperren getestet; Metadaten-/Wartepaket nach wiederholtem Kontextfehler zurückgenommen; übrige Identitäts-/API-Fixes offen |
| C: Policy / Datenbank | offen | keine Übernahme oder Migrationsabnahme in diesem Block |
| D: Stabilität / Leistung | offen | keine Übernahme oder Meshora-Benchmarks in diesem Block |
| E: optionale Funktionen | Vorschläge | weiterhin getrennte Bewertung und Voraussetzungen gemäß Abschnitt 15 |

Die Gesamtliste ist damit ausdrücklich noch nicht abgeschlossen.


## 17. Fortsetzung des Upstream-Abgleichs (9. November 2026)

**Status:** Lokale Umsetzung auf codex/headscale-sync-2026-11-08 läuft.
Der frühere Kontextfehler aus Abschnitt 16 ist fachlich aufgelöst: Die
abweichenden Kommentare zur Ablaufzeit werden nicht ersetzt; Meshoras
vorhandene Ablaufzweige bleiben bei der Metadaten-Anpassung erhalten.

- 5a4d366c angepasst: Pending-Anmeldungen behalten nur Hostname, OS und
  eine eigene Kopie der Tags. Bei erneuter Anmeldung bleiben Betriebsdaten,
  Dienste, Routen und Discovery-Key erhalten; zu große zusammengeführte
  Daten werden ohne Mutation abgelehnt.
- b833be28: Pro Anmeldung/SSH-Check wartet eine Folgeanfrage. Neuere
  Anfragen lösen ältere ab; Folge-URLs sind begrenzt und nicht benötigte
  Request-Daten werden vor dem Warten freigegeben.
- 221a387b: MapRequests prüfen den aktuellen NodeKey im NodeStore-Writer.
  Veraltete wartende Anfragen dürfen weder Schlüssel noch Gerätezustand
  überschreiben. Nur Registrierung darf den NodeKey wechseln.
- e67ef861: Administrative Ablaufzeiten getaggter Geräte erfordern bei
  erneuter Anmeldung einen gültigen Schlüssel; verbrauchte Schlüssel
  heben den Ablauf nicht mehr auf.
- bd74ce67 angepasst: Registrierung und Ablaufänderungen nutzen die
  gemeinsame Persistenzsperre und speichern die aktuelle NodeStore-Zeile.
  Rollback nimmt nur noch eigene unveränderte Felder zurück und erhält
  spätere Admin-/Map-Änderungen.
- d67ee0bd angepasst: Feste Anzahl von Schlüsselsperren serialisiert
  parallele Erstnutzung eines Einmalschlüssels. Schlüssel nach Warten erneut
  laden; höchstens ein Gerät erhält die Rechte, auch vorübergehend.
  Der zuerst fehlgeschlagene Regressionstest deckte die noch fehlende
  bd74ce67-Persistenzabhängigkeit auf; nach deren Anpassung grün.
- Nachweise: vollständige Tests für hscontrol/state/types/db/webui grün
  (Serverpaket 188,794 s); gezielte Race-Tests für Metadaten, Folgeanfragen,
  Schlüsselwechsel, Einmalschlüssel, Persistenz und Rollback grün.
  Diff-Lint 0 neue Meldungen; Build und diff --check grün.
- Zusätzlich gefunden: Terraform-Provider 0.29.3 benötigt ACL-Validierung.
  a63ee3e0 plus a336e1e7 ergänzt /acl/validate und strukturierte
  Policy-Testfehler, ohne eine Policy zu speichern. Endpunkttests und
  zuvor fehlgeschlagene echte OpenTofu-Provider-Fälle grün (22,327 s).
  Diese Commits ändern keine Proto-/Generatorquellen; entgegen dem alten
  Übergabetext ist für dieses konkrete Paket keine Generierung nötig.
- Der breite servertest-Gesamtlauf erreichte nach 8 Minuten sein Limit;
  dadurch keine vollständige Gesamtabnahme. Die darin tatsächlich
  gemeldeten ACL-Provider-Fehler wurden gezielt behoben und nachgeprüft.
  Restliche Registrierungs-/API-/Policy-/DB-/DERP-Pakete bleiben offen.
- Kein Push, Main-Merge, Release oder produktiver Konfigurationseingriff.
