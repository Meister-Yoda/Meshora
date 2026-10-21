# Meshora

**English** · [Deutsch](README.de.md)

Meshora is a self-hosted Tailscale control server built on
[Headscale](https://github.com/juanfont/headscale). It keeps everything
Headscale does and adds a web dashboard, so day-to-day administration no
longer needs the command line:

- **End users** connect the regular Tailscale client and sign in with your
  identity provider (OIDC).
- **Admins** sign in to a web dashboard at `/admin` with a local admin
  account and manage users, devices and pending registrations there.

> Meshora is an independent fork. It is not affiliated with Tailscale Inc. or
> the Headscale project. Please do not report Meshora issues to Headscale.

## Features

Everything Headscale provides (node registration, IP allocation, ACL policy,
MagicDNS, DERP, OIDC, …), plus:

- **Admin dashboard** at `/admin`
  - overview of users, nodes and online devices
  - lists of users, nodes and pending registrations
- **Local admin accounts**, separate from tailnet users
  - Argon2id password hashes
  - in-memory sessions that end on password change, account deletion or restart
  - CSRF protection on every form and a login rate limit per IP and username
  - strict Content Security Policy: no scripts, no inline styles, no
    third-party requests
- **Meshora design** for the dashboard and for all end-user pages
  (registration, sign-in result, client setup), with light and dark mode and
  a mobile layout
- **CLI and API** to manage admin accounts (`headscale webadmins …`,
  `/api/v1/webadmin`)

## Status

| Phase | Content                                                                                            | State       |
| ----- | -------------------------------------------------------------------------------------------------- | ----------- |
| 1     | Admin accounts, login, sessions, Meshora design                                                    | done        |
| 2     | Dashboard lists: users, nodes, pending registrations                                               | done        |
| 3     | Dashboard actions: assign registrations, approve SSH checks, manage nodes, users and pre-auth keys | in progress |
| 4     | Sign-in comfort for end users: Meshora start page before OIDC, optional remembered session         | planned     |
| 5     | Documentation and a Docker Compose example (Meshora + reverse proxy + Keycloak)                    | planned     |

## Quick start

Meshora is configured like Headscale. Start from
[`config-example.yaml`](config-example.yaml) and enable the dashboard:

```yaml
server_url: https://vpn.example.com

web_admin:
  enabled: true
  # How long an admin stays signed in. Sessions are kept in memory.
  session_ttl: 12h

# Optional, but intended: end users sign in through your identity provider.
oidc:
  issuer: https://idp.example.com/realms/example
  client_id: meshora
  client_secret_path: /etc/meshora/oidc_client_secret
```

Build and run:

```bash
make build
./headscale serve -c /etc/meshora/config.yaml
```

Create the first admin account. The password is prompted twice, or read from
`--password-file` or piped stdin. It is never accepted as a flag value.

```bash
./headscale webadmins create admin
```

Then open `https://vpn.example.com/admin`.

### Connecting a device

On the device, point the Tailscale client at your server:

```bash
tailscale login --login-server https://vpn.example.com
```

With OIDC configured, the user signs in with the identity provider and
confirms the device. Without OIDC, an admin assigns the device to a user (from
the dashboard once phase 3 is done, or today with
`headscale auth register --auth-id … --user …`).

## Managing admin accounts

```bash
headscale webadmins list
headscale webadmins create <username> [--password-file FILE]
headscale webadmins set-password <username> [--password-file FILE]
headscale webadmins delete <username>
```

Passwords need at least 12 characters. Changing a password or deleting an
account ends that admin's open sessions.

## Development

Meshora is developed like Headscale. Read [`AGENTS.md`](AGENTS.md) for the
code conventions, database migration rules and test procedures.

- Go 1.27.2 or newer. `nix develop` provides the full toolchain.
- `make build`, `make test`, `make lint`
- After changing the API: `make client` regenerates `gen/client/`. Commit the
  generated code separately.
- On Windows, work inside WSL: one upstream test fixture has a `>` in its file
  name, which a Windows checkout cannot create.

Dashboard code lives in:

| Path                             | Purpose                                        |
| -------------------------------- | ---------------------------------------------- |
| `hscontrol/webui/`               | dashboard handlers, sessions, CSRF, rate limit |
| `hscontrol/templates/admin.go`   | dashboard pages                                |
| `hscontrol/assets/admin.css`     | dashboard stylesheet                           |
| `hscontrol/assets/style.css`     | stylesheet of the end-user pages               |
| `hscontrol/db/web_admin.go`      | admin accounts and password hashing            |
| `hscontrol/api/v1/webadmins.go`  | admin account API                              |
| `cmd/headscale/cli/web_admin.go` | `headscale webadmins` command                  |

## Based on Headscale

Meshora is built on [Headscale](https://github.com/juanfont/headscale) by
Juan Font and contributors. All core functionality comes from Headscale, and
its [documentation](https://headscale.net/stable/) applies to everything
except the dashboard. Thank you to everyone who works on Headscale.

## License

BSD 3-Clause, the same as Headscale. See [`LICENSE`](LICENSE).
