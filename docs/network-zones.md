# Network zones

Meshora's **Zonen** page at `/admin/zones` manages network permissions,
exit permissions and address ranges together. Zones are logical policy groups;
creating a zone does not configure an IEEE 802.1Q VLAN on a switch or router.

## Configure and activate

Use **Neue Zone** to open the creation dialog, or **Bearbeiten** on a zone to
edit it. Escape or **Schließen** dismisses the dialog without saving. Validation
errors keep the dialog open with the attempted values. Successful saves return
to the overview; activation is still a separate action.

1. Create a zone with its name, allocation range and permitted exits. Assign users on their user detail pages and devices on their device detail pages. Device assignments take precedence
   over user assignments. A user belongs to at most one zone, and a device has at
   most one explicit zone assignment. Tagged devices never inherit a creator's
   user membership. New user-owned devices inherit their owner's zone.
1. Use the address range field for the mesh allocation pool. IPv4 and optional IPv6 prefixes can be separated by a comma. Existing routed subnets remain in the policy when a zone is edited; advanced subnet policy changes can be made in the policy editor.
1. Select the tags of approved exit nodes this zone may use. Use a unique tag per
   exit when individual gateways must be distinguished. A shared tag represents
   a gateway group; the existing routing engine handles its router selection.
1. Create directed connections between zones and specify services such as
   `tcp:443`, `udp:53`, `icmp:*`, `tcp:8000-8100` or `*`.
1. Optionally narrow exits for a user or device. Effective permission is the
   intersection of the zone, user and device lists. An empty restriction denies
   every exit; removing the restriction restores inheritance. Restrictions do
   not grant access to an exit outside the zone's list.
1. Save the draft, review the effective assignment table and policy comparison,
   then explicitly activate it. Saving alone never changes the running policy.

The first save requires explicit acknowledgement that zone mode replaces
existing ACLs and grants, including application grants. Unrelated policy fields,
including SSH, groups, tag owners, auto-approvers, policy tests and comments,
are preserved. Existing policy tests must pass before saving and activation.
Source and loaded revisions protect against concurrent changes.

In explicitly configured zones, absent permissions deny access, including within the same zone.
Create a connection from a zone to itself to permit communication among its
members. Connections authorize initiation in the configured direction; reply
traffic is allowed. Separate rules are needed for initiation in the reverse
direction. User-owned devices without an explicit zone assignment automatically share a hidden personal zone with the same owner. Different users remain isolated. Unassigned tagged devices do not inherit a creator's personal zone.

## Routes and exits

Subnet routers must still advertise routes and an administrator or auto-approver
must approve them. Zone permissions do not create routes, enable forwarding,
configure local firewalls or make an offline gateway reachable.

Exit permission compiles to `autogroup:internet` grants with `via` tags. Default
routes from disallowed exits are excluded from the client's network map, and
packet filters enforce the permissions. Allowed tags on the page describe the
policy, not proof that a gateway is currently online. The client still selects
its exit; this feature does not force a client preference or provision a client
management agent.

Devices behind a subnet router are represented by source network addresses,
not automatically by individual Meshora users. NAT can hide their original
addresses. User/device limits apply to identified mesh devices; configure the
subnet router and local network appropriately for routed subnet traffic.
Traffic staying entirely inside a local LAN/VLAN does not cross the mesh and
is outside these zone controls.

## Mesh address pools

Mesh IPv4 pools must be canonical subprefixes of the configured IPv4 allocation
prefix within `100.64.0.0/10`; IPv6 pools must lie in the configured allocation
prefix within `fd7a:115c:a1e0::/48`. Pools need at least four addresses and may
not overlap. Routed subnets are separate and cannot overlap mesh ranges.

A new user-owned registration uses its inherited zone's pools. An omitted
family uses the global pool, excluding all reserved zone pools. Tagged devices
without an explicit assignment use the global pool. The shared allocator tracks
all persisted addresses, excludes network/end addresses and Tailscale reserved
ranges, and publishes dual-stack allocations only when both families succeed.
Missing address families during backfill also respect zone pools.

Existing devices keep their current addresses when membership or pools change.
A pool defines allocation, not identity or permission. This avoids unexpected
renumbering and connection loss. Used addresses remain reserved even when a
pool changes or a server restarts. Exhaustion returns an error instead of
borrowing another zone's addresses.

## Public IPv6 inventory

