# Running the service via TLS (optional)

## Bring your own certificate

Headscale can be configured to expose its web service via TLS. To configure the certificate and key file manually, set the `tls_cert_path` and `tls_key_path` configuration parameters. If the path is relative, it will be interpreted as relative to the directory the configuration file was read from.

```yaml title="config.yaml"
tls_cert_path: ""
tls_key_path: ""
```

The certificate should contain the full chain, else some clients, like the Tailscale Android client, will reject it. Replace certificate and key together, then send the server SIGHUP. A malformed, mismatched, expired or not-yet-valid replacement keeps the previous certificate serving. Canonical keys are `tls.cert_path` and `tls.key_path`; legacy keys remain supported.

## Let's Encrypt / ACME

To get a certificate automatically via [Let's Encrypt](https://letsencrypt.org/), set `tls_letsencrypt_hostname` to the desired certificate hostname. This name must resolve to the IP address(es) headscale is reachable on (i.e., it must correspond to the `server_url` configuration parameter). The certificate and Let's Encrypt account credentials will be stored in the directory configured in `tls_letsencrypt_cache_dir`. If the path is relative, it will be interpreted as relative to the directory the configuration file was read from.

```yaml title="config.yaml"
tls_letsencrypt_hostname: ""
tls_letsencrypt_listen: ":http"
tls_letsencrypt_cache_dir: ".cache"
tls_letsencrypt_challenge_type: HTTP-01
```

### Challenge types

Supported challenge types are `HTTP-01` (default), `TLS-ALPN-01`, and `DNS-01`. Canonical keys under `tls.acme` take precedence over legacy keys.

#### HTTP-01

For `HTTP-01`, headscale must be reachable on port 80 for the Let's Encrypt automated validation, in addition to whatever port is configured in `listen_addr`. By default, headscale listens on port 80 on all local IPs for Let's Encrypt automated validation.

!!! warning "`listen_addr` cannot also be port 80"

    `listen_addr` and `tls_letsencrypt_listen` must not bind the same TCP socket. A common mistake is setting `listen_addr: 0.0.0.0:80` together with `tls_letsencrypt_hostname` — both endpoints try to claim port 80, and the second bind fails with `address already in use` even though no other process is involved. Headscale validates this at startup and refuses to launch with a `Fatal config error: listen_addr and tls_letsencrypt_listen would bind the same TCP socket` message. Use `listen_addr: 0.0.0.0:443` (or any non-80 port) when HTTP-01 is enabled.

If you need to change the ip and/or port used by headscale for the Let's Encrypt validation process, set `tls_letsencrypt_listen` to the appropriate value. This can be handy if you are running headscale as a non-root user (or can't run `setcap`). Keep in mind, however, that Let's Encrypt will _only_ connect to port 80 for the validation callback, so if you change `tls_letsencrypt_listen` you will also need to configure something else (e.g. a firewall rule) to forward the traffic from port 80 to the ip:port combination specified in `tls_letsencrypt_listen`.

#### TLS-ALPN-01

For `TLS-ALPN-01`, headscale listens on the ip:port combination defined in `listen_addr`. Let's Encrypt will _only_ connect to port 443 for the validation callback, so if `listen_addr` is not set to port 443, something else (e.g. a firewall rule) will be required to forward the traffic from port 443 to the ip:port combination specified in `listen_addr`.

#### DNS-01

DNS-01 uses the configured `dns.challenge.provider` to publish TXT records.
It needs DNS provider credentials and a domain you control, and does not need
an inbound port 80 challenge listener. This can be enabled for the server
without enabling device certificates. See the working configuration below.

### Technical description

CertMagic manages server ACME issuance, persistent certificate/account storage
and background renewal. Renewal follows the certificate lifetime and CA renewal
information; the old autocert timing rules no longer apply. Retries stay on
the configured `tls.acme.url`. The server starts management after binding its
listeners and cancels management at shutdown.

Persist and back up the configured cache directory. CertMagic stores its assets
in its own subdirectories and does not import or remove old autocert files.
The first start after the change obtains a new account/certificate, so plan for
the configured CA's issuance limits. Test a CA change with a separate cache.

#### Checking certificate expiry

