package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/juanfont/headscale/hscontrol/state"
	"github.com/juanfont/headscale/hscontrol/templates"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/tailscale/hujson"
)

var errZoneForm = errors.New("invalid zone form")

func (h *Handler) zonesPage(w http.ResponseWriter, r *http.Request) {
	h.renderZones(w, r, http.StatusOK, "")
}

func (h *Handler) renderZones(w http.ResponseWriter, r *http.Request, status int, message string) {
	w.Header().Set("Content-Security-Policy", strings.Replace(adminCSP, "default-src 'none';", "default-src 'none'; script-src 'self';", 1))
	source, loaded, err := h.state.PolicyEditorState()
	page := templates.AdminZonesInfo{OpenZoneEditor: r.URL.Query().Get("new") == "1", SourceRevision: source.Revision, LoadedRevision: loaded.Revision, Pending: source.Text != loaded.Text, Error: message, Writable: err == nil}

	model, parseErr := types.NetworkZonesFromPolicy(source.Text)
	if err != nil || parseErr != nil {
		page.Error = "Die gespeicherte Policy ist nicht lesbar. Bitte im Policy-Editor prüfen."
		page.Writable = false
	}

	page.Enabled = model != nil
	if model == nil {
		model = &types.NetworkZones{}
	}

	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if r.Method == http.MethodPost {
		from, to = r.PostForm.Get("from"), r.PostForm.Get("to")
	}

	if zoneIndex(model, from) >= 0 && (zoneIndex(model, to) >= 0 || to == types.OwnExitsTarget) {
		page.FilterFrom, page.FilterTo = from, to
	}

	if r.Method == http.MethodPost && r.PostForm.Get("op") == "zone" && message != "" {
		page.ZoneForm = &templates.AdminZoneForm{
			ID: r.PostForm.Get("zone"), Name: r.PostForm.Get("name"), BlockOwnExits: r.PostForm.Get("block_own_exits") == "on", AddressRange: r.PostForm.Get("address_range"),
			Nodes: slices.Clone(r.PostForm["nodes"]), Users: slices.Clone(r.PostForm["users"]),
			Subnets: r.PostForm.Get("subnets"), Pool4: r.PostForm.Get("pool4"), Pool6: r.PostForm.Get("pool6"),
			Exits: slices.Clone(r.PostForm["exits"]), ReplaceNetwork: r.PostForm.Get("replace_network") == "on",
		}
		// A rejected stale form must retain its original concurrency tokens.
		// Merely redisplaying attempted values must not authorize an overwrite.
		page.SourceRevision = r.PostForm.Get("source_revision")
		page.LoadedRevision, _ = strconv.ParseUint(r.PostForm.Get("loaded_revision"), 10, 64)
	}

	if h.runtimeConfig.PrefixV4 != nil {
		page.MeshPrefix4 = h.runtimeConfig.PrefixV4.String()
	}

	if h.runtimeConfig.PrefixV6 != nil {
		page.MeshPrefix6 = h.runtimeConfig.PrefixV6.String()
	}
	page.Model = *model
	page.PolicyGroups, _ = ownExitPolicyGroups(source.Text)

	page.SelectedOwnGroup = r.URL.Query().Get("own_group")
	if r.Method == http.MethodPost && (r.PostForm.Get("op") == "own-exits" || r.PostForm.Get("op") == "own-exit-group") && message != "" {
		page.SelectedOwnGroup = r.PostForm.Get("group")
		page.OwnAttempt = &types.OwnExitRule{Mode: r.PostForm.Get("own_mode"), Services: strings.FieldsFunc(r.PostForm.Get("own_services"), func(c rune) bool { return c == ',' || c == ' ' || c == '\n' || c == '\r' }), Locked: r.PostForm.Get("own_locked") == "on"}
		page.SourceRevision = r.PostForm.Get("source_revision")
		page.LoadedRevision, _ = strconv.ParseUint(r.PostForm.Get("loaded_revision"), 10, 64)
	}

	active, _ := types.NetworkZonesFromPolicy(loaded.Text)
	page.Active = active != nil

	selected := r.URL.Query().Get("zone")
	if r.Method == http.MethodPost {
		selected = r.PostForm.Get("zone")
	}

	for _, zone := range model.Zones {
		page.Zones = append(page.Zones, templates.AdminChoice{Value: zone.ID, Label: zone.Name})
		if zone.ID == selected {
			selectedZone := zone
			page.Selected = &selectedZone
		}
	}

	users, userErr := h.state.ListAllUsers()
	if userErr != nil {
		page.Writable = false
		page.Error = "Benutzer konnten nicht geladen werden."
	}

	for _, user := range users {
		view := user.View()
		page.Users = append(page.Users, templates.AdminChoice{Value: strconv.FormatUint(uint64(view.ID()), 10), Label: view.Username()})
	}

	tags := map[string]bool{}

	for _, node := range h.state.ListNodes().All() {
		id := strconv.FormatUint(node.ID().Uint64(), 10)

		page.Nodes = append(page.Nodes, templates.AdminChoice{Value: id, Label: node.GivenName() + " (#" + id + ")"})
		if node.IsExitNode() {
			for _, tag := range node.Tags().All() {
				tags[tag] = true
			}
		}

		zone := model.ZoneForNode(node)

		row := templates.AdminZoneDevice{ID: id, Name: node.GivenName(), Zone: "Keine Zone", Exits: "Keine"}
		if zone != nil {
			row.Zone = zone.Name
		}

		if exits := model.ExitsForNode(node); len(exits) > 0 {
			row.Exits = strings.Join(exits, ", ")
		}

		page.Devices = append(page.Devices, row)
	}
	// Keep configured tags visible when a gateway is offline, removed or no
	// longer approved. Saving never silently drops an existing restriction.
	for _, zone := range model.Zones {
		for _, tag := range zone.Exits {
			tags[tag] = true
		}

		for _, pool := range zone.IPv6Pools {
			tags[pool.Gateway] = true
		}
	}

	for _, limit := range model.ExitLimits {
		for _, tag := range limit.Exits {
			tags[tag] = true
		}
	}

	for tag := range tags {
		page.Gateways = append(page.Gateways, templates.AdminChoice{Value: tag, Label: tag})
	}

	slices.SortFunc(page.Gateways, func(a, b templates.AdminChoice) int { return strings.Compare(a.Value, b.Value) })
	writeHTML(w, status, templates.AdminZones(h.pageInfo(r), page).Render())
}