Select an existing zone to add a routed public IPv6 prefix, the gateway tag and
fixed address reservations for devices in that zone. Gateway tags must already
exist in `tagOwners`; a gateway does not have to be an approved Internet exit
just to be recorded as an IPv6 gateway. Prefixes and reservations are validated
for overlaps, containment and duplicates. A single `/128` address cannot serve
as a distributable prefix. Documentation examples may use `2001:db8::/64`;
real deployments require a prefix actually routed by the provider.

**These are reservations requiring setup, not automatically provisioned public
addresses.** Standard Tailscale clients cannot be given arbitrary additional
public addresses by this server setting alone. Provider routing, gateway
forwarding, the route to the selected device, client address configuration and
inbound firewall permissions must be established separately. Saving or
activating a reservation performs none of those host changes and does not
implicitly authorize inbound traffic. IPv6 prefixes generally cannot move to a
different provider or exit. Dynamic prefix changes require updating reservations
and the external gateway/client configuration.

Automated public-address provisioning, provider prefix delegation, automatic
renumbering of existing devices and forced client exit selection require a
separate gateway/client management component. They are not provided here.

## Storage and validation

The optional `networkZones` object is part of the existing file-backed or
database-backed HuJSON policy. No database schema migration is needed. Nonempty
`acls` or `grants` cannot be combined with `networkZones`; policies without this
object retain their original behavior. Zone rules are materialized against each
current node snapshot, including after registration, ownership or tag changes.

Regression tests cover deny-by-default behavior, directed service rules,
user/device exit intersection, exit visibility, tagged identity, registration
and restart, concurrent allocation, dual-stack exhaustion, IP backfill, public
IPv6 reservations, CSRF, draft activation and stale revision rejection.

## Hidden personal zones and providing exits

When zone mode is active, each user has an implicit personal zone containing only
that user's otherwise unassigned, user-owned mesh devices. Those devices may
communicate with each other. Named zone assignments take precedence. Tagged
devices never inherit a creator's personal zone. Personal zones are derived at
runtime and are not listed in the matrix or zone picker. They do not implicitly
grant Internet exit access. Legacy policies without `networkZones` are unchanged.

User and device details can restrict providing an exit **in the hidden personal
zone only**. Either restriction blocks provision there; a device toggle cannot
undo its owner's personal-zone restriction. These restrictions do not carry into
named zones. Each named zone has its own `blockOwnExits` option, including for
explicitly assigned tagged devices. Allowed exits for outgoing traffic remain a
separate setting.

Provider restrictions are saved as policy drafts and apply after activation.
Activation revokes existing exit-route approvals while retaining subnet routes.
Manual and automatic approval cannot re-enable a prohibited exit. The network
map excludes prohibited default routes, and Internet via packet-filter rules are
suppressed on prohibited providers. A client may still announce an exit locally;
the server does not authorize its use as an exit in this tailnet. Removing a
restriction does not automatically grant route approval.

## Own exits in the matrix

The **Eigene Exits** destination column controls use of approved exits belonging
to the same effective named zone, or the same owner in a hidden personal zone.
It does not authorize other users' personal exits. Existing explicitly permitted
external exit tags remain separate.

Click the cell to set the zone default: **Erben**, **Gesperrt**, or **Erlaubte
Dienste**. Services accept `*`, `tcp:443`, `udp:53`, `icmp:*`, and port ranges.
The default without a rule is deny. Device rules override user rules, user rules
override policy-group rules, and group rules override the zone default. An
explicit **Erben** removes that override. For several matching groups, any deny
wins; otherwise protocol/port intervals are intersected, including partial
range overlaps. Policy groups are the canonical `groups` in the HuJSON policy,
not unverified identity-provider claims.

A zone's **Nicht überschreibbare Sperre** is only valid with **Gesperrt** and
prevents all device, user and group exceptions. Entity details show the
resulting services and their origin. Group overrides are edited on the zones
page. All changes remain drafts until separately activated.

Providing an exit is still independent of permission to use it. A prohibited
provider cannot become available through a use-rule exception. Approved default
routes are distributed only for permitted pairs. Internet filters are installed
only on the corresponding provider and retain the effective protocol/port
limits. Legacy broad via grants cannot bypass those limits for a provider in
the same zone. This does not force the client's selected exit, limit bandwidth,
or provision gateway forwarding.
