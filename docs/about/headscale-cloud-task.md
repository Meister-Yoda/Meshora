# Cloud-Auftrag: Headscale-Fixes sorgfaeltig in Meshora integrieren

## Auftrag und Basis

Der Nutzer hat beauftragt, die Upstream-Liste in ROADMAP.md Abschnitt 15
sorgfaeltig in der Codex Cloud abzuarbeiten. Implementierung ist autorisiert.
Arbeite auf dem dedizierten Branch codex/headscale-sync-2026-11-08.
Kein automatisches Merge nach main, kein Release, Tag oder Deployment.
Ergebnis soll als pruefbarer Diff mit kleinen, fachlich getrennten Commits
und Testnachweisen vorliegen. Keine Nachrichten an Dritte versenden.

Meshora-Ausgangscode: 44e87797.
Gepruefter Headscale-Stand:
48416ce0894ef7bef0ef9c064609b4aa76a0d9b6 (6. November 2026).
Letzter frueherer Reviewstand: 2d220b2e.
360 Upstream-Commits liegen dazwischen, einschliesslich Testdaten und
generierter Dateien. Das ist keine Aufforderung zum unkontrollierten Merge.
Originalpatches und Abhaengigkeiten lesen; neuere Fixes in der Kette
beruecksichtigen. Roadmap Abschnitt 15 ist die detaillierte Arbeitsliste.

AGENTS.md befolgen. Vor Tests cmd/hi/README.md und integration/README.md
vollstaendig lesen. Vor Aenderungen an mehr als drei Dateien den konkreten
Scope anzeigen. Bei zwei gleichen Fehlern stoppen, Ursache und offenen
Stand berichten, keine Testbehauptungen erfinden.

## Unbedingt erhalten

- Meshoras WebUI, gemeinsame Konten, unveraenderliche UID, Login-E-Mail-
  Eindeutigkeit, lokale Passwoerter, OIDC-Verknuepfung, Passkeys, TOTP,
  Wiederherstellungscodes, Sitzungsrevision und Sicherheitsrichtlinien.
- Einladungen fuer lokale und OIDC-Konten, einmalige gehashte Links,
  Ablauf und Widerruf, SMTP/JMAP samt Testmail und Sitzungsverwaltung.
- Verbindliche Zonen-/Benutzer-/Geraetesperren, getrennte Behandlung von
  Ankuendigung, Freigabe, Sichtbarkeit und tatsaechlichem Paketfluss.
- Tailscale SSH bleibt standardmaessig aus. Keine SSH-Freigabe durch
  Uebernahme von Capabilities oder veraenderte Policy-Defaults.
- Vorhandene Meshora-Migrationen und Daten. Neue DDL-Migrationen nur hinten
  anhaengen, keine FK-Deaktivierung, kein AutoMigrate in Migrationen.
- View-basierte Leserouten und vorhandene PrimaryRoutesForNode-Optimierung.
- go.mod/go.sum nicht komplett durch Upstream ersetzen. WebAuthn/OTP und
  aktuelle Moby-APIs erhalten. Bei Abhaengigkeiten gekoppelte Pins und
  Nix-Go-Version beachten; vendorhash nach jeder Modulaenderung aktualisieren.
- Keine Produktionszugangsdaten verwenden, keine echten Mails verschicken,
  mesh.langsebastian.eu nicht umkonfigurieren.

Die separate Exit-Compose-Datei war ein unvollstaendiger lokaler Versuch,
keine freigegebene Produktloesung. Der gewuenschte integrierte Exit in
Abschnitt 14 ist ein eigenstaendiges Vorhaben und wird durch dieses
Upstream-Paket nicht nebenbei gebaut. Der Paketfluss war zweimal erfolglos.
Kein Filterfix gilt deshalb automatisch als Beweis fuer funktionierendes
Exit-Routing.

## Arbeitspakete in Reihenfolge

### A. Kleine Sicherheitsfixes zuerst

1. 208043c8: OIDC-E-Mail und Verifikationsflag als Paar zusammenfuehren.
   Die getrennten cmp.Or-Ausdruecke in hscontrol/oidc.go sind noch vorhanden.
   Tests fuer wechselnde, leere und unbestaetigte UserInfo-Adressen.
   Gemeinsame ResolveOIDCUser-Verknuepfung, WebUI und Einladungen absichern.
2. cd66e241: /metrics aus dem Noise-Router entfernen; dedizierten Listener
   erhalten und dessen Erreichbarkeit getrennt testen.
3. 41067c7e: Wartezeit fuer Legacy-Hash-Pruefung begrenzen und Credential
   nach Pruefung erneut laden; parallelen Widerruf/Loeschung testen.
4. 499b58e7: OIDC-Subjects ablehnen, deren gespeicherter Identifier den
   Subject veraendern wuerde. Gilt auch fuer WebUI und OIDC-Einladungen.
   Keine pauschale Umbenennung vorhandener Provider-Identifier.

### B. Registrierung, Ablauf und Rechte

- 2e563635, 4ce92841, 5a4d366c, 28959cb2, 7892a846, b833be28,
  c9e9f728, c0925962, 15a8c758, e3c1bfd6, 21c91d4b:
  Begrenzte Hostinfo/Endpoints/Routen/Logs, sichere Ablaufzeiten,
  Body-/Idle-Timeouts, authentisierter Follow-up und bounded Pending-Login.
- e67ef861, e1de83cb, d67ee0bd, b1ab98bd, 221a387b, b5bebf9b,
  bd74ce67, 7721d8c2, 0ecbeafb, e8461504, ba319ba8:
  Registrierungs-/Eigentums-/NodeKey-/Loeschrennen, Einmalschluessel,
  IP-Freigabe, abgelaufene/getaggte/ephemere Geraete und SSH-Check-Entzug.