func zonePolicyText(source string, model *types.NetworkZones) (string, error) {
	if strings.TrimSpace(source) == "" {
		source = "{}"
	}

	ast, err := hujson.Parse([]byte(source))
	if err != nil {
		return "", err
	}

	if _, ok := ast.Value.(*hujson.Object); !ok {
		return "", state.ErrPolicyEditorInput
	}
	// JSON Patch replaces only the owned network fields. SSH, tag owners,
	// auto-approvers, tests, comments and all other policy sections survive.
	patch := []map[string]any{{"op": "add", "path": "/networkZones", "value": model}}

	for _, field := range []string{"acls", "grants"} {
		if ast.Find("/"+field) != nil {
			patch = append(patch, map[string]any{"op": "remove", "path": "/" + field})
		}
	}

	raw, err := json.Marshal(patch)
	if err != nil {
		return "", err
	}

	err = ast.Patch(raw)
	if err != nil {
		return "", err
	}

	ast.Format()

	return string(ast.Pack()), nil
}

func zoneIndex(model *types.NetworkZones, id string) int {
	return slices.IndexFunc(model.Zones, func(z types.NetworkZone) bool { return z.ID == id })
}

func parseZoneIDs[T ~uint64](values []string) ([]T, error) {
	var ids []T

	for _, raw := range values {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || id == 0 {
			return nil, fmt.Errorf("%w: Ungültige Benutzer- oder Geräte-ID", errZoneForm)
		}

		ids = append(ids, T(id))
	}

	return ids, nil
}

