package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOwnExitRulePriorityAndIntersection(t *testing.T) {
	owner := uint(1)
	view := (&Node{ID: 7, UserID: &owner}).View()
	model := &NetworkZones{Zones: []NetworkZone{{ID: "z", Name: "Z", Users: []UserID{1}, OwnExits: &OwnExitRule{Mode: "deny"}}}, OwnExitOverrides: []OwnExitOverride{
		{Group: "group:a", Rule: OwnExitRule{Mode: "allow", Services: []string{"tcp:440-450", "udp:53"}}},
		{Group: "group:b", Rule: OwnExitRule{Mode: "allow", Services: []string{"tcp:443-460"}}},
	}}
	decision := model.ResolveOwnExits(view, []string{"group:a", "group:b"})
	require.Equal(t, []string{"tcp:443-450"}, decision.Rule.Services)
	model.OwnExitOverrides = append(model.OwnExitOverrides, OwnExitOverride{User: 1, Rule: OwnExitRule{Mode: "allow", Services: []string{"tcp:22"}}})
	require.Equal(t, "Benutzer", model.ResolveOwnExits(view, []string{"group:a"}).Source)
	model.OwnExitOverrides = append(model.OwnExitOverrides, OwnExitOverride{Node: 7, Rule: OwnExitRule{Mode: "allow", Services: []string{"udp:123"}}})
	require.Equal(t, []string{"udp:123"}, model.ResolveOwnExits(view, nil).Rule.Services)
	model.Zones[0].OwnExits = &OwnExitRule{Mode: "deny", Locked: true}
	decision = model.ResolveOwnExits(view, []string{"group:a"})
	require.True(t, decision.Rule.Locked)
	require.Equal(t, "Zone: Z", decision.Source)

	model.Zones[0].OwnExits = nil
	model.OwnExitOverrides = model.OwnExitOverrides[:2]
	model.OwnExitOverrides[1].Rule = OwnExitRule{Mode: "deny"}
	require.Equal(t, "deny", model.ResolveOwnExits(view, []string{"group:a", "group:b"}).Rule.Mode)
	model.OwnExitOverrides[1].Rule = OwnExitRule{Mode: "inherit"}
	require.Equal(t, []string{"tcp:440-450", "udp:53"}, model.ResolveOwnExits(view, []string{"group:a", "group:b"}).Rule.Services)

	tagged := (&Node{ID: 7, UserID: &owner, Tags: []string{"tag:service"}}).View()
	require.Equal(t, "deny", model.ResolveOwnExits(tagged, []string{"group:a"}).Rule.Mode, "tagged identity must not inherit creator groups")
	require.NoError(t, model.Validate())
	require.Error(t, (OwnExitRule{Mode: "allow", Services: []string{"*"}, Locked: true}).Validate(true))
	require.Error(t, (OwnExitRule{Mode: "deny", Locked: true}).Validate(false))
}

func TestOwnExitServiceIntersections(t *testing.T) {
	require.Equal(t, []string{"tcp:443"}, IntersectOwnExitServices([]string{"*"}, []string{"tcp:443"}))
	require.Equal(t, []string{"tcp:445-450"}, IntersectOwnExitServices([]string{"tcp:440-450"}, []string{"tcp:445-460"}))
	require.Empty(t, IntersectOwnExitServices([]string{"tcp:443"}, []string{"udp:443"}))
	require.Equal(t, []string{"icmp:*"}, IntersectOwnExitServices([]string{"icmp:*"}, []string{"icmp:*"}))
}