- 0bf5b0f0, 408b9b77, 77005fac, 4e946edb, 9979be14:
  API-Bodies und Fehlermeldungen begrenzen/redigieren, OAuth-Scopes,
  Tags und Tokenlimits durchsetzen.
- 14baf554: Benutzerupdates serialisieren und NodeStore/Policy/Clients
  konsistent erneuern. Meshoras EnsureUserAccount, Kontorevision und
  Login-E-Mail zwingend erhalten. OIDC-E-Mail-Loeschungen mit der
  gemeinsamen Konto- und Verknuepfungslogik abstimmen.

### C. Exit-/Zonen-Policy und Datenbank

- 256a67e8, 6a1f8d15, fc107292, 4342883b, e0717bc1, 672aa401:
  Exit-Routen nur mit Internetberechtigung, mehrere via-Grants,
  deterministisches HA/Failover und zusammenhaengende Map-Updates.
- 9f88e7dd, d2a85b1d, 49030541, 99b2d949:
  fehlende Regeln, explizit leere Regeln und Zonen-Policies unterscheiden.
  Keine Zonensperre durch implizite Freigabe gleicher Benutzer aufheben.
- bdfe6fc3, 6876bcfa, 3e9195e4, 6fe62dec:
  automatische gegen manuelle Routenfreigaben unterscheiden. Schema,
  Meshora-UI und Widerruf gemeinsam anpassen.
- 059b95ba und aa8d73ea: finale Ablehnung zu alter Datenbanken;
  Zwischenschritt 3bb5789d nicht isoliert uebernehmen. Unveraendertes
  SQLite-Dump bei Ablehnung und frischen abgebrochenen Start testen.
- 92bc0b62, 1a32d031, f32fc9f1, 59f9f78b, e99ad440,
  060f145a, 5adc504f:
  nullable Schluesselablauf, Ephemeral-Provenienz und API-Felder mit
  passenden append-only Migrationen. Keine komplette Schema-Uebernahme.
  Kontrolladresse nur mit korrekter Trusted-Proxy-Behandlung.

### D. Stabilitaet, Leistung und kleine Kompatibilitaet

- a25aff2c, 017ab6b2, 6f2f97ca, 581ec5b3: DERP-Fehlerbehandlung,
  letzte gueltige Map, HomeParams-Panic und stabile Reihenfolge.
- 137a66d5, dea3af08, 4b2d227f, 91b920cc, 800392ae, 526daf08:
  Matcher/Filter wiederverwenden; echten Meshora-Zonenworkload messen.
- 34653a1b: SQLite-Prepared-Statement-Cache und Transaktionen.
- f37c6a69, a886c717, 91168cc4, c2b4c97c, 2d094f22:
  begrenzte Mapstreams, Workerwartezeit, korrekte Initialmaps,
  keine redundanten Frames.
- d0e10f2d: HuJSON fuer .json-Dateien.
- dc9513f4, 4a531ec7, 48416ce0, 9a199a7e: capver-Tool robust abrufen.
- a63ee3e0: ACL-Validierungsendpunkt und Generatorausgaben.
  Generierte Dateien durch dokumentierte Generierung aktualisieren,
  getrennt committen; nie gen/ handeditieren.

### E. Optionale Features getrennt bewerten

Remote-Clientupdates (672340c0, 1386772d, c8e5aa66, 8230c58a,
7af29e8c, f9992637, fda2a123, ac25916b), TLS-Reload b3ffec42,
ACME-Retry 8bc7e793 und DNS-01/CertMagic sind eigene Pakete.
Vor Umsetzung Voraussetzungen und Meshora-Integration konkret dokumentieren.
Eine fehlende DNS-/Providerentscheidung nicht mit einer willkuerlichen
Produktionskonfiguration uebergehen. Ergebnis klar als Vorschlag oder
implementiert/geprueft markieren, kein optionales Feature still aktivieren.

## Abnahme und Abschluss

- Gezielt aussagekraeftige Fehler- und Konkurrenztests pro Paket.
- Fuer Kontoaenderungen lokale und OIDC-Anmeldung einschliesslich
  bestehender Faktoren, Einladungen, Sitzungswiderruf und UID testen.
- Zonen-Tests fuer erlaubte/verbotene Quellen, Benutzer-/Geraeteausnahmen,
  IPv4/IPv6, via-Ueberlappung, HA, manuell/automatisch und Widerruf.
- SQLite und PostgreSQL fuer Migrationen und Konkurrenzfaelle.
- Geeignete Pakettests und Race-Tests, Build, Lint des Diffs, diff --check.
  Bekannte Voll-Lint-Baseline nicht als neue Regression ausgeben und
  bestehende Fehler nicht durch abgeschwaechte Tests verdecken.
- Netzwerk-/Exit-Abnahme benoetigt echten Clientverkehr. Fehlen der Cloud
  TUN/Capabilities/Docker, Teiltests trotzdem abschliessen und diese
  Abnahme ausdruecklich offen lassen.
- ROADMAP.md bei jedem Paket aktualisieren: Commitquellen, Anpassungen,
  tatsaechlich ausgefuehrte Tests und offene Punkte.
- Abschliessend Tabelle fuer jedes Paket: uebernommen/angepasst/bereits
  vorhanden/nicht anwendbar/blockiert, Commit-SHA, Testbeleg, Restrisiko.
  Keine vollstaendige Erledigung melden, solange Pflichtpunkte offen sind.