func (h *Handler) updateZones(w http.ResponseWriter, r *http.Request) {
	source, loaded, err := h.state.PolicyEditorState()

	revision, revisionErr := strconv.ParseUint(r.PostForm.Get("loaded_revision"), 10, 64)
	if err != nil || revisionErr != nil || source.Revision != r.PostForm.Get("source_revision") || revision != loaded.Revision {
		h.renderZones(w, r, http.StatusConflict, "Die Policy wurde geändert. Seite neu laden und Änderungen erneut prüfen.")
		return
	}

	model, err := types.NetworkZonesFromPolicy(source.Text)
	if err != nil {
		h.renderZones(w, r, http.StatusBadRequest, err.Error())
		return
	}

	if model == nil {
		if r.PostForm.Get("replace_network") != "on" {
			h.renderZones(w, r, http.StatusBadRequest, "Bestätige den Wechsel zum Zonenmodus: vorhandene ACLs und Grants werden ersetzt.")
			return
		}

		model = &types.NetworkZones{}
	}

	err = h.mutateZones(model, r)
	if err == nil {
		err = model.Validate()
	}

	var text string
	if err == nil {
		text, err = zonePolicyText(source.Text, model)
	}

	if err == nil {
		_, err = h.state.SavePolicyDraft(text, source.Revision, loaded.Revision)
	}

	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, state.ErrPolicyEditorConflict) {
			status = http.StatusConflict
		}

		h.renderZones(w, r, status, strings.TrimPrefix(err.Error(), errZoneForm.Error()+": "))

		return
	}

	target := "/zones"

	id := r.PostForm.Get("zone")
	if id == "" && r.PostForm.Get("op") == "zone" && len(model.Zones) > 0 {
		id = model.Zones[len(model.Zones)-1].ID
	}

	if r.PostForm.Get("op") != "zone" && zoneIndex(model, id) >= 0 {
		target += "?zone=" + url.QueryEscape(id)
	}

	if r.PostForm.Get("op") == "own-exit-group" {
		target = "/zones?" + url.Values{"own_group": {r.PostForm.Get("group")}}.Encode() + "#own-exit-groups"
	}

	if op := r.PostForm.Get("op"); op == "link" || op == "delete-link" || op == "own-exits" {
		from, to := r.PostForm.Get("from"), r.PostForm.Get("to")
		if zoneIndex(model, from) >= 0 && (zoneIndex(model, to) >= 0 || to == types.OwnExitsTarget) {
			target = "/zones?" + url.Values{"from": {from}, "to": {to}}.Encode() + "#zone-policy-list"
		}
	}

	h.redirect(w, r, target, success("Zonenentwurf gespeichert und geprüft. Erst Aktivieren wendet die Änderungen an."))
}

