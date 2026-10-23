# Meshora starten und im Browser einrichten

## Neu in v0.1.0-rc.5

- Deutsch/English-Umschalter mit gespeicherter Sprachwahl.
- Automatischer stiller Authentik-Login für Admins mit Rückfall auf das lokale Formular.
- Admin-Zugriff an signierte Rollen und/oder Gruppen binden.
- OIDC-Button im Admin-Panel frei beschriften; Änderungen gelten sofort.
- Neues Meshora-Logo, Dunkelmodus-Variante und Symbol-Favicon.

Die Einrichtung von Claims und Scopes beschreibt die
[Admin-OIDC-Anleitung](https://github.com/Meister-Yoda/Meshora/blob/v0.1.0-rc.5/README.de.md#admin-anmeldung-mit-authentik).
Bei OIDC-Problemen kann der lokale Admin-Login immer direkt unter
`/admin/login?local=1` geöffnet werden.

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
docker load -i meshora_v0.1.0-rc.5_linux_amd64.docker.tar.gz
```

Für das lokal geladene Image `MESHORA_IMAGE=meshora` setzen.

```bash
docker build -f Dockerfile.meshora --build-arg VERSION=dev -t meshora:dev .
```

Beim Selbstbau `MESHORA_IMAGE=meshora` und `MESHORA_VERSION=dev` verwenden.
