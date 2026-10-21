# Meshora auf dem Docker-Server testen

Die Vorabversion enthält Docker-Images für Linux AMD64 und ARM64 als
Download. Ein Registry-Konto ist nicht erforderlich. Benötigt werden Docker
und Docker Compose v2. Die Images laufen als root im Container.

## Download und Start im LAN

Von der Release-Seite dieses Projekts
`meshora-compose.tar.gz`, `SHA256SUMS` und das passende Image herunterladen:
`meshora_v0.1.0-rc.1_linux_amd64.docker.tar.gz` für Intel/AMD oder
`meshora_v0.1.0-rc.1_linux_arm64.docker.tar.gz` für ARM64.

```bash
sha256sum --ignore-missing -c SHA256SUMS
docker load -i meshora_v0.1.0-rc.1_linux_amd64.docker.tar.gz
mkdir meshora
tar -xzf meshora-compose.tar.gz -C meshora
cd meshora
mkdir -p config data
cp config.yaml config/config.yaml
cat > .env <<'EOF'
MESHORA_VERSION=v0.1.0-rc.1
MESHORA_SERVER_URL=http://192.168.1.100:8080
EOF
```

In `.env` die Beispiel-IP durch die IP des Docker-Servers ersetzen. Optional
`MESHORA_BIND_IP` auf dessen LAN-IP setzen; standardmäßig wird Port 8080 auf
allen Interfaces veröffentlicht. Den unverschlüsselten Testzugang nur im
vertrauenswürdigen LAN verwenden.

```bash
docker compose up -d
docker compose ps
docker compose logs --tail=100 meshora
docker compose exec meshora /usr/local/bin/headscale webadmins create admin --config /etc/meshora/config.yaml
```

Beim Erstellen das gewünschte Admin-Passwort eingeben und `http://SERVER-IP:8080/admin/`
öffnen. Daten, Schlüssel und Einstellungen liegen dauerhaft in `data/` und
`config/`. Vor einem Versionswechsel beide Verzeichnisse bei gestopptem
Container sichern. Die Konfiguration wird als beschreibbares Verzeichnis
eingebunden, damit das Webformular sie atomar speichern kann.

## Authentik und Traefik

Für Authentik bevorzugt zuerst HTTPS über Traefik einschalten. In `.env`:

```dotenv
MESHORA_SERVER_URL=https://mesh.example.com
TRAEFIK_NETWORK=traefik
TRAEFIK_ENTRYPOINT=websecure
MESHORA_BIND_IP=127.0.0.1
```

Netzwerk und Entrypoint an die vorhandene Traefik-Installation anpassen.
DNS muss auf Traefik zeigen. Das Overlay verwendet den Standard-TLS-Resolver
von Traefik. Falls keiner konfiguriert ist, im Overlay das Label
`traefik.http.routers.meshora.tls.certresolver` mit dem eigenen Resolvernamen
ergänzen.

```bash
docker compose -f compose.yaml -f compose.traefik.yaml up -d
```

Unter `/admin/settings` den Issuer
`https://auth.example.com/application/o/meshora/` eintragen, sofern der
Authentik-Anwendungs-Slug `meshora` lautet. Client-ID und Geheimnis selbst
eintragen. Die Seite zeigt die gültige Callback-URL; bei HTTPS ist sie
`https://mesh.example.com/oidc/callback`. Genau diese URL im
Authentik-OAuth2/OIDC-Provider erlauben. Bei einem LAN-Test stattdessen die
angezeigte Callback-URL mit Server-IP verwenden.

Nach dem Speichern neu starten:

```bash
docker compose restart meshora
```

Neustarts beenden bestehende Admin-Sitzungen. Tailscale-Clients verbinden
sich anschließend mit `tailscale up --login-server=https://mesh.example.com`.

## Selbst bauen

```bash
docker build -f Dockerfile.meshora --build-arg VERSION=dev -t meshora:dev .
```

Beim Wechsel auf dieses Image `MESHORA_VERSION=dev` in `.env` setzen.