func (h *Handler) mutateZones(model *types.NetworkZones, r *http.Request) error {
	form := r.PostForm

	index := zoneIndex(model, form.Get("zone"))
	switch form.Get("op") {
	case "zone":
		return h.upsertNetworkZone(model, r, index)

	case "own-exits":
		return mutateOwnExitZone(model, r)
	case "own-exit-group":
		rule, err := parseOwnExitForm(r, false)
		if err != nil {
			return err
		}

		setOwnExitOverride(model, types.OwnExitOverride{Group: form.Get("group"), Rule: rule})
	case "delete-zone":
		if index < 0 {
			return fmt.Errorf("%w: Zone existiert nicht mehr", errZoneForm)
		}

		id := model.Zones[index].ID
		model.Zones = slices.Delete(model.Zones, index, index+1)
		model.Links = slices.DeleteFunc(model.Links, func(link types.ZoneLink) bool { return link.From == id || link.To == id })
	case "link", "delete-link":
		from, to := form.Get("from"), form.Get("to")

		model.Links = slices.DeleteFunc(model.Links, func(link types.ZoneLink) bool { return link.From == from && link.To == to })
		if form.Get("op") == "link" {
			model.Links = append(model.Links, types.ZoneLink{From: from, To: to, Services: strings.FieldsFunc(form.Get("services"), func(c rune) bool { return c == ',' || c == ' ' || c == '\n' || c == '\r' })})
		}
	case "limit", "delete-limit":
		kind, raw, ok := strings.Cut(form.Get("target"), ":")

		id, err := strconv.ParseUint(raw, 10, 64)
		if !ok || err != nil || id == 0 || (kind != "node" && kind != "user") {
			return fmt.Errorf("%w: Benutzer oder Gerät für die Exit-Einschränkung wählen", errZoneForm)
		}

		limit := types.ZoneExitLimit{Exits: form["exits"]}
		if kind == "node" {
			limit.Node = types.NodeID(id)
			if _, exists := h.state.GetNodeByID(limit.Node); !exists && form.Get("op") != "delete-limit" {
				return fmt.Errorf("%w: Gerät existiert nicht mehr", errZoneForm)
			}
		} else {
			limit.User = types.UserID(id)

			_, err = h.state.GetUserByID(limit.User)
			if err != nil && form.Get("op") != "delete-limit" {
				return fmt.Errorf("%w: Benutzer existiert nicht mehr", errZoneForm)
			}
		}

		model.ExitLimits = slices.DeleteFunc(model.ExitLimits, func(old types.ZoneExitLimit) bool { return old.Node == limit.Node && old.User == limit.User })
		if form.Get("op") == "limit" {
			model.ExitLimits = append(model.ExitLimits, limit)
		}
	case "pool", "delete-pool", "reservation", "delete-reservation":
		return h.mutateZoneIPv6(model, r, index)

	default:
		return fmt.Errorf("%w: Unbekannte Zonenaktion %q", errZoneForm, form.Get("op"))
	}

	return nil
}

func (h *Handler) activateZones(w http.ResponseWriter, r *http.Request) {
	revision, err := strconv.ParseUint(r.PostForm.Get("loaded_revision"), 10, 64)
	if err != nil {
		h.renderZones(w, r, http.StatusConflict, "Ungültige Policy-Version. Seite neu laden.")
		return
	}

	result, err := h.state.ActivatePolicySource(r.PostForm.Get("source_revision"), revision)
	h.change(result.Changes...)

	if err != nil {
		message := "Aktivierung fehlgeschlagen; die bisherige Policy bleibt aktiv: " + err.Error()
		if result.Applied {
			message = "Die neue Policy ist aktiv, aber die Nachverarbeitung ist fehlgeschlagen: " + err.Error()
		}

		h.renderZones(w, r, http.StatusConflict, message)

		return
	}

	h.redirect(w, r, "/zones", success("Zonen-Policy aktiviert. Öffentliche IPv6-Reservierungen benötigen weiterhin Gateway- und Client-Einrichtung."))
}

func (h *Handler) mutateZoneIPv6(model *types.NetworkZones, r *http.Request, index int) error {
	form := r.PostForm

	if index < 0 {
		return fmt.Errorf("%w: Zone für das IPv6-Präfix wählen", errZoneForm)
	}

	zone := &model.Zones[index]
	prefix := strings.TrimSpace(form.Get("prefix"))
	poolIndex := slices.IndexFunc(zone.IPv6Pools, func(p types.ZoneIPv6Pool) bool { return p.Prefix == prefix })

	switch form.Get("op") {
	case "pool":
		pool := types.ZoneIPv6Pool{Prefix: prefix, Gateway: form.Get("gateway")}
		if poolIndex >= 0 {
			pool.Reservations = zone.IPv6Pools[poolIndex].Reservations
			zone.IPv6Pools[poolIndex] = pool
		} else {
			zone.IPv6Pools = append(zone.IPv6Pools, pool)
		}
	case "delete-pool":
		if poolIndex < 0 {
			return fmt.Errorf("%w: IPv6-Präfix existiert nicht mehr", errZoneForm)
		}

		zone.IPv6Pools = slices.Delete(zone.IPv6Pools, poolIndex, poolIndex+1)
	default:
		if poolIndex < 0 {
			return fmt.Errorf("%w: IPv6-Präfix existiert nicht mehr", errZoneForm)
		}

		id, err := strconv.ParseUint(form.Get("node"), 10, 64)
		if err != nil || id == 0 {
			return fmt.Errorf("%w: Gerät für die IPv6-Reservierung wählen", errZoneForm)
		}

		nodeID := types.NodeID(id)
		node, ok := h.state.GetNodeByID(nodeID)

		member := model.ZoneForNode(node)
		if form.Get("op") == "reservation" && (!ok || member == nil || member.ID != zone.ID) {
			return fmt.Errorf("%w: Das Gerät muss Mitglied dieser Zone sein", errZoneForm)
		}

		pool := &zone.IPv6Pools[poolIndex]
		pool.Reservations = slices.DeleteFunc(pool.Reservations, func(res types.ZoneIPv6Reservation) bool { return res.Node == nodeID })

		if form.Get("op") == "reservation" {
			address, err := netip.ParseAddr(strings.TrimSpace(form.Get("address")))
			if err != nil {
				return fmt.Errorf("%w: Ungültige IPv6-Adresse", errZoneForm)
			}

			pool.Reservations = append(pool.Reservations, types.ZoneIPv6Reservation{Node: nodeID, Address: address.String()})
		}
	}

	return nil
}