If you want to validate that certificate renewal completed successfully, this can be done either manually, or through external monitoring software. Two examples of doing this manually:

1. Open the URL for your headscale server in your browser of choice, and manually inspecting the expiry date of the certificate you receive.
1. Or, check remotely from CLI using `openssl`:

```console
$ openssl s_client -servername [hostname] -connect [hostname]:443 | openssl x509 -noout -dates
(...)
notBefore=Feb  8 09:48:26 2024 GMT
notAfter=May  8 09:48:25 2024 GMT
```

#### ACME logs

ACME activity is routed through the server's structured logger with logger
name `acme`, using the configured log level and format. Monitor renewal
failures and the certificate actually served to clients.

## Geplante Erweiterung: Knotenzertifikate, DNS-01 und CertMagic

Stand: 11. November 2026. Schritte 1 bis 3 sind lokal implementiert;
die echte Anbieter-/Client-Abnahme und Releasepruefungen bleiben offen.
Die folgenden Planabschnitte dokumentieren Umfang und Abnahmekriterien.

### Ziel und Betriebsarten

Zwei getrennte Zertifikatswege werden unterstuetzt:

- **Meshora-Server:** HTTPS weiterhin am Reverse Proxy, mit statischem
  Zertifikat oder mit eigenem ACME-Client. CertMagic ersetzt fuer diesen
  letzten Weg autocert und ergaenzt DNS-01 neben HTTP-01/TLS-ALPN-01.
- **Mesh-Geraete:** `tailscale cert` und HTTPS mit `tailscale serve` erhalten
  die noetige CertDomains-/HTTPS-Ankuendigung. Der Client erzeugt seinen
  privaten Schluessel und fordert das Zertifikat an; Meshora veroeffentlicht
  ausschliesslich die autorisierte DNS-Challenge. DNS-API-Geheimnisse und
  private Geraeteschluessel werden nicht aneinander weitergereicht.

HTTPS-Terminierung durch Coolify/Traefik bleibt moeglich, waehrend Meshora
intern auf Container-Port 8080 HTTP bedient. Geraetezertifikate benoetigen
keinen zweiten HTTPS-Listener am Controlserver. Zonenfreigaben bleiben fuer
Serve-Verkehr massgeblich; ein Zertifikat erteilt keine Netzwerkberechtigung.
Funnel und eine oeffentliche Freigabe interner Dienste sind kein Bestandteil.

### Voraussetzungen und noch offene Betriebsdaten

Fuer eine echte DNS-01-Abnahme fehlen die Auswahl einer oeffentlichen Domain
unter eigener Kontrolle, des DNS-Anbieters und eines begrenzten Testzugangs.
Diese Angaben sind erst fuer die externe Abnahme erforderlich; Entwicklung
und lokale Protokolltests koennen mit isolierten DNS-/ACME-Testdiensten laufen.
Bestehendes `meshora.internal` ist keine Grundlage fuer oeffentliche Zertifikate.
Vorhandene Geraetenamen oder die produktive Basisdomain werden nicht automatisch
umbenannt. Vor einem Domainwechsel sind DNS, OIDC, WebAuthn-RP-ID/Origin und
bestehende Passkeys getrennt auf Auswirkungen zu pruefen.

Das aktuelle Upstream-Register bietet `acmedns`, `cloudflare`, `gigahost`,
`hetzner`, `powerdns` und `rfc2136`. Erster externer Abnahmefall ist genau ein
passender Anbieter, keine Behauptung realer Abnahme aller Anbieter.
DNS-API-Rechte auf die erforderliche Zone begrenzen; Delegation der
ACME-Challenge-Zone ist eine moegliche Betriebsvariante. Oeffentliche
Zertifikate koennen Geraetenamen in Certificate-Transparency-Logs offenlegen.

### Zusammenhaengender Integrationsumfang

Pruefbasis ist Headscale `7989fed78e8ac3c3ea4f12d8a2a423335a2f450f`;
Upstream `main` war am 11. November beim erneuten Abruf unveraendert.
Die Originalcommits dienen zur Zuordnung. Implementiert wird die gepruefte
Endfassung dieses Stands, nicht eine Mischung frueher Zwischenstaende.

