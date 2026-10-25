package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/tailscale/hujson"
)

// NetworkZones replaces network ACLs and grants with a deny-by-default zone policy.
// Stable IDs keep membership independent of mutable usernames and device names.
type NetworkZones struct {
	Zones      []NetworkZone   `json:"zones"`
	Links      []ZoneLink      `json:"links"`
	ExitLimits []ZoneExitLimit `json:"exitLimits"`
}

type NetworkZone struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Nodes     []NodeID       `json:"nodes"`
	Users     []UserID       `json:"users"`
	Subnets   []string       `json:"subnets"`
	Pool4     string         `json:"pool4,omitempty"`
	Pool6     string         `json:"pool6,omitempty"`
	Exits     []string       `json:"exits"`
	IPv6Pools []ZoneIPv6Pool `json:"ipv6Pools"`
}

type ZoneLink struct {
	From     string   `json:"from"`
	To       string   `json:"to"`
	Services []string `json:"services"`
}

// ZoneExitLimit intersects with zone permissions; an empty Exits list denies all.
type ZoneExitLimit struct {
	User  UserID   `json:"user,omitempty"`
	Node  NodeID   `json:"node,omitempty"`
	Exits []string `json:"exits"`
}

// ZoneIPv6Pool holds inventory requiring gateway/client setup.
// They never become mesh addresses or access permissions implicitly.
type ZoneIPv6Pool struct {
	Prefix       string                `json:"prefix"`
	Gateway      string                `json:"gateway"`
	Reservations []ZoneIPv6Reservation `json:"reservations"`
}

type ZoneIPv6Reservation struct {
	Node    NodeID `json:"node"`
	Address string `json:"address"`
}

func zoneTagValid(tag string) bool {
	name, ok := strings.CutPrefix(tag, "tag:")
	if !ok || name == "" || name[0] < 'a' || name[0] > 'z' {
		return false
	}

	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}

	return true
}

func zoneServiceValid(s string) bool {
	if s == "*" || s == "icmp:*" {
		return true
	}

	proto, ports, ok := strings.Cut(s, ":")
	if !ok || (proto != "tcp" && proto != "udp" && proto != "sctp") {
		return false
	}

	if ports == "*" {
		return true
	}

	first, last, ranged := strings.Cut(ports, "-")

	a, err := strconv.ParseUint(first, 10, 16)
	if err != nil || a == 0 {
		return false
	}

	if !ranged {
		return true
	}

	b, err := strconv.ParseUint(last, 10, 16)

	return err == nil && b >= a
}

// ErrNetworkZones identifies invalid zone configuration at every write boundary.
var ErrNetworkZones = errors.New("invalid network zones")

func validateZoneTags(tags []string) error {
	for _, tag := range tags {
		if !zoneTagValid(tag) {
			return fmt.Errorf("%w: invalid gateway tag %q", ErrNetworkZones, tag)
		}
	}

	return nil
}

func appendZoneRange(raw string, ranges *[]netip.Prefix) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(raw)
	if err != nil || p != p.Masked() || p.Addr().Is4In6() {
		return netip.Prefix{}, fmt.Errorf("%w: invalid canonical prefix %q", ErrNetworkZones, raw)
	}

	if slices.ContainsFunc(*ranges, p.Overlaps) {
		return netip.Prefix{}, fmt.Errorf("%w: overlapping prefix %q", ErrNetworkZones, raw)
	}

	*ranges = append(*ranges, p)

	return p, nil
}

func validateZoneMeshPools(zone NetworkZone, pools *[]netip.Prefix) error {
	for _, pair := range [][2]string{{zone.Pool4, "100.64.0.0/10"}, {zone.Pool6, "fd7a:115c:a1e0::/48"}} {
		if pair[0] == "" {
			continue
		}

		p, err := appendZoneRange(pair[0], pools)
		if err != nil {
			return err
		}

		parent := netip.MustParsePrefix(pair[1])
		if p.Addr().BitLen() != parent.Addr().BitLen() || p.Bits() < parent.Bits() || p.Bits() > p.Addr().BitLen()-2 || !parent.Contains(p.Addr()) {
			return fmt.Errorf("%w: mesh pools must be subprefixes of the Tailscale address ranges with at least four addresses", ErrNetworkZones)
		}
	}

	return nil
}

