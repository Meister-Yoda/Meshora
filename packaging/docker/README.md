# Meshora mit Docker Compose

Benötigt werden Docker und Docker Compose v2 auf einem Linux-Server
(AMD64 oder ARM64). Das Image bringt die Grundkonfiguration bereits mit.
Beim ersten Start kopiert Docker sie automatisch in ein eigenes Volume.
Weitere Konfigurationsdateien oder Initialisierungscontainer sind nicht nötig.

## Erste Einrichtung

Die Datei [compose.yaml](compose.yaml) herunterladen oder ihren Inhalt
kopieren. Daneben eine `.env` mit der von den Clients erreichbaren URL anlegen:

```dotenv
MESHORA_SERVER_URL=http://192.168.1.100:8080
```

Die IP ist ein Beispiel und muss durch die Adresse des eigenen Servers
ersetzt werden. Bei einem Reverse Proxy stattdessen die externe HTTPS-URL
eintragen, beispielsweise `https://mesh.example.com`.

```bash
docker compose up -d
docker compose ps
docker compose exec meshora /usr/local/bin/headscale webadmins create admin --config /etc/meshora/config.yaml
```

Beim letzten Befehl das eigene Admin-Passwort setzen. Es gibt kein
vorgegebenes Standardpasswort. Danach `http://SERVER-IP:8080/admin/`
beziehungsweise `https://mesh.example.com/admin/` öffnen.

Jede Installation startet mit einer eigenen Datenbank und eigenen
Schlüsseln. Konfiguration und Daten bleiben in den Volumes
`meshora-config` und `meshora-data` bei Neustarts und Updates erhalten.
Vor Updates die Volumes bei gestopptem Meshora sichern. `docker compose down -v`
löscht diese Daten und ist kein normaler Neustart.

## Domain, Ports und Coolify

Standardmäßig wird Port 8080 veröffentlicht. Optional in `.env`:

```dotenv
MESHORA_PORT=8080
MESHORA_BIND_IP=0.0.0.0
```

URL und veröffentlichter Port müssen zusammenpassen. Hinter einem auf
demselben Host laufenden Reverse Proxy kann `MESHORA_BIND_IP=127.0.0.1`
verwendet werden.

In Coolify die normale Compose-Datei unter **Docker Compose Empty**
einfügen, `MESHORA_SERVER_URL` auf die eigene externe URL setzen und den
Dienst `meshora` mit dieser Domain und dem internen Zielport 8080 verbinden.
Coolify verwaltet die Proxy-Konfiguration. Die Datei benötigt keine
Coolify-spezifischen Variablen oder YAML-Felder.

Das Runtime-Image enthält keine Shell. Falls die Plattform keinen direkten
CLI-Aufruf anbietet, das erste Admin-Konto auf dem Docker-Host anlegen:

```bash
docker ps
docker exec -it CONTAINERNAME /usr/local/bin/headscale webadmins create admin --config /etc/meshora/config.yaml
```

## Authentik und OIDC

Unter `/admin/settings` den eigenen Issuer eintragen, beispielsweise
`https://auth.example.com/application/o/meshora/`, sowie Client-ID und
Geheimnis. Die Seite zeigt die Callback-URL, im Beispiel
`https://mesh.example.com/oidc/callback`. Diese URL im OIDC-Provider erlauben.
Nach dem Speichern Meshora neu starten:

```bash
docker compose restart meshora
```

Tailscale-Clients anschließend mit der eigenen Server-URL verbinden:

```bash
tailscale up --login-server=https://mesh.example.com
```

## Version und Image-Archive

Die Compose-Datei verwendet ein festes Image-Release. Für ein anderes
Release `MESHORA_VERSION` in `.env` setzen.

Die Release-Seite bietet zusätzlich Docker-Image-Archive für AMD64 und ARM64
mit `SHA256SUMS`. Nach dem Download beispielsweise:

```bash
sha256sum --ignore-missing -c SHA256SUMS
docker load -i meshora_v0.1.0-rc.3_linux_amd64.docker.tar.gz
```

Für lokal geladene Images in `.env` zusätzlich `MESHORA_IMAGE=meshora` setzen.

## Selbst bauen

```bash
docker build -f Dockerfile.meshora --build-arg VERSION=dev -t meshora:dev .
```

Danach `MESHORA_IMAGE=meshora` und `MESHORA_VERSION=dev` verwenden.

## Bereits bestehende Installationen

Die neue Vorlage verwendet benannte Volumes. Wer bisher Host-Verzeichnisse
wie `./config` und `./data` eingebunden hat, muss diese Mounts weiterverwenden
oder die bestehenden Daten vor dem Wechsel in die Volumes übertragen.
Ein leerer neuer Datenträger führt zu einer neuen Installation.