| Arbeitspaket                              | Upstream-Anker                        | Meshora-Anpassung                                                                                                      |
| ----------------------------------------- | ------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| DNS-Anbieter und Challenge-Verwaltung     | `24cf63294`, `6cb950cd9`, `eef0d1fe4` | Provider-Register, Geheimnisdateien, autoritative Propagationspruefung, begrenzte Anfragen und TXT-Bereinigung         |
| Geraetekonfiguration und Zertifikatsnamen | `0efe4b779`, `3f6e54cbd`, `f16d5e185` | Config, NodeView-Namenspruefung, Mapper-CertDomains/HTTPS und authentifizierter Noise-Endpunkt                         |
| TLS-Konfiguration                         | `dcc01dc2b`                           | Neuer tls-Block mit Kompatibilitaet fuer bisherige flache Schluessel und Umgebungsvariablen                            |
| Server-ACME                               | `5d15cc930`, `8bc7e793a`              | CertMagic-Lifecycle, Fehlerlogging, dauerhafter Speicher; Retries bleiben bei der konfigurierten CA                    |
| Manuelle Zertifikate nachladen            | `b3ffec426`                           | Bestehendes SIGHUP-Verhalten fuer Policy erhalten; fehlerhaftes Ersatzpaar darf das gueltige Zertifikat nicht ersetzen |

Betroffen sind insbesondere `hscontrol/dns/`, `types/config.go`,
`types/node.go`, `mapper/mapper.go`, `noise.go`, `app.go`, das TLS-Logging,
vorhandene Tests, Beispielkonfiguration und TLS-/DNS-Dokumentation.
Neue lokale Konfigurationsfelder werden erst mit ihrer Implementierung
beworben. Ein NixOS-Abgleich umfasst die neuen Optionen und den bestehenden
Testkit-Vertrag. Konto-, Mail-, Einladungs- und Zonenimplementierungen werden
nicht durch Upstream-Dateien ersetzt. Eine Datenbankmigration ist nach dem
geprueften Design nicht erforderlich.

Abhaengigkeiten gezielt ergaenzen: CertMagic und DNS-Provider samt ihren
transitiven Abhaengigkeiten. CertMagic und Provider muessen dieselbe libdns-
Hauptversion verwenden. Bestehende WebAuthn-/OTP-Abhaengigkeiten und gekoppelte
Tailscale/gVisor- sowie SQLite/libc-Pins erhalten. Bei jeder Aenderung von
go.mod/go.sum Vendorhash aktualisieren; Go-/Nix-Kompatibilitaet pruefen.
Keine neuen Proto-Typen im geprueften Basispaket erforderlich. Falls bei der
Umsetzung doch API-Schema-Aenderungen entstehen, gilt deren dokumentiertes
Generierungsverfahren separat.

### Konfiguration und Uebergang

Geplante kanonische Schluessel aus Upstream:

- `dns.certificates.enabled` (Standard aus) und optional `.zone`.
- `dns.challenge.provider.name` und anbieterspezifische Felder; String-
  Geheimnisse bevorzugt ueber das jeweilige `_path`-Feld.
- `dns.challenge.propagation_timeout` und `.resolvers`.
- `tls.cert_path`, `tls.key_path` sowie `tls.acme.hostname`, `.challenge_type`,
  `.http_listen`, `.url`, `.email` und `.cache_dir`.

Die bisherige flache Konfiguration (`tls_cert_path`, `tls_key_path`,
`tls_letsencrypt_*`, `acme_url`, `acme_email`) bleibt lesbar. Wie im geprueften
Upstream gilt: nichtleerer neuer Wert, sonst alter Wert, sonst Standard.
Ein leerer neuer Wert deaktiviert daher keinen noch gesetzten alten Wert;
Konflikte sind beim Configtest sichtbar zu machen. Auch HEADSCALE_TLS\_\*-Env-
Zuordnung und durch Env gesperrte Einstellungen muessen abgedeckt sein.
Statisches Zertifikat und Server-ACME bleiben gegenseitig ausgeschlossen.

Bestehende Installationen werden nicht automatisch umgeschrieben. Setup und
WebUI duerfen unbekannte YAML-Bloecke nicht verlieren. Im ersten Schritt ist
Zertifikatskonfiguration dateibasiert mit Configtest und Neustart; kein neuer
Provider-Editor und kein automatisches DNS-Schreiben beim Oeffnen einer Seite.
Eine spaetere Einstellungsseite bleibt ein eigenes Arbeitspaket.