func (h *Handler) upsertNetworkZone(model *types.NetworkZones, r *http.Request, index int) error {
	form := r.PostForm

	name := strings.TrimSpace(form.Get("name"))
	if name == "" {
		return fmt.Errorf("%w: Bitte gib einen Namen für die Zone ein", errZoneForm)
	}

	if len(name) > 128 {
		return fmt.Errorf("%w: Der Zonenname ist zu lang. Bitte verwende einen kürzeren Namen", errZoneForm)
	}

	zone := types.NetworkZone{ID: form.Get("zone"), Name: name, Subnets: dnsLines(form.Get("subnets")), Pool4: strings.TrimSpace(form.Get("pool4")), Pool6: strings.TrimSpace(form.Get("pool6")), Exits: form["exits"]}

	if form.Get("simple_zone") == "on" {
		for _, raw := range strings.FieldsFunc(form.Get("address_range"), func(c rune) bool { return c == ',' || c == '\n' || c == ' ' }) {
			prefix, parseErr := netip.ParsePrefix(raw)
			if parseErr != nil {
				return fmt.Errorf("%w: Bitte einen gültigen IP-Adressbereich angeben", errZoneForm)
			}

			if prefix.Addr().Is4() {
				if zone.Pool4 != "" {
					return fmt.Errorf("%w: Nur einen IPv4-Bereich angeben", errZoneForm)
				}

				zone.Pool4 = raw
			} else {
				if zone.Pool6 != "" {
					return fmt.Errorf("%w: Nur einen IPv6-Bereich angeben", errZoneForm)
				}

				zone.Pool6 = raw
			}
		}
	}

	zone.BlockOwnExits = form.Get("block_own_exits") == "on"

	err := h.validateZonePools(zone)
	if err != nil {
		return err
	}

	zone.Nodes, err = parseZoneIDs[types.NodeID](form["nodes"])
	if err != nil {
		return err
	}

	zone.Users, err = parseZoneIDs[types.UserID](form["users"])
	if err != nil {
		return err
	}

	for _, id := range zone.Nodes {
		if _, ok := h.state.GetNodeByID(id); !ok {
			return fmt.Errorf("%w: Ein ausgewähltes Gerät existiert nicht mehr", errZoneForm)
		}
	}

	for _, id := range zone.Users {
		_, err = h.state.GetUserByID(id)
		if err != nil {
			return fmt.Errorf("%w: Ein ausgewählter Benutzer existiert nicht mehr", errZoneForm)
		}
	}

	if index >= 0 {
		zone.IPv6Pools = model.Zones[index].IPv6Pools

		zone.OwnExits = model.Zones[index].OwnExits
		if !form.Has("nodes") {
			zone.Nodes = model.Zones[index].Nodes
		}

		if !form.Has("users") {
			zone.Users = model.Zones[index].Users
		}

		if !form.Has("subnets") {
			zone.Subnets = model.Zones[index].Subnets
		}
		model.Zones[index] = zone
	} else {
		if zone.ID != "" {
			return fmt.Errorf("%w: Zone existiert nicht mehr", errZoneForm)
		}

		zone.ID = randomToken()
		model.Zones = append(model.Zones, zone)
	}

	return nil
}

