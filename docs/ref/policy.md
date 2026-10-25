# Policy

Headscale implements a large portion of Tailscale's [policy
features](https://tailscale.com/docs/features/tailnet-policy-file), most notably access control based on
[ACLs](https://tailscale.com/docs/features/access-control/acls) and
[Grants](https://tailscale.com/docs/features/access-control/grants) or [Tailscale
SSH](https://tailscale.com/docs/features/tailscale-ssh). See [limitations](#limitations) to learn about missing features
and notable implementation differences between Headscale and Tailscale.

Headscale uses the same [huJSON](https://github.com/tailscale/hujson) based file format as Tailscale. By default, no
policy is loaded which means that Headscale allows all traffic between nodes. To start using a policy file[^1], specify
its path in the `policy.path` key in the [configuration file](configuration.md).

Headscale needs to be reloaded to pick up changes to the policy file. Either reload Headscale via its systemd service
(`sudo systemctl reload headscale`) or by sending a SIGHUP signal (`sudo kill -HUP $(pidof headscale)`) to the main
process. Headscale logs the result of policy processing after each reload.

Please have a look at Tailscale's policy related documentation to learn more:

- [Tailscale policy file](https://tailscale.com/docs/features/tailnet-policy-file): A description of supported sections
  within the policy file along with links to syntax references for each section.
- [ACLs](https://tailscale.com/docs/features/access-control/acls): How to configure access control using ACLs.
- [Grants](https://tailscale.com/docs/features/access-control/grants): Introduction to Grants with links to [syntax
  reference](https://tailscale.com/docs/reference/syntax/grants),
  [examples](https://tailscale.com/docs/reference/examples/grants) and a [migration guide from ACLs to
  Grants](https://tailscale.com/docs/reference/migrate-acls-grants).

## Getting started

Headscale supports both [ACLs](https://tailscale.com/docs/features/access-control/acls) and
[Grants](https://tailscale.com/docs/features/access-control/grants) to write an access control policy. We recommend the
use of Grants since ACLs are considered legacy and will not receive new features by Tailscale.

### Allow All

If you define a policy file but completely omit the `"acls"` or `"grants"` section, Headscale will default to an [allow
all](https://tailscale.com/docs/reference/examples/acls#allow-all-default-acl) policy. This means all devices connected
to your tailnet will be able to communicate freely with each other.

```json title="policy.json"
{}
```

### Deny All

To [prevent all communication within your tailnet](https://tailscale.com/docs/reference/examples/acls#deny-all), you can
include an empty array for the `"grants"` section in your policy file.

```json title="policy.json"
{
  "grants": []
}
```

### More examples

- See our documentation on [subnet routers](routes.md#subnet-router) and [exit nodes](routes.md#exit-node) to learn how
  to restrict their use or how to automatically approve them.
- The Tailscale documentation provides a large collection of configuration examples:
    - [ACL examples](https://tailscale.com/docs/reference/examples/acls)
    - [Grants examples](https://tailscale.com/docs/reference/examples/grants)
    - [SSH configuration](https://tailscale.com/docs/features/tailscale-ssh#configure-tailscale-ssh)
    - [Define a tag](https://tailscale.com/docs/features/tags#define-a-tag)

______________________________________________________________________

## Limitations

- [Device postures](https://tailscale.com/docs/features/device-posture) and the related sections such as `postures` or
  `srcPosture` aren't supported.
- [IP sets](https://tailscale.com/docs/features/tailnet-policy-file/ip-sets) aren't supported.
- A subset of [Autogroups](#autogroups) are available.

## Autogroups

Headscale supports several [Autogroups](https://tailscale.com/docs/reference/targets-and-selectors#autogroups) that
automatically include users, destinations, or devices with specific properties. Autogroups provide a convenient way to
write policy rules without manually listing individual users or devices.

### [`autogroup:internet`](https://tailscale.com/docs/reference/targets-and-selectors#autogroupinternet)

Allows access to the internet through [exit nodes](routes.md#exit-node). Can only be used in policy destinations.

```json title="policy.json"
{
  "grants": [
    {
      "src": ["alice@"],
      "dst": ["autogroup:internet"],
      "ip": ["*"]
    }
  ]
}
```

### [`autogroup:member`](https://tailscale.com/docs/reference/targets-and-selectors#autogrouprole)

Includes all [personal (untagged) devices](registration.md/#identity-model).

```json title="policy.json"
{
  "grants": [
    {
      "src": ["autogroup:member"],
      "dst": ["tag:prod-app-servers"],
      "ip": ["80,443"]
    }
  ]
}
```

### [`autogroup:tagged`](https://tailscale.com/docs/reference/targets-and-selectors#autogrouptagged)

Includes all devices that [have at least one tag](registration.md/#identity-model).

```json title="policy.json"
{
  "grants": [
    {
      "src": ["autogroup:tagged"],
      "dst": ["tag:monitoring"],
      "ip": ["9090"]
    }
  ]
}
```

### [`autogroup:self`](https://tailscale.com/docs/reference/targets-and-selectors#autogroupself)

Includes devices where the same user is authenticated on both the source and destination. Does not include tagged
devices. Can only be used in policy destinations.

```json title="policy.json"
{
  "grants": [
    {
      "src": ["autogroup:member"],
      "dst": ["autogroup:self"],
      "ip": ["*"]
    }
  ]
}
```

!!! warning "The current implementation of `autogroup:self` is inefficient"

    Using `autogroup:self` may cause performance degradation on the Headscale coordinator server in large deployments,
    as filter rules must be compiled per-node rather than globally and the current implementation is not very efficient.

    If you experience performance issues, consider using more specific policy rules or limiting the use of
    `autogroup:self`.

    ```json title="policy.json"
    {
      "grants": [
        // The following rules allow internal users to communicate with their
        // own nodes in case autogroup:self is causing performance issues.
        {
          "src": ["boss@"],
          "dst": ["boss@"],
          "ip": ["*"]
        },
        {
          "src": ["dev1@"],
          "dst": ["dev1@"],
          "ip": ["*"]
        },
        {
          "src": ["intern1@"],
          "dst": ["intern1@"],
          "ip": ["*"]
        }
      ]
    }
    ```

### [`autogroup:nonroot`](https://tailscale.com/docs/reference/targets-and-selectors#other-built-in-targets)

Used in Tailscale SSH rules to allow access to any user except root. Can only be used in the `users` field of SSH rules.

```json title="policy.json"
{
  "ssh": [
    {
      "action": "accept",
      "src": ["autogroup:member"],
      "dst": ["autogroup:self"],
      "users": ["autogroup:nonroot"]
    }
  ]
}
```

### [`autogroup:danger-all`](https://tailscale.com/docs/reference/targets-and-selectors#autogroupdanger-all)

This autogroup resolves to all IP addresses (`0.0.0.0/0` and `::/0`) which also includes all IP addresses outside the
standard Tailscale IP ranges. This autogroup can only be used as source.

## Node Attributes

[Node attributes](https://tailscale.com/docs/reference/syntax/policy-file#node-attributes) allow for device-specific
configuration and attributes. At least the following node attributes are currently supported by Headscale[^2]:

- `drive:access`, `drive:share`: [Taildrive support](https://tailscale.com/docs/features/taildrive).
- `suggest-exit-node`, `suggest-exit-node-ui`: [Automatic exit node selection](routes.md#automatic-exit-node-selection).
- `nextdns:<profile>`, `nextdns:no-device-info`: [NextDNS integration](https://tailscale.com/docs/integrations/nextdns).
  Be sure to set NextDNS as global resolver in the [configuration](configuration.md).
- `magicdns-aaaa`: Respond to AAAA queries on the local [MagicDNS](https://tailscale.com/docs/features/magicdns)
  resolver at 100.100.100.100.
- `disable-ipv4`: Selectively disable IPv4 for specific nodes. This may be useful to work around [CGNat
  conflicts](https://tailscale.com/docs/reference/troubleshooting/network-configuration/cgnat-conflicts).
- `randomize-client-port`: Allocate a [random port for WireGuard
  traffic](https://tailscale.com/docs/reference/syntax/policy-file#randomizeclientport) instead of the static default
  port 41641.
- `disable-captive-portal-detection`: [Disable automatic captive portal
  detection](https://tailscale.com/docs/integrations/captive-portals#disable-captive-portal-detection).

```json title="policy.json"
{
  "nodeAttrs": [
    {
      // Enable MagicDNS AAAA records for all nodes
      "target": ["*"]
      "attr": ["magicdns-aaaa"]
    }
  ]
}
```

## Network-wide policy options

The following options are applied for the entire tailnet. Consider [node attributes](#node-attributes) for a more
fine-grained configuration instead.

- `randomizeClientPort`: Allocate a [random port for WireGuard
  traffic](https://tailscale.com/docs/reference/syntax/policy-file#randomizeclientport) instead of the static default
  port 41641.

```json title="policy.json"
{
  // Use a random WireGuard port for the entire tailnet
  "randomizeClientPort": true
}
```

## Meshora policy overview and editor

The overview at `/admin/policy` reads the current file/database source.
`/admin/policy/edit` adds three separate actions:

1. **Validate and preview** uses a private instance of the real policy engine
   with current users/nodes. Compilation, declared ACL/SSH tests and per-node
   SSH policy construction must succeed.
1. **Save validated draft** writes exact submitted HuJSON to the configured
   source. It does not replace the running policy.
1. **Activate saved source** revalidates the reviewed saved revision, then
   switches the engine and performs existing reload work.

All actions require an admin session and CSRF token. Saving also requires
the same session's unexpired validation token and exactly the validated
text. Editing after validation requires revalidation. No JSON serialization,
minimization or formatting is applied to the draft. Unknown fields stay in
the draft, but the existing engine rejects them; the editor reports this
without deleting them. Duplicate keys at any nesting level, including
equivalent escaped names, require explicit resolution. The line-based
before/after preview never rewrites text. Browsers may normalize textarea
line endings during submission; the server stores submitted text. A leading
newline is preserved when the original text is loaded into the textarea.

### Loaded-policy evidence

The policy manager records original accepted input, SHA-256, process-local
revision, UTC acceptance time and whether startup used an implicit policy.
It commits this snapshot under the engine lock after successful compilation.
Startup, ordinary State/API policy writes and reloads share this boundary.
Rejected writes do not advance the snapshot; legacy empty SetPolicy input
remains a no-op. Revisions and dashboard sessions reset on process restart.

This proves engine acceptance, not client delivery, effective access or
reachability. Source revisions and database row IDs are separate evidence.

### Conflicts and failure boundaries

State-level policy writes and reloads in this process are serialized.
Validation, saving and activation compare the source revision (mode,
reference/row ID, exact text) and loaded revision. Activation repeats engine
validation against current users and devices. Stale forms are rejected,
not silently rebased.

Both configured file and database modes are editable. File saves resolve
the configured target, write and sync an owner-only temporary file, recheck
the source revision and atomically replace the target. Database saves
append source records without changing the engine. Missing/unreadable
sources disable editor actions. Draft size is limited to 1 MiB; the
authenticated form limit permits URL-encoding overhead.

Do not write concurrently through an external file editor or another server
process. Revision rechecks detect preceding changes, but the in-process
lock cannot guarantee cross-process compare-and-swap or exclude an external
writer in the final filesystem replacement window. Existing policy APIs
retain their established apply/persist/reload sequence; this dashboard
does not turn that sequence into a transaction.

Validation and engine-switch errors leave the previous loaded snapshot
unchanged. Write failures do not activate the draft. Errors after the engine
switch are explicitly reported as **new policy active, follow-up work
failed**. There is no rollback claim: route approvals may already have
changed in the NodeStore. Returned changes are dispatched even on this
partial outcome. Inspect the error and route state before retrying.

### Content, identity and DNS

The overview retains all configured fields and original HuJSON. Display
parsing is not engine validation. Invalid source content can be inspected
and repaired without replacing the loaded valid policy. Tagged devices
belong to their tags; a recorded creator does not make them user-owned.

Policy activation does not activate DNS settings saved for the next process
start. NextDNS capabilities may affect per-device DNS separately from the
global baseline at `/admin/dns`.

### Joint acceptance checklist

Use disposable file and database sources:

- Compare source and loaded revisions/hashes after validation, saving and
  activation. Verify comments and submitted text survive.
- Reject malformed rules, unknown fields, duplicate keys and failing ACL/SSH
  tests without changing either source or loaded snapshot.
- Reject edited validated text, missing/expired/foreign validation tokens,
  external source changes and an intervening policy activation.
- Inject source-write and pre-switch errors; separately inject route
  persistence failure after switching and verify the explicit partial
  result and returned client-change notifications.
- Verify HTML-looking text stays text in the editor, preview and diagnostics,
  and forged/missing CSRF tokens cannot mutate state.
- Exercise German/English, mobile scrolling, keyboard focus and all three
  actions. Real-client distribution and reachability need separate checks;
  configuration-workflow tests do not prove either.

[^1]: Headscale also allows to store the policy in the database. This is typically only required in case a [web
    interface](integration/web-ui.md) is used.

[^2]: Other key-only node attributes can be used as well. Find them in the client source code with `grep -E ' Cap ' tailcfg/nodecap/nodecap.go` or by using [GitHub code search (requires
    login)](https://github.com/search?q=repo%3Atailscale%2Ftailscale+language%3Ago+path%3Atailcfg%2Fnodecap%2Fnodecap.go+%2F+Cap+%2F&type=code).