CertMagic-Speicher und autocert-Cache haben getrennt zu pruefende Formate.
Kein Loeschen oder ungeprueftes Wiederverwenden alter Konten/Zertifikate.
Der Uebergang wird zuerst mit Test-CA, gesichertem alten Cache und isoliertem
neuen persistenten Speicher geprobt. Relative Pfade behalten ihre dokumentierte
Bedeutung. Container-Volumes, Schreibrechte und Restart-Verhalten pruefen.
`tls.acme.url` steuert Server-ACME; die vom Geraeteclient verwendete CA wird
separat verifiziert und nicht als automatisch mitumgestellt vorausgesetzt.

### Sicherheits- und Betriebsbedingungen

- Noise-MachineKey und NodeKey gemeinsam pruefen, abgelaufene/unbekannte Nodes
  ablehnen. Nur TXT fuer den exakt zulassigen eigenen `_acme-challenge`-Namen
  erlauben. Kein freier DNS-Editor, keine fremden Namen und keine Verwechslung
  von getaggter Identitaet mit dem erfassten Ersteller.
- Namenskollisionen, Gross-/Kleinschreibung, ungueltige/zu lange FQDNs und
  reservierte DNS-extra-records einschliesslich Unterbaeumen behandeln.
  Bei acme-dns pro Zertifikatsname isolierte Registrierung verlangen;
  eine gemeinsam genutzte Registrierung darf keine fremden Namen bestaetigen.
- Neueste Upstream-Grenzen uebernehmen: vier laufende Challenges je Node,
  begrenzte Provider-Aufrufe, duplikatbegrenzte Propagationspruefungen und
  Abbruch bei Shutdown. Umbenennen darf keine neuen Limitslots schaffen.
  Bestehende Noise-Body-/Idle-Limits muessen auch den neuen Handler schuetzen.
- TXT-Werte anhaengen und nur eigene Records entfernen; fremde oder parallel
  benoetigte Werte erhalten. Timeout, Abbruch und Shutdown testen. Ein Absturz
  kann TXT-Reste hinterlassen: DNS-TTL loescht sie nicht. Bereinigung darf nur
  eindeutig zuordenbare Werte betreffen; automatische Crash-Recovery ist ohne
  Nachweis keine zugesagte Funktion.
- Provider-Geheimnisse aus Config-Dumps, WebUI, Fehlern und Logs fernhalten;
  Cache-/Accountdateien nur fuer den Dienst lesbar. Ungueltige Provider-
  Konfiguration beim Start erkennen. Propagationspruefung bleibt standardmaessig
  aktiv, und Fehler werden nicht durch eine permissive Konfiguration verdeckt.
- Beim Abschalten werden keine neuen Challenges zugelassen. Bereits ausgestellte
  Zertifikate werden dadurch nicht automatisch widerrufen. Zugriffssperren
  laufen weiterhin ueber Geraete-/Zonenrechte. Namenswechsel und geloeschte
  Geraete veraendern die weiteren Ausstellungsrechte, nicht rueckwirkend ein
  bereits ausgestelltes Zertifikat.

### Umsetzung und Abnahme in Reihenfolge

1. **Konfiguration und Provider-Grundlage:** Lade-/Validierungslogik samt Legacy-
   und Env-Faellen, Provider und Geheimnisdateien integrieren. Config-Dumps auf
   Geheimnisse pruefen. Aktuelle Proxy-, statische TLS-, Setup- und Login-Tests
   muessen unveraendert funktionieren.
1. **Geraete-Challenges:** NodeView-Namen, Mapper und Noise-Endpunkt verbinden.
   Lokale DNS-Tests fuer Parallelitaet, Isolation, Limits, Fehler, Abbruch,
   Propagation und Bereinigung. HTTP-/Noise-Tests fuer fremde Keys/Namen,
   abgelaufene Nodes, Wiederholungen und ausgeschaltete Zertifikate.
