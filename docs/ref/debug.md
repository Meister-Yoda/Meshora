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