func (h *Handler) validateZonePools(zone types.NetworkZone) error {
	for family, raw := range []string{zone.Pool4, zone.Pool6} {
		if raw == "" {
			continue
		}

		label := "IPv4"
		allowed := netip.MustParsePrefix("100.64.0.0/10")
		configured := h.runtimeConfig.PrefixV4

		if family == 1 {
			label = "IPv6"
			allowed = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
			configured = h.runtimeConfig.PrefixV6
		}

		prefix, err := netip.ParsePrefix(raw)
		if err != nil || prefix != prefix.Masked() {
			return fmt.Errorf("%w: Der Mesh-%s-Pool ist kein gültiges Netzpräfix", errZoneForm, label)
		}

		if prefix.Addr().BitLen() != allowed.Addr().BitLen() || prefix.Bits() < allowed.Bits() || !allowed.Contains(prefix.Addr()) {
			return fmt.Errorf("%w: Der Mesh-%s-Pool muss innerhalb von %s liegen. Ein vorhandenes LAN wie 10.3.1.0/24 gehört in das Feld Geroutete Subnetze", errZoneForm, label, allowed)
		}

		if configured == nil {
			return fmt.Errorf("%w: Die Mesh-%s-Adressvergabe ist auf diesem Server nicht aktiviert. Lass dieses Feld leer", errZoneForm, label)
		}

		if prefix.Bits() < configured.Bits() || !configured.Contains(prefix.Addr()) {
			return fmt.Errorf("%w: Der Mesh-%s-Pool muss innerhalb des auf diesem Server konfigurierten Bereichs %s liegen", errZoneForm, label, configured)
		}
	}

	return nil
}

func (h *Handler) zoneAssignment(user types.UserID, node types.NodeID) *templates.AdminZoneAssignment {
	source, loaded, err := h.state.PolicyEditorState()

	result := &templates.AdminZoneAssignment{SourceRevision: source.Revision, LoadedRevision: loaded.Revision, Effective: "Standardzuordnung"}
	if err != nil {
		return result
	}

	model, err := types.NetworkZonesFromPolicy(source.Text)
	if err != nil || model == nil {
		return result
	}

	result.Available = true
	if node == 0 {
		result.ProviderDirectBlocked = slices.Contains(model.ExitProviderUsers, user)
		result.ProviderBlocked = result.ProviderDirectBlocked
	} else {
		result.ProviderDirectBlocked = slices.Contains(model.ExitProviderNodes, node)
		if view, ok := h.state.GetNodeByID(node); ok {
			result.ProviderBlocked = model.ExitProviderBlocked(view)
		}
	}

	for _, zone := range model.Zones {
		result.Choices = append(result.Choices, templates.AdminChoice{Value: zone.ID, Label: zone.Name})
		if (node != 0 && slices.Contains(zone.Nodes, node)) || (node == 0 && slices.Contains(zone.Users, user)) {
			result.Selected = zone.ID

			result.Effective = zone.Name
			if node == 0 {
				result.ProviderBlocked = zone.BlockOwnExits
			}
		}
	}

	if node != 0 {
		if view, ok := h.state.GetNodeByID(node); ok {
			if zone := model.ZoneForNode(view); zone != nil {
				result.Effective = zone.Name
			}
		}
	}

	result.OwnRule, result.OwnDecision, err = h.state.InspectOwnExitDraft(source.Text, user, node)
	if err != nil {
		result.OwnDecision = types.OwnExitDecision{Rule: types.OwnExitRule{Mode: "deny"}, Source: "Auflösung fehlgeschlagen"}
	}

	return result
}

