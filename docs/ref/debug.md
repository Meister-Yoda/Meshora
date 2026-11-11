# Debugging and troubleshooting

Headscale and Tailscale provide debug and introspection capabilities that can be helpful when things don't work as
expected. This page explains some debugging techniques to help pinpoint problems.

Please also have a look at [Tailscale's Troubleshooting guide](https://tailscale.com/docs/reference/troubleshooting). It
offers a many tips and suggestions to troubleshoot common issues.

## Tailscale

The Tailscale client itself offers many commands to introspect its state as well as the state of the network:

- [Check local network conditions](https://tailscale.com/docs/reference/tailscale-cli#netcheck): `tailscale netcheck`
- [Get the client status](https://tailscale.com/docs/reference/tailscale-cli#status): `tailscale status --json`
- [Get DNS status](https://tailscale.com/docs/reference/tailscale-cli#dns): `tailscale dns status --all`
- Client logs: `tailscale debug daemon-logs`
- Client netmap: `tailscale debug netmap`
- Test DERP connection: `tailscale debug derp headscale`
- And many more, see: `tailscale debug --help`

Many of the commands are helpful when trying to understand differences between Headscale and Tailscale SaaS.

## Headscale

### Application logging

The log levels `debug` and `trace` can be useful to get more information from Headscale.

```yaml hl_lines="3"
log:
  # Valid log levels: panic, fatal, error, warn, info, debug, trace
  level: debug
```

### Database logging

The database debug mode logs all database queries. Enable it to see how Headscale interacts with its database. This also
requires the application log level to be set to either `debug` or `trace`.

```yaml hl_lines="3 7"
database:
  # Enable debug mode. This setting requires the log.level to be set to "debug" or "trace".
  debug: false

log:
  # Valid log levels: panic, fatal, error, warn, info, debug, trace
  level: debug
```

### Metrics and debug endpoint

Headscale provides a metrics and debug endpoint. It allows to introspect different aspects such as:

- Information about the Go runtime, memory usage and statistics
- Connected nodes and pending registrations
- Active policy, filters and SSH policy
- Current DERPMap
- Prometheus metrics

!!! warning "Keep the metrics and debug endpoint private"

    The listen address and port can be configured with the `metrics_listen_addr` variable in the [configuration
    file](configuration.md). By default it listens on localhost, port 9090.

    Keep the metrics and debug endpoint private to your internal network and don't expose it to the Internet.

    The metrics and debug interface can be disabled completely by setting `metrics_listen_addr: null` in the
    [configuration file](configuration.md).

Query metrics via <http://localhost:9090/metrics> and get an overview of available debug information via
<http://localhost:9090/debug/>. Metrics may be queried from outside localhost but the debug interface is subject to
additional protection despite listening on all interfaces.

=== "Direct access"

    Access the debug interface directly on the server where Headscale is installed.

    ```console
    curl http://localhost:9090/debug/
    ```

=== "SSH port forwarding"

    Use SSH port forwarding to forward Headscale's metrics and debug port to your device.

    ```console
    ssh <HEADSCALE_SERVER> -L 9090:localhost:9090
    ```

    Access the debug interface on your device by opening <http://localhost:9090/debug/> in your web browser.

=== "Via debug key"

    The access control of the debug interface supports the use of a debug key. Traffic is accepted if the path to a
    debug key is set via the environment variable `TS_DEBUG_KEY_PATH` and the debug key sent as value for `debugkey`
    parameter with each request.

    ```console
    openssl rand -hex 32 | tee debugkey.txt
    export TS_DEBUG_KEY_PATH=debugkey.txt
    headscale serve
    ```

    Access the debug interface on your device by opening `http://<IP_OF_HEADSCALE>:9090/debug/?debugkey=<DEBUG_KEY>` in
    your web browser. The `debugkey` parameter must be sent with every request.

=== "Via debug IP address"

    The debug endpoint expects traffic from localhost. A different debug IP address may be configured by setting the
    `TS_ALLOW_DEBUG_IP` environment variable before starting Headscale. The debug IP address is ignored when the HTTP
    header `X-Forwarded-For` is present.

    ```console
    export TS_ALLOW_DEBUG_IP=192.168.0.10       # IP address of your device
    headscale serve
    ```

    Access the debug interface on your device by opening `http://<IP_OF_HEADSCALE>:9090/debug/` in your web browser.

## Meshora joint network acceptance — 2026-10-25

This report closes the previously missing device-diagnostics acceptance
handover. The accumulated routes, DNS and policy changes were exercised
together with device filtering and onboarding. Tests used the complete
working tree based on `3fa01802`, a newly built
`Dockerfile.meshora` image, a disposable SQLite database and three real
Tailscale 1.104.1 clients. No production policy, DNS source or deployment
was changed.

### Isolation and evidence

All server configuration, credentials, resolver fixtures and client state
belonged to the acceptance run. Only its controller wrote configuration:
initial setup, subsequent dashboard submissions and controlled restarts
were sequential. No external configuration editor or second Meshora
process wrote the source. File-source compare-and-swap remains subject to
the cross-process limitation documented in [Policy](policy.md).

The disposable artifacts are under
`control_logs/network-acceptance/` (gitignored): build/lint/test logs,
`traffic-results.json`, `exit-client-routes.txt`, `dns-results.json`,
`ui-results.json`, screenshots and recorded offline diagnostics.
They are local evidence, not release assets; fixture secrets and private
test certificates must never be published.

The initial fixture had an empty DERP map and was corrected before client
acceptance. Docker-only control-name resolution and Tailscale's HTTPS
reconnect fallback required a fixed control-host mapping and a local TLS
proxy trusted only by the test clients. The successful DNS restart run
then reconnected clients automatically, without restarting them.

### Verified behavior

- Browser onboarding assigned an actual pending client to the intended
  user. It registered and opened its control stream.

- An unapproved advertised subnet failed actual HTTP requests. Approval
  enabled IPv4 and IPv6 requests; withdrawal blocked requests again.
  Responses identified the isolated target and observed source address.

- A single exit-node action approved both `0.0.0.0/0` and `::/0`.
  HTTP over each family succeeded through the selected exit node, and
  revoking either displayed default route revoked both and blocked both
  requests.

- For the synthetic exit target, its directly attached LAN was also
  advertised to satisfy Tailscale's local-forwarding safeguard, but its
  subnet routes were **not approved**. Recorded client IPv4 routes show the
  default route, with no specific route to that target subnet. Thus the
  successful request used exit routing, not an approved target subnet.

- This WSL kernel lacks Tailscale IPv6 NAT support. Isolated targets had
  an explicit return route for the tailnet IPv6 prefix. IPv6 forwarding
  and withdrawal were tested; IPv6 NAT was not.

- Invalid policy validation preserved working peer access. Saving a valid
  restrictive draft left access unchanged. Activating it denied the
  formerly allowed peer while still permitting the tagged router.
  Restoring the policy restored peer access, with clients continuously
  connected during those policy changes.

- The router belonged to `tag:router`; its recorded creator was not used
  as user ownership. A manual subnet withdrawal under matching
  `autoApprovers` blocked traffic. Activating the saved policy again
  restored approval and actual traffic, as the dashboard warning states.

- DNS validation rejected an invalid resolver. Saving valid resolver,
  MagicDNS domain, search-domain and Split-DNS changes kept the old
  resolution active. Policy activation also kept it active. After server
  restart, real queries through the client's `100.100.100.100` resolver
  used the new global resolver, the separate split resolver and the new
  MagicDNS suffix. A short-name lookup through the system resolver
  verified the search domain. Old-only names stopped resolving.

- Chromium checked 40 page/language/viewport combinations: overview,
  devices, device details, routes, DNS, policy overview/editor, pending
  registrations, settings and onboarding, in English/German at 1440/390
  pixels. No document overflow or uncaught page errors occurred.
  Screenshots were inspected. Keyboard checks covered device filtering,
  opening mobile navigation, following a route link and focus traversal.

- HTML-looking policy comments remained text in the editor and preview.
  A forged native form submission returned HTTP 403 and left the saved
  policy unchanged. An intervening activation invalidated another open
  form. Stopping a real peer produced the explicit offline diagnostic;
  the peer was restarted after that check. A later browser action expired
  its authorization and verified the separate sign-in guidance. Mobile
  table scrolling was exercised with the keyboard; the Linux connection
  guide remained usable with JavaScript disabled.

- A second fresh instance used a file-backed policy. Browser validation
  rejected duplicate keys, unknown sections and invalid rules without
  writing the file. Saving retained the exact submitted HuJSON, including
  comments and a leading newline; activation remained a separate action.
  Native form submission normalized LF to CRLF as documented.

### Diagnostic interpretation

Online means a recorded control connection, not successful service or
route access. Offline does not identify the cause. Expired authorization
is a separate state; template tests cover its sign-in guidance and UTC
timestamps. The live offline check and independent HTTP probes above
must not be conflated with a dashboard reachability measurement. The UI
does not perform one.

`TestDeviceDiagnostics`, `TestRouteDiagnosticsOnlyListUnapprovedAdvertisements`,
`TestDiagnosticTranslations`, device-filter tests and public-enrollment
tests ran with the accumulated changes. File/database policy validation,
source conflicts, exact HuJSON preservation, write failures and
post-switch follow-up failures are also covered by state/HTTP tests,
including race checks. Injected failure tests are not claims that such
failures were induced in the real-client browser run.

### Suite status and remaining limits

The first full `go test ./... -count=1 -timeout=20m` run timed out in
the server suite. Missing `tofu`/`tscli` was resolved by isolated,
checksum-verified binaries (OpenTofu 1.13.1, tscli 0.3.0). The JSON
diagnostic run identified the locally enabled `TestHAProberProperty`:
its documented default is 100 real-Noise checks, about 15–25 seconds
each. The next full run retains that budget, records per-test JSON and
uses `GOMAXPROCS=4`, `-parallel=4`, `-timeout=45m`.
An intermediate diagnostic run was stopped after temporary Go helper
programs in the log directory were inadvertently included as a duplicate-
main package. Their source files now have non-Go extensions;
`go list ./...` confirms they are outside package discovery. The final
complete run is recorded in `unit-complete.jsonl`.

Full-suite final result: **passed**, exit code 0, 30 test-bearing packages.
The server package completed in 1831.845 seconds, including all 100 HA
property checks (1238.74 seconds). No assertion was relaxed and no
server-suite case was skipped. The corrected full command was:

```sh
PATH="/path/to/acceptance/tools:$PATH" GOMAXPROCS=4 \
  go test ./... -count=1 -json -parallel=4 -timeout=45m
```

The complete lint run reported zero issues and `git diff --check`
passed. Race tests passed for webui, templates, state, policy/v2, types,
db and the CLI. Docker integration tests passed for enabling subnet and
exit routes, MagicDNS, live database policy updates and multiple-network
exit routing.

CSS formatting was checked with Prettier after the browser run. Its
non-whitespace content was unchanged; a newly built formatted image
passed setup/health smoke checks. No behavior or presentation change
required another browser run.

HTTP probes opened fresh connections; revocation of already established,
long-lived flows was not tested.

Not tested here: DoH/NextDNS resolver services, an existing production database/configuration, physical
Windows/macOS/mobile clients, the user's Authentik provider, Safari or
Firefox, Internet-wide IPv6/NAT, PostgreSQL in this combined run, DNS-01
or certificates. TLS certificates above were test infrastructure only.

For later policy editing, retain the separate validate/save/activate
steps, raw HuJSON and loaded-snapshot evidence. Do not claim client
delivery or reachability from an accepted snapshot. Follow-up failures
after a successful engine switch mean new policy active with incomplete
follow-up work, not rollback. Policy reload never activates saved global
DNS settings; device-specific NextDNS capabilities remain separate.

### Publication and CI follow-up

The accepted feature set was published as `v0.1.0-rc.8`, commit
`8a463df7fb1f3250feb9be5a03f4314e66325391`. Release workflow
`40210745120` passed. Release-asset checksums, both GHCR platforms and the
published AMD64 image's version, setup and health were verified.
The tag and `latest` shared image-index digest
`sha256:7a17871e79d76e1f4e11a5318d0b274e90e46a4de4387231c9766204ddd16e99`.

Nix CI passed its build, Go tests, Go lint and race checks. Its whole-tree
formatting check found one pre-existing extra blank line in
`hscontrol/assets/style.css`, outside the original changed-file set.
A follow-up removes only that blank line; CSS content excluding whitespace
is identical. The tracked-tree Prettier check passes. No tag or history
is rewritten and no new feature release is needed for that formatting-only
follow-up.

The separate documentation-site workflow built the docs successfully but
could not push `gh-pages`: GitHub returned HTTP 403 for
`github-actions[bot]`. This repository-permission/deployment follow-up
remains open. No deployment settings or permissions were changed.

## Meshora zone, exit and account acceptance — 2026-11-10

Local acceptance of the current checkout based on `aae667a9`, on branch
`codex/headscale-sync-2026-11-08`. Existing unrelated release/Compose changes
were preserved. No production database, policy, identity provider or mail server
was changed. No push or release was performed.

Two regression tests were added to the existing `integration/acl_test.go` and
registered by the documented workflow generator in the SQLite CI matrix.
They run real Tailscale 1.104 daemons in disposable containers; these are
packet-flow tests rather than synthetic filter evaluations.

### Verified data paths

- `TestNetworkZonesDataPlane`: four clients, two users, IPv4 and IPv6 HTTP.
  Every endpoint expected to be denied is first proven reachable using the
  same running service and exact target-container response. A directed TCP/80
  rule permits the intended source; reverse initiation and same-zone access
  without a rule are denied. Explicit device membership overrides inherited
  user membership. Changing the permitted port, removing the link and restoring
  it update actual traffic. Hidden personal zones allow same-owner devices and
  isolate other users.
- `TestNetworkZonesExitDataPlane`: a tagged router and client on separate
  networks, with separate IPv4 and IPv6 HTTP targets. Only `0.0.0.0/0` and
  `::/0` are approved; the advertised target subnets remain unapproved and are
  absent from the client's allowed routes. Responses identify the exact target.
  User and device exit restrictions each remove both default routes and stop
  both traffic families; removing each restriction restores them. A locked
  own-exit zone deny stops both traffic families despite an explicit user allow.
  Restoring the zone permission restores traffic. Revoking route approval
  removes both defaults and stops both traffic families again.
- Existing `TestEnablingRoutes`, `TestEnablingExitRoutes` and
  `TestSubnetRouterMultiNetworkExitNode` passed in a separate fresh run.

The exit targets use `198.51.100.0/24` and a per-router `2001:db8:…::/64`
documentation prefix on isolated Docker networks. They do not contact public
Internet hosts. An earlier fixture used an RFC1918 destination; the actual
`autogroup:internet` filter correctly excluded it, so that positive expectation
was corrected without widening product permissions. The IPv6 fixture explicitly
enables forwarding on the disposable router and adds a return route for
`fd7a:115c:a1e0::/48` on the target, because this WSL kernel lacks IPv6 NAT.
Only that target-route setup uses a privileged exec; neither the Meshora server
nor the target HTTP service runs privileged. No host forwarding or global
network setting is changed. IPv6 NAT itself is not covered.

### Account and code checks

Full `hscontrol/webui`, `hscontrol/templates`, `hscontrol/db` and
`hscontrol/types` tests passed (75.356 s, 0.021 s, 6.848 s and 0.060 s).
The WebUI suite includes local/OIDC login, TOTP, signed WebAuthn responses,
account/session isolation and email-confirmation checks. Targeted race tests
for OIDC, factors, account sessions, zone/exit management and route approval
passed in 86.785 s. Diff lint reported zero issues; `git diff --check` passed.
The generated matrix adds exactly the two new tests.

The final clean-container dual-stack run is
`control_logs/20261110-234956-89de84/`; both tests passed in 49.13 s and
35.84 s and the runner cleaned its containers. Summaries are under
`control_logs/roadmap-20261110/` (gitignored). The runner used the freshly built
current server image `hs-zone-exit-data-86de82:latest` with the corrected
precompiled tests, following the documented prebuilt-image procedure.
Diagnostic runs and fixture corrections remain in the local logs; the final
result does not depend on manual changes to running containers.

### Limits

This is local network and account-handler acceptance on one client version.
It does not establish public-Internet connectivity, IPv6 NAT, real hardware,
new production Authentik/mail delivery or browser/platform-authenticator
compatibility. No new browser UI was changed. The proposed integrated Meshora
server exit and the remaining upstream integration work retain their separate
open status in the roadmap. This run is not a full repository-wide or
PostgreSQL integration suite.