func validateZoneSubnets(raws []string, subnets *[]netip.Prefix) error {
	for _, raw := range raws {
		p, err := appendZoneRange(raw, subnets)
		if err != nil {
			return err
		}

		if p.Bits() == 0 || !p.Addr().IsGlobalUnicast() {
			return fmt.Errorf("%w: invalid subnet %q", ErrNetworkZones, raw)
		}

		for _, forbidden := range []string{"0.0.0.0/8", "100.64.0.0/10", "fd7a:115c:a1e0::/48", "127.0.0.0/8", "169.254.0.0/16", "224.0.0.0/3", "::/128", "::1/128", "fe80::/10", "ff00::/8"} {
			if p.Overlaps(netip.MustParsePrefix(forbidden)) {
				return fmt.Errorf("%w: subnet %s overlaps a reserved or mesh range", ErrNetworkZones, raw)
			}
		}
	}

	return nil
}

func validateZonePublicPools(pools []ZoneIPv6Pool, public *[]netip.Prefix) error {
	for _, pool := range pools {
		p, err := appendZoneRange(pool.Prefix, public)
		if err != nil {
			return err
		}

		if !p.Addr().Is6() || p.Bits() < 3 || p.Bits() > 126 || !netip.MustParsePrefix("2000::/3").Contains(p.Addr()) {
			return fmt.Errorf("%w: invalid public IPv6 prefix %q", ErrNetworkZones, pool.Prefix)
		}

		if !zoneTagValid(pool.Gateway) {
			return fmt.Errorf("%w: public IPv6 pool requires a gateway tag", ErrNetworkZones)
		}

		addresses := map[netip.Addr]bool{}
		devices := map[NodeID]bool{}

		for _, r := range pool.Reservations {
			a, err := netip.ParseAddr(r.Address)
			if err != nil || !p.Contains(a) || a == p.Addr() || addresses[a] || r.Node == 0 || devices[r.Node] {
				return fmt.Errorf("%w: IPv6 reservations require unique devices and usable addresses inside their pool", ErrNetworkZones)
			}

			addresses[a], devices[r.Node] = true, true
		}
	}

	return nil
}

func (n *NetworkZones) validateLinks(ids map[string]bool) error {
	links := map[string]bool{}

	for _, link := range n.Links {
		key := link.From + "\x00" + link.To
		if !ids[link.From] || !ids[link.To] || len(link.Services) == 0 || links[key] {
			return fmt.Errorf("%w: links require existing zones, services and a unique source/destination pair", ErrNetworkZones)
		}

		links[key] = true

		for _, service := range link.Services {
			if !zoneServiceValid(service) {
				return fmt.Errorf("%w: invalid service %q; use tcp:443, udp:53, icmp:* or *", ErrNetworkZones, service)
			}
		}
	}

	return nil
}

func (n *NetworkZones) validateExitLimits() error {
	limits := map[string]bool{}

	for _, limit := range n.ExitLimits {
		if (limit.Node == 0) == (limit.User == 0) {
			return fmt.Errorf("%w: an exit limit requires exactly one user or device", ErrNetworkZones)
		}

		key := fmt.Sprintf("%d/%d", limit.User, limit.Node)
		if limits[key] {
			return fmt.Errorf("%w: duplicate exit limit", ErrNetworkZones)
		}

		limits[key] = true

		err := validateZoneTags(limit.Exits)
		if err != nil {
			return err
		}
	}

	return nil
}