func (h *Handler) assignUserZone(w http.ResponseWriter, r *http.Request) {
	h.saveZoneAssignment(w, r, false)
}

func (h *Handler) assignNodeZone(w http.ResponseWriter, r *http.Request) {
	h.saveZoneAssignment(w, r, true)
}

func (h *Handler) saveZoneAssignment(w http.ResponseWriter, r *http.Request, isNode bool) {
	id, ok := urlID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	path := fmt.Sprintf("/users/%d", id)
	if isNode {
		path = fmt.Sprintf("/nodes/%d", id)
		if _, exists := h.state.GetNodeByID(types.NodeID(id)); !exists {
			http.NotFound(w, r)
			return
		}
	} else {
		_, err := h.state.GetUserByID(types.UserID(id))
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}

	source, loaded, err := h.state.PolicyEditorState()

	revision, parseErr := strconv.ParseUint(r.PostForm.Get("loaded_revision"), 10, 64)
	if err != nil || parseErr != nil || revision != loaded.Revision || source.Revision != r.PostForm.Get("source_revision") {
		h.redirect(w, r, path, failure("Die Policy wurde geändert. Bitte die Detailseite neu laden."))
		return
	}

	model, err := types.NetworkZonesFromPolicy(source.Text)
	if err != nil || model == nil {
		h.redirect(w, r, path, failure("Zuerst eine Zone unter Zonen erstellen."))
		return
	}

	if strings.HasSuffix(r.URL.Path, "/exit-provider") {
		value := r.PostForm.Get("blocked")
		if value != "true" && value != "false" {
			h.redirect(w, r, path, failure("Ungültige Einstellung für eigene Exit Nodes."))
			return
		}

		if isNode {
			model.ExitProviderNodes = slices.DeleteFunc(model.ExitProviderNodes, func(old types.NodeID) bool { return old == types.NodeID(id) })
			if value == "true" {
				model.ExitProviderNodes = append(model.ExitProviderNodes, types.NodeID(id))
			}
		} else {
			model.ExitProviderUsers = slices.DeleteFunc(model.ExitProviderUsers, func(old types.UserID) bool { return old == types.UserID(id) })
			if value == "true" {
				model.ExitProviderUsers = append(model.ExitProviderUsers, types.UserID(id))
			}
		}
	} else if strings.HasSuffix(r.URL.Path, "/own-exits") {
		rule, ruleErr := parseOwnExitForm(r, false)
		if ruleErr != nil {
			h.redirect(w, r, path, failure("Exit-Regel nicht gespeichert: %v", ruleErr))
			return
		}

		override := types.OwnExitOverride{Rule: rule}
		if isNode {
			override.Node = types.NodeID(id)
		} else {
			override.User = types.UserID(id)
		}

		setOwnExitOverride(model, override)
	} else {
		target := r.PostForm.Get("zone")
		if target != "" && zoneIndex(model, target) < 0 {
			h.redirect(w, r, path, failure("Die ausgewählte Zone existiert nicht mehr."))
			return
		}

		for i := range model.Zones {
			zone := &model.Zones[i]
			if isNode {
				zone.Nodes = slices.DeleteFunc(zone.Nodes, func(old types.NodeID) bool { return old == types.NodeID(id) })
				if zone.ID == target {
					zone.Nodes = append(zone.Nodes, types.NodeID(id))
				}
			} else {
				zone.Users = slices.DeleteFunc(zone.Users, func(old types.UserID) bool { return old == types.UserID(id) })
				if zone.ID == target {
					zone.Users = append(zone.Users, types.UserID(id))
				}
			}
		}
	}

	text, err := zonePolicyText(source.Text, model)
	if err == nil {
		_, err = h.state.SavePolicyDraft(text, source.Revision, loaded.Revision)
	}

	if err != nil {
		h.redirect(w, r, path, failure("Zuordnung nicht gespeichert: %v", err))
		return
	}

	h.redirect(w, r, path, success("Einstellung als Entwurf gespeichert. Unter Zonen aktivieren."))
}
