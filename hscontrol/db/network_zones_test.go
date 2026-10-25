package db

import (
	"net/netip"
	"sync"
	"testing"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/require"
)

func TestNetworkZoneAllocation(t *testing.T) {
	allocator, err := NewIPAllocator(nil, mpp("100.64.0.0/24"), mpp("fd7a:115c:a1e0::/120"), types.IPAllocationStrategySequential)
	require.NoError(t, err)

	model := &types.NetworkZones{Zones: []types.NetworkZone{{ID: "a", Name: "A", Pool4: "100.64.0.0/28", Pool6: "fd7a:115c:a1e0::/124"}}}
	v4, v6, err := allocator.NextForZone(nil, model)
	require.NoError(t, err)
	require.False(t, netip.MustParsePrefix(model.Zones[0].Pool4).Contains(*v4))
	require.False(t, netip.MustParsePrefix(model.Zones[0].Pool6).Contains(*v6))
	zone4, zone6, err := allocator.NextForZone(&model.Zones[0], model)
	require.NoError(t, err)
	require.Equal(t, "100.64.0.1", zone4.String())
	require.Equal(t, "fd7a:115c:a1e0::1", zone6.String())
	// Retrying after restart must not hand out a previously persisted address:
	// the allocator's global used-IP set serves both default and zone pools.
	next4, next6, err := allocator.NextForZone(&model.Zones[0], model)
	require.NoError(t, err)
	require.NotEqual(t, *zone4, *next4)
	require.NotEqual(t, *zone6, *next6)
}

func TestNetworkZoneAllocationAtomicExhaustion(t *testing.T) {
	allocator, err := NewIPAllocator(nil, mpp("100.64.0.0/24"), mpp("fd7a:115c:a1e0::/120"), types.IPAllocationStrategySequential)
	require.NoError(t, err)

	model := &types.NetworkZones{Zones: []types.NetworkZone{{ID: "a", Name: "A", Pool4: "100.64.0.0/29", Pool6: "fd7a:115c:a1e0::/126"}}}
	for range 2 {
		_, _, err = allocator.NextForZone(&model.Zones[0], model)
		require.NoError(t, err)
	}

	_, _, err = allocator.NextForZone(&model.Zones[0], model)
	require.ErrorIs(t, err, ErrCouldNotAllocateIP)

	model.Zones[0].Pool6 = ""
	v4, _, err := allocator.NextForZone(&model.Zones[0], model)
	require.NoError(t, err)
	require.Equal(t, "100.64.0.3", v4.String(), "failed dual-stack allocation must not consume IPv4")
}

func TestNetworkZoneAllocationConcurrent(t *testing.T) {
	allocator, err := NewIPAllocator(nil, mpp("100.64.0.0/24"), nil, types.IPAllocationStrategyRandom)
	require.NoError(t, err)

	model := &types.NetworkZones{Zones: []types.NetworkZone{{ID: "a", Name: "A", Pool4: "100.64.0.0/25"}}}

	var wg sync.WaitGroup

	addresses := make(chan netip.Addr, 32)
	errs := make(chan error, 32)

	for range 32 {
		wg.Go(func() {
			ip, _, err := allocator.NextForZone(&model.Zones[0], model)
			if err != nil {
				errs <- err
				return
			}

			addresses <- *ip
		})
	}

	wg.Wait()
	close(addresses)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	seen := map[netip.Addr]bool{}
	for ip := range addresses {
		require.False(t, seen[ip])
		seen[ip] = true
	}

	require.Len(t, seen, 32)
}

func TestNetworkZoneBackfillPreservesExistingFamily(t *testing.T) {
	db := dbForTest(t)
	user := types.User{Name: "alice"}
	require.NoError(t, db.DB.Create(&user).Error)

	original := netip.MustParseAddr("100.64.0.50")
	node := types.Node{Hostname: "laptop", GivenName: "laptop", UserID: &user.ID, IPv4: &original}
	require.NoError(t, db.DB.Create(&node).Error)
	allocator, err := NewIPAllocator(db, mpp("100.64.0.0/24"), mpp("fd7a:115c:a1e0::/120"), types.IPAllocationStrategySequential)
	require.NoError(t, err)

	model := &types.NetworkZones{Zones: []types.NetworkZone{{ID: "a", Name: "A", Users: []types.UserID{types.UserID(user.ID)}, Pool4: "100.64.0.0/28", Pool6: "fd7a:115c:a1e0::/124"}}}
	changes, err := db.BackfillNodeIPs(allocator, model)
	require.NoError(t, err)
	require.Len(t, changes, 1)

	var saved types.Node
	require.NoError(t, db.DB.First(&saved, node.ID).Error)
	require.Equal(t, original, *saved.IPv4)
	require.True(t, netip.MustParsePrefix(model.Zones[0].Pool6).Contains(*saved.IPv6))
	// Backfilling IPv6 must not reserve an unused address in the IPv4 zone pool.
	v4, _, err := allocator.NextForZone(&model.Zones[0], model)
	require.NoError(t, err)
	require.Equal(t, "100.64.0.1", v4.String())
}
