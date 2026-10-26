# Meshora starten und im Browser einrichten

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

## Coolify und andere Reverse Proxies

Die normale Compose-Datei einfügen. Die eigene Domain auf den Dienst
`meshora` und dessen internen Port 8080 routen. Die Domain und DNS-Zuordnung
müssen vor dem Browserzugriff eingerichtet sein. Anschließend die
Ersteinrichtung über diese Domain öffnen und dieselbe URL in Meshora eingeben.

Die Datei enthält keine Coolify-spezifischen Felder. Die Einrichtung meldet
sich bereits während des Assistenten als gesund, damit der Proxy sie
erreichen kann.

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
