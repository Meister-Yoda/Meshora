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

**Stand: 28. Oktober 2026.** Fuer v0.2.1-rc.4 vorbereitet; Veroeffentlichung und Release-CI werden geprueft. Fremde Arbeitskopie-Aenderungen bleiben erhalten.

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
Release-CI und GHCR-Verifikation folgen dem Tag-Push; latest bleibt unveraendert.