1. **Server-TLS:** CertMagic integrieren, HTTP-01/TLS-ALPN-01/DNS-01 und
   Nachladen manueller Zertifikate mit lokaler ACME-CA pruefen. Erneuerung,
   Restart mit persistentem Speicher, Shutdown und fehlgeschlagene Erneuerung
   testen. CA-Wechsel bei Retries ausschliessen.
1. **Reale Clients und Anbieter:** Isolierte Domain und begrenzte DNS-Zugangsdaten
   festlegen. Ausstellung, nachgewiesene Erneuerung und HTTPS ueber Serve mit
   echten Clients testen; erlaubte und gesperrte Zonen pruefen. DNS-Records vor
   und nach Tests kontrollieren. Kein produktives DNS aus einem Test heraus
   ohne festgelegte Testzone veraendern.
1. **Releasebereitschaft:** Paket-/Race-Tests, bestehende Zonen-/Kontoabnahme,
   Build, Lint, Vendorhash sowie AMD64/ARM64-Container pruefen. Migration und
   Wiederherstellung dokumentieren. Lokale Test-CA und echte oeffentliche
   Ausstellung getrennt ausweisen; Veroeffentlichung im eigenen Releaseauftrag.

Die Planung ist abgeschlossen. Die erste Konfigurations-/Provider-Stufe ist unten
dokumentiert; weitere Umsetzung und externe Abnahme bleiben offen;
Provider-/Domainwahl ist kein Hindernis fuer die ersten drei lokalen Schritte.

### Quellen des Plans