// Validate rejects ambiguous membership, overlapping ranges and unsafe selectors.
func (n *NetworkZones) Validate() error {
	if n == nil {
		return nil
	}

	if len(n.Zones) > 128 || len(n.Links) > 1024 || len(n.ExitLimits) > 4096 {
		return fmt.Errorf("%w: too many zones, links or exit limits", ErrNetworkZones)
	}

	ids := map[string]bool{}
	nodes := map[NodeID]bool{}
	users := map[UserID]bool{}

	var subnets, pools, public []netip.Prefix

	for _, zone := range n.Zones {
		if zone.ID == "" || len(zone.ID) > 64 || ids[zone.ID] || strings.TrimSpace(zone.Name) == "" || len(zone.Name) > 128 {
			return fmt.Errorf("%w: zone IDs must be unique and names must be nonempty (maximum 128 bytes)", ErrNetworkZones)
		}

		ids[zone.ID] = true
		for _, id := range zone.Nodes {
			if id == 0 || nodes[id] {
				return fmt.Errorf("%w: a device may belong to only one zone", ErrNetworkZones)
			}

			nodes[id] = true
		}

		for _, id := range zone.Users {
			if id == 0 || users[id] {
				return fmt.Errorf("%w: a user may belong to only one zone", ErrNetworkZones)
			}

			users[id] = true
		}

		err := validateZoneTags(zone.Exits)
		if err != nil {
			return err
		}

		err = validateZoneMeshPools(zone, &pools)
		if err != nil {
			return err
		}

		err = validateZoneSubnets(zone.Subnets, &subnets)
		if err != nil {
			return err
		}

		err = validateZonePublicPools(zone.IPv6Pools, &public)
		if err != nil {
			return err
		}
	}

	for _, p := range public {
		if slices.ContainsFunc(subnets, p.Overlaps) {
			return fmt.Errorf("%w: an unprovisioned IPv6 pool cannot also be a routed zone subnet", ErrNetworkZones)
		}
	}

	err := n.validateLinks(ids)
	if err != nil {
		return err
	}

	return n.validateExitLimits()
}

// ValidateMeshPrefixes also checks the installation's narrower allocation pools.
func (n *NetworkZones) ValidateMeshPrefixes(v4, v6 *netip.Prefix) error {
	if n == nil {
		return nil
	}

	for _, z := range n.Zones {
		for i, raw := range []string{z.Pool4, z.Pool6} {
			if raw == "" {
				continue
			}

			p, err := netip.ParsePrefix(raw)

			parent := v4
			if i == 1 {
				parent = v6
			}

			if err != nil || parent == nil || p.Bits() < parent.Bits() || !parent.Contains(p.Addr()) {
				return fmt.Errorf("%w: zone %s pool %s is outside the configured mesh prefix", ErrNetworkZones, z.Name, raw)
			}
		}
	}

	return nil
}

func (n *NetworkZones) ZoneForNode(node NodeView) *NetworkZone {
	if n == nil || !node.Valid() {
		return nil
	}

	for i := range n.Zones {
		if node.ID() != 0 && slices.Contains(n.Zones[i].Nodes, node.ID()) {
			return &n.Zones[i]
		}
	}

	if node.IsTagged() {
		return nil
	}

	for i := range n.Zones {
		if slices.Contains(n.Zones[i].Users, node.TypedUserID()) {
			return &n.Zones[i]
		}
	}

	return nil
}

func (n *NetworkZones) ExitsForNode(node NodeView) []string {
	z := n.ZoneForNode(node)
	if z == nil {
		return nil
	}

	exits := slices.Clone(z.Exits)

	for _, limit := range n.ExitLimits {
		if limit.Node == node.ID() || (limit.User != 0 && !node.IsTagged() && limit.User == node.TypedUserID()) {
			exits = slices.DeleteFunc(exits, func(tag string) bool { return !slices.Contains(limit.Exits, tag) })
		}
	}

	return exits
}

// NetworkZonesFromPolicy reads only the zone model; full validation remains the
// policy engine's responsibility. The original HuJSON is never rewritten here.
func NetworkZonesFromPolicy(text string) (*NetworkZones, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil //nolint:nilnil // Absence selects the legacy policy and allocator.
	}

	raw, err := hujson.Standardize([]byte(text))
	if err != nil {
		return nil, err
	}

	var document struct {
		NetworkZones *NetworkZones `json:"networkZones"`
	}

	err = json.Unmarshal(raw, &document)
	if err != nil {
		return nil, err
	}

	return document.NetworkZones, nil
}
