package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNetworkZoneValidation(t *testing.T) {
	cases := map[string]*NetworkZones{
		"overlap":          {Zones: []NetworkZone{{ID: "a", Name: "A", Subnets: []string{"192.168.0.0/24"}}, {ID: "b", Name: "B", Subnets: []string{"192.168.0.0/25"}}}},
		"mesh as subnet":   {Zones: []NetworkZone{{ID: "a", Name: "A", Subnets: []string{"100.64.0.0/24"}}}},
		"default route":    {Zones: []NetworkZone{{ID: "a", Name: "A", Subnets: []string{"::/0"}}}},
		"duplicate device": {Zones: []NetworkZone{{ID: "a", Name: "A", Nodes: []NodeID{1}}, {ID: "b", Name: "B", Nodes: []NodeID{1}}}},
		"duplicate user":   {Zones: []NetworkZone{{ID: "a", Name: "A", Users: []UserID{1}}, {ID: "b", Name: "B", Users: []UserID{1}}}},
		"bad service":      {Zones: []NetworkZone{{ID: "a", Name: "A"}}, Links: []ZoneLink{{From: "a", To: "a", Services: []string{"tcp:99999"}}}},
		"unknown target":   {Zones: []NetworkZone{{ID: "a", Name: "A"}}, Links: []ZoneLink{{From: "a", To: "b", Services: []string{"*"}}}},
		"single ipv6":      {Zones: []NetworkZone{{ID: "a", Name: "A", IPv6Pools: []ZoneIPv6Pool{{Prefix: "2001:db8::1/128", Gateway: "tag:exit"}}}}},
		"outside ipv6":     {Zones: []NetworkZone{{ID: "a", Name: "A", IPv6Pools: []ZoneIPv6Pool{{Prefix: "2001:db8::/64", Gateway: "tag:exit", Reservations: []ZoneIPv6Reservation{{Node: 1, Address: "2001:db9::1"}}}}}}},
		"mixed limit":      {ExitLimits: []ZoneExitLimit{{Node: 1, User: 1}}},
	}
	for name, model := range cases {
		t.Run(name, func(t *testing.T) { require.Error(t, model.Validate()) })
	}

	valid := &NetworkZones{Zones: []NetworkZone{{ID: "a", Name: "A", Pool4: "100.64.0.0/24", Pool6: "fd7a:115c:a1e0:1::/64", Subnets: []string{"192.168.0.0/24"}, IPv6Pools: []ZoneIPv6Pool{{Prefix: "2001:db8::/64", Gateway: "tag:exit", Reservations: []ZoneIPv6Reservation{{Node: 1, Address: "2001:db8::10"}}}}}}, Links: []ZoneLink{{From: "a", To: "a", Services: []string{"tcp:443", "udp:53", "icmp:*", "tcp:8000-8100"}}}}
	require.NoError(t, valid.Validate())
}