- [Headscale-Pruefstand](https://github.com/juanfont/headscale/tree/7989fed78e8ac3c3ea4f12d8a2a423335a2f450f)
- [Tailscale: HTTPS-Zertifikate und Certificate Transparency](https://tailscale.com/docs/how-to/set-up-https-certificates)
- [Let's Encrypt: DNS-01, Delegation und parallele TXT-Werte](https://letsencrypt.org/docs/challenge-types/#dns-01-challenge)

## Umsetzungsstand: Konfigurations- und Provider-Grundlage

11. November 2026: Schritt 1 ist lokal implementiert und geprueft. Die folgenden
   Konfigurationsfelder sind jetzt lesbar, ohne DNS-Challenges automatisch anzulegen:

- `dns.challenge.provider.name`: acmedns, cloudflare, gigahost, hetzner,
  powerdns oder rfc2136. Felder folgen dem jeweiligen Provider; `_path` liest
  String-Geheimnisse aus Dateien. Env-Werte werden je Feld beruecksichtigt.
- `dns.challenge.propagation_timeout`: standardmaessig zwei Minuten;
  ungueltige und negative Zeitangaben werden abgelehnt.
- `dns.challenge.resolvers`: IP-Adressen mit optionalem Port; Standard
  `1.1.1.1:53` und `8.8.8.8:53`. Ungueltige Adressen oder Ports werden abgelehnt.
- Der oben geplante `tls`-Namensraum wird auf die bestehende TLS-Implementierung
  abgebildet. Nichtleere neue Werte haben Vorrang; bisherige flache Schluessel,
  relative TLS-Pfade und bisherige Defaults bleiben wirksam. HTTP-01 und
  TLS-ALPN-01 bleiben die derzeitigen Server-ACME-Verfahren.

Provider werden beim Laden konstruiert und lokal validiert. Dies prueft
Konfigurationsform, nicht die Gueltigkeit von Zugangsdaten beim Anbieter.
Leere Zugangsdaten, unbekannte Felder und Konflikte zwischen Wert und Datei
werden abgewiesen. Decoderfehler enthalten keine eingegebenen Geheimniswerte.
Der optionale Server-Debugdump nutzt jetzt JSON und beachtet geheime Felder;
das Providerobjekt ist von der Serialisierung ausgeschlossen.

Die uebernommene acme-dns-Schutzlogik verhindert das Verdraengen noch
benoetigter TXT-Werte; lokale HTTP-Tests und Tests der Registrierung pro Name
sind enthalten. Die Test-Serveruhr verwendet monotone verstrichene Zeit, damit
Systemzeitspruenge keine Records kuenstlich umsortieren.

**Historischer Zwischenstand von Schritt 1:** Die Aktivierung war bis zur
zusammenhaengenden Anbindung von Challenge-Verwaltung, Mapper und Noise-Endpunkt
gesperrt. Diese Zwischensperre ist mit dem unten dokumentierten Schritt 2
aufgehoben. Server-DNS-01 ist seit dem unten dokumentierten Schritt 3 integriert.

Validierung: vollstaendige DNS-/Konfigurationstests und Race-Checks bestanden;
Serverpaket 204,302 s, WebUI 74,933 s, Templates 0,024 s; finale DNS-/Types-
Race-Tests 4,395/1,225 s. Serverbuild, Modulpruefung, Vendorhash-Check und
Diff-Lint (0 Befunde) bestanden. Nix-Vendorhash aktualisiert; ein kompletter
Nix-Build und reale Anbieterzugriffe wurden nicht ausgefuehrt. Nachweise unter
`control_logs/roadmap-20261110/cert-*.log` (gitignored).

Naechster Schritt: Challenge-Verwaltung mit Propagationspruefung und Bereinigung,
NodeView-Namensschutz, Mapper-Ankuendigung und authentifiziertem Noise-Endpunkt
als zusammenhaengendes Paket integrieren. Aktivierung erst mit diesem Paket.
CertMagic-Erneuerung und externe DNS-/ACME-Abnahme folgen separat.

## Umsetzungsstand: Geraete-Challenges und Noise-Endpunkt

11. November 2026: Schritt 2 ist lokal implementiert und validiert. Die
   Geraeteprotokolle fuer `tailscale cert` und Serve-HTTPS sind verbunden;
   eine echte Zertifikatsausstellung bei einer oeffentlichen CA ist noch nicht
   abgenommen. Standardmaessig bleibt die Funktion ausgeschaltet.

### Aktivierung nach Bereitstellung der DNS-Voraussetzungen

Das folgende Beispiel zeigt die jetzt verfuegbare Konfiguration. Domain und
Zonenzugang sind Platzhalter und muessen zum eigenen Betrieb passen:

```yaml
dns:
  magic_dns: true
  base_domain: mesh.example.com
  certificates:
    enabled: true
    zone: example.com
  challenge:
    propagation_timeout: 2m
    provider:
      name: cloudflare
      api_token_path: /run/secrets/meshora_dns
```

Die Schreibzone muss die Basisdomain enthalten. Ohne explizite Zone wird die
Basisdomain verwendet; das passt nur, wenn der DNS-Anbieter diese Zone auch
verwaltet. Die Server-URL muss weiterhin von der MagicDNS-Basisdomain getrennt
sein. DNS-Geheimnisse werden wie bisher auf dem Server bereitgestellt; es gibt
keinen neuen Provider-Editor in der WebUI. Konfigurationsaenderungen benoetigen
einen Neustart. Dies aktiviert kein Server-DNS-01 und aendert nicht die
HTTPS-Terminierung am Reverse Proxy.

### Durchgesetzte Grenzen

- Das Geraet erhaelt nur seinen zulassigen MagicDNS-Zertifikatsnamen und die
  passende HTTPS-Faehigkeit. Namen mit ungueltiger Gross-/Kleinschreibung,
  ueberlangen Challenges oder Konflikten mit DNS-extra-records am Namen oder
  darunter werden nicht angeboten. acme-dns benoetigt eine isolierte
  Registrierung fuer jeden freigegebenen Namen.
- `/machine/set-dns` ist ausschliesslich im bestehenden Noise-Tunnel verbunden.
  NodeKey und MachineKey muessen dasselbe registrierte Geraet bezeichnen.
  Abgelaufene/unbekannte Geraete, fremde Namen, falsche Recordtypen und
  ungueltige Challengewerte erreichen den Provider nicht. Body-Limit und
  Ablehnung nachfolgender JSON-Objekte gelten auch fuer diesen Handler.
- Records werden angehaengt und einzeln entfernt. Vorhandene Betreiberwerte
  bleiben erhalten; Wiederholungen desselben Werts erzeugen keinen neuen
  Provider-Schreibvorgang. Vier laufende Challenges je Node und begrenzte
  Propagationsanfragen verhindern eine Umgehung durch Umbenennung oder
  parallele Wiederholungen.
- Der Handler wartet auf alle autoritativen Nameserver und folgt begrenzten
  CNAME-Ketten. Weitere Werte an demselben Namen beruecksichtigen den
  DNS-Cache-Zeitraum. Eine Propagationszeit von 0 deaktiviert diese Pruefung
  ausdruecklich und ist kein Nachweis oeffentlicher DNS-Sichtbarkeit.
- Request-Abbruch und Lebensdauer laufen getrennt von einer schon begonnenen
  Provider-Schreiboperation, damit erfolgreiche Writes verfolgt und spaeter
  bereinigt werden. Provider-Aufrufe sind begrenzt. Beim normalen Shutdown
  werden Propagationspruefungen abgebrochen, eigene Werte geloescht und weitere
  Anfragen abgewiesen. Ein Prozessabsturz oder ein fehlgeschlagener Delete kann
  weiterhin TXT-Reste hinterlassen; eine DNS-TTL loescht diese nicht.
- Die v2-Einstellung `httpsEnabled` meldet jetzt die Geraetezertifikatsfunktion,
  statt aus dem Serverzertifikat abgeleitet zu werden. Sie bestaetigt weder
  die Erreichbarkeit des Providers noch eine erfolgreiche Ausstellung.

### Nachweise und naechster Schritt

Lokale UDP-DNS-Tests pruefen mehrere autoritative Nameserver, verspaetete
Records und mehrstufige CNAME-Delegation. Challenge-Tests pruefen TTL-/Lebensdauer,
Parallelitaet, Limits, Abbruch, Record-Isolation und Shutdown. Mapper-/HTTP-Tests
pruefen Namensschutz und Ablehnung unberechtigter Requests.

Ein echter Tailscale-Controlclient prueft den SetDNS-Aufruf ueber Noise,
CertDomains/HTTPS-Faehigkeit und den Namenswechsel nach Umbenennung. Ein zweiter
Client kann weder einen fremden Namen noch einen fremden NodeKey auf seinem
Tunnel verwenden. Dies ist ein Control-Protokolltest mit lokalem Provider,
keine oeffentliche ACME-Ausstellung und kein Browser-HTTPS-Nachweis.

Vollstaendige Tests: hscontrol 203,611 s, webui 76,812 s, templates 0,028 s,
types 0,081 s, mapper 63,570 s. Race-Tests: dns 4,969 s, types 1,255 s,
mapper 64,516 s; Endpunkt-/API-Pruefungen 3,900 s und echter Noise-Client
5,831 s. Build, Modulpruefung, Vendorhash und Diff-Lint (0 Befunde) bestanden.
Protokolle: `control_logs/roadmap-20261110/cert-challenges-*.log` (gitignored).

Als Naechstes folgt CertMagic fuer Server-TLS mit lokaler ACME-CA, Erneuerung,
Cache-Uebergang und manuellem Zertifikats-Reload. Die echte Anbieter-/CA-Abnahme
bleibt ein eigener Schritt mit festgelegter Testdomain und begrenzten Credentials.
Kein produktiver DNS-Eingriff, Commit, Push oder Release in diesem Arbeitspaket.

## Umsetzungsstand: Server-TLS mit CertMagic

Schritt 3 ist lokal implementiert. Serverzertifikate werden mit CertMagic
bezogen, gespeichert und automatisch zur Erneuerung verwaltet. HTTP-01 und
TLS-ALPN-01 bleiben unterstuetzt; DNS-01 nutzt denselben Provider und dieselbe
Propagationspruefung wie die Geraete-Challenges. Es aktiviert keine
Geraetezertifikate und verlangt fuer Server-TLS allein kein MagicDNS.

### Beispiel fuer eigenes Server-DNS-01

```yaml
server_url: https://headscale.example.com
listen_addr: 0.0.0.0:443
tls:
  acme:
    hostname: headscale.example.com
    challenge_type: DNS-01
    cache_dir: /var/lib/headscale/cache
    url: https://acme-v02.api.letsencrypt.org/directory
    email: admin@example.com
dns:
  challenge:
    propagation_timeout: 2m
    provider:
      name: cloudflare
      api_token_path: /run/secrets/meshora_dns
```

Dies ist ein Konfigurationsbeispiel fuer eine eigene, oeffentliche Domain.
Bestehende Reverse-Proxy-Installationen koennen weiter HTTP intern auf Port
8080 verwenden; sie muessen keinen zweiten ACME-Client aktivieren.
`meshora.internal` ist keine oeffentlich zertifizierbare Testdomain.
Alte TLS-Schluessel, relative Pfade und Env-Konfiguration bleiben lesbar.
Automatische und manuelle Serverzertifikate bleiben gegenseitig ausgeschlossen.

### Speicher, Erneuerung und Reload

- Den Cache persistent einbinden und mitsamt Accountdaten/privaten Schluesseln
  sichern. CertMagic schreibt eigene Unterverzeichnisse. Alte autocert-Dateien
  werden weder geloescht noch als neues Speicherformat interpretiert.
  Der erste Start stellt neu aus; CA-Limits vorher beruecksichtigen.
- Vor einem CA-Wechsel eine getrennte Testumgebung und einen getrennten Cache
  verwenden. Auch nach einem Fehler bleiben Wiederholungen bei der explizit
  konfigurierten CA; kein stiller Wechsel zu einer anderen CA.
- Zertifikatsverwaltung startet erst nach den Listenern. Ausstellung und Laden
  sind asynchron; ein frisch gestarteter Server kann daher zunaechst noch
  keinen erfolgreichen TLS-Handshake liefern. Shutdown bricht Verwaltungsjobs
  ab und stoppt die Wartung; mehrfacher Stop ist sicher.
- Bei manuellen Zertifikaten beide Dateien vollstaendig ersetzen und SIGHUP
  senden. Neue Verbindungen erhalten das neue Paar, auch ohne SNI. Fehlerhafte,
  unpassende oder zeitlich ungueltige Ersatzdateien belassen das bisherige Paar.
  Der Reload wird auch ohne konfigurierte ACL-Datei ausgefuehrt.

### Lokale Nachweise und verbleibende Abnahme

Die lokale Pebble-CA validiert HTTP-01, TLS-ALPN-01 und DNS-01 gegen echte
lokale Listener und einen lokalen DNS-Server. Die TLS-Clients pruefen die
ausgestellte Kette und den Hostnamen. Ein fehlgeschlagener Ausstellungslauf
prueft, dass Wiederholungen die konfigurierte CA nicht verlassen.

Zusaetzlich werden eine fehlgeschlagene und eine erfolgreiche erzwungene
Erneuerung, das Weiterbedienen des bisherigen Zertifikats, Laden des erneuerten
Zertifikats nach Neustart ohne erneuten DNS-Schreibvorgang sowie der Erhalt
einer alten autocert-Datei geprueft. Das ist ein lokaler Erneuerungsnachweis,
kein ueber Wochen beobachteter Wartungszyklus. Die Erneuerungsfixture deaktiviert
ARI wegen Pebbles Seriennummernnormalisierung bei gesetztem hoechstem Bit;
der produktive Server behaelt ARI aktiviert. Manuelle Reload-Tests decken
Dateifehler, falsche Schluessel, Zeitgrenzen und parallele Handshakes ab.

Die echte DNS-Anbieter-/CA-Abnahme und HTTPS mit `tailscale cert`/`tailscale serve`
bleiben Schritt 4. Dafuer fehlen weiterhin eine vereinbarte oeffentliche
Testdomain und begrenzte Anbieterzugangsdaten. Nix-Gesamtbuild,
Containerabnahme und Release folgen in Schritt 5.

Validierung: finale vollstaendige Serverregression 267,861 s; Types, Logger,
WebUI und Templates ebenfalls gruen. TLS-/Types-/Logger-Race-Tests
4,223/1,037/1,030 s; finale Erneuerungs-/Logout-Race-Pruefung 6,536 s,
echter Noise-Client 4,593 s. Build/Versionsstart, Module, Vendorhash und
Diff-Lint (0 Befunde) bestanden. Ein erster Gesamtlauf meldete neben dem
behobenen Pebble-Fixtureproblem zwei Logout-Ablaufassertionen; isolierte,
Race- und finale Gesamttests bestanden. Ursache der ersten Logoutfehler
nicht abschliessend belegt; Zeitdiagnosen fuer erneutes Auftreten ergaenzt.
Protokolle: `control_logs/roadmap-20261110/cert-server-*.log` (gitignored).
