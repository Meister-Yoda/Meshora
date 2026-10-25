# Network zones

Meshora's **Zonen** page at `/admin/zones` manages network permissions,
exit permissions and address ranges together. Zones are logical policy groups;
creating a zone does not configure an IEEE 802.1Q VLAN on a switch or router.

## Configure and activate

1. Create a zone and assign devices or users. Device assignments take precedence
   over user assignments. A user belongs to at most one zone, and a device has at
   most one explicit zone assignment. Tagged devices never inherit a creator's
   user membership. New user-owned devices inherit their owner's zone.
1. Optionally enter routed IPv4/IPv6 subnet prefixes and mesh allocation pools.
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

In zone mode, absent permissions deny access, including within a zone.
Create a connection from a zone to itself to permit communication among its
members. Connections authorize initiation in the configured direction; reply
traffic is allowed. Separate rules are needed for initiation in the reverse
direction. Devices outside all zones receive no zone permissions.

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
