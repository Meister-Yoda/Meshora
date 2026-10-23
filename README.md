# Meshora

**English** · [Deutsch](README.de.md)

Meshora is a self-hosted Tailscale control server built on
[Headscale](https://github.com/juanfont/headscale), with a web dashboard for
managing users, devices, registrations and pre-auth keys.

> Meshora is an independent fork, unaffiliated with Tailscale Inc. and the
> Headscale project. Please report Meshora issues in this repository.

## v0.1.0-rc.6

- Sidebar navigation, with a collapsible menu on smaller screens.
- Separate settings pages for identity provider, admin access and appearance.
- OIDC callback URLs grouped with the identity provider settings.
- Compact German/English flag dropdown with a saved language preference.
- Selected Headscale fixes for database connections, policy updates, map sessions
  and shutdown; Go builds updated to 1.27.3.
- Authentik admin sign-in, role/group restrictions and local-login recovery.

## Start with Docker

Use the [Compose file](packaging/docker/compose.yaml) with Docker Compose v2:

```bash
docker compose up -d
```

Open `http://localhost:8080/setup` (or your server's address), set the public
server URL, and create your first local admin account. Passwords require at
least 12 characters. OIDC is optional and can also be configured later under
**Settings**. The setup assistant saves the configuration and activates the
server automatically. Existing installations keep their data and skip setup.

The release image is `ghcr.io/meister-yoda/meshora:v0.1.0-rc.6`, for Linux
AMD64 and ARM64. See the [Docker guide](packaging/docker/README.md) for volumes,
reverse proxies and updates. Route a reverse proxy to **container port 8080**.

## Dashboard

Visit `/admin/` to view users, registered and online devices, and pending
requests. Admins can create, rename and delete users; rename, tag, expire and
delete devices; assign/reject registrations; approve/reject SSH checks; and
create/expire pre-auth keys. New keys are displayed in full only once.

Choose **Deutsch** or **English** at the top of the page. The selected language
is saved in a cookie across login, setup, dashboard and public pages. Before a
selection, the browser's language preference is used; English is the fallback.
User-entered names, secrets, commands and custom button text are not translated.

## Authentik admin sign-in

Under **Settings**, configure the issuer, client ID and client secret, then
specify **Admin roles** and/or **Authentik groups with admin access**. Restart
Meshora after changing provider credentials or authorization restrictions.
The OIDC button label has its own save button and takes effect immediately;
leave it blank to use the default label in the selected UI language.

Example configuration (local accounts remain available):

```yaml
server_url: https://vpn.example.com
web_admin:
  enabled: true
  session_ttl: 12h
  oidc:
    allowed_groups: []
    allowed_roles: [meshora-admin]
    roles_claim: roles
    button_text: ""  # translated default

oidc:
  issuer: https://auth.example.com/application/o/meshora/
  client_id: meshora
  client_secret_path: /run/secrets/meshora-oidc
  scope: [openid, profile, email, meshora_roles]
```

In Authentik, configure an OAuth2/OIDC provider with a signing key and enable
**Include claims in id_token**. Add these exact **Strict redirect URIs**:

- `https://vpn.example.com/admin/oidc/callback` for dashboard admins.
- `https://vpn.example.com/oidc/callback` for device registration.

Create an OAuth2 scope mapping named `meshora_roles`, assign it to the provider,
and request the same scope under `oidc.scope`. For current Authentik versions,
this mapping exports the user's assigned and inherited Authentik roles:

```python
return {"roles": [role.name for role in request.user.all_roles()]}
```

Assign the Authentik role `meshora-admin` only to the intended administrators.
An existing mapping can instead supply another top-level claim: configure its
name in **Roles claim**. The claim must be an array of exact, case-sensitive
role names. See [Authentik property mappings](https://docs.goauthentik.io/add-secure-apps/providers/property-mappings/).

An allowed role alone is sufficient if no group filter is set. An allowed group
alone is sufficient if no role filter is set. **When both lists are configured,
both must match**; within each list any one entry matches. Empty lists disable
admin OIDC entirely. Device filters (`oidc.allowed_groups`, `allowed_users`,
`allowed_domains`) never grant admin permissions. Every accepted SSO admin has
full dashboard permissions; this is an access gate, not a per-action role system.

### Automatic sign-in and recovery

When admin OIDC is configured, opening `/admin/` tries `prompt=none` once. An
existing Authentik session can sign in without interaction. If authentication,
consent or an allowed role is missing, the normal login page remains available.
The OIDC button starts an interactive attempt. Discovery/token failures also
fall back to the local form.

Open `/admin/login?local=1` to bypass automatic OIDC. Signing out also returns
to this local-login page, rather than immediately signing in again. Keep a
local admin account for recovery. A provider that does not return its callback
can be bypassed by opening this URL directly.

SSO sessions are held in memory and expire at the earlier of the ID-token expiry
or `session_ttl`. Role/group changes are checked at the next sign-in. Restarting
Meshora clears all admin sessions. Meshora logout does not end the Authentik
session, and SSO identities are never linked to local accounts by username/email.

## Connecting devices

```bash
tailscale up --login-server=https://vpn.example.com
```

With OIDC, users sign in with their identity provider and confirm the device.
Without OIDC, assign pending registrations under `/admin/pending` or use
`headscale auth register --auth-id … --user …`.

## Local admin accounts

```bash
headscale webadmins list
headscale webadmins create <username> [--password-file FILE]
headscale webadmins set-password <username> [--password-file FILE]
headscale webadmins delete <username>
```

Passwords are hashed with Argon2id. Changing a password or deleting an account
ends its sessions. Forms use CSRF protection, sign-in is rate limited, and the
dashboard uses a strict CSP without scripts, inline styles or external assets.

## Development

Go 1.27.3 or newer; `nix develop` provides the full toolchain. Read
[AGENTS.md](AGENTS.md) for conventions and test procedures. Use `make build`,
`make test`, and `make lint`. On Windows, build inside WSL.

The dashboard lives in `hscontrol/webui/`; templates and the English/German
catalog are in `hscontrol/templates/`; assets are in `hscontrol/assets/`.
OIDC tests use a local provider with signed tokens to exercise discovery, PKCE,
role/group restrictions, callback replay protection and automatic-login fallback.

## Based on Headscale

Core functionality comes from Headscale by Juan Font and contributors. Its
[documentation](https://headscale.net/stable/) applies to the control server.
Meshora adds the dashboard and the features described above.

BSD 3-Clause license; see [LICENSE](LICENSE).
