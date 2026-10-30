# Meshora Roadmap

Stand: 27. Oktober 2026.

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
