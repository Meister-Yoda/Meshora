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
	source, loaded, err := h.state.PolicyEditorState()
	page := templates.AdminZonesInfo{SourceRevision: source.Revision, LoadedRevision: loaded.Revision, Pending: source.Text != loaded.Text, Error: message, Writable: err == nil}

	model, parseErr := types.NetworkZonesFromPolicy(source.Text)
	if err != nil || parseErr != nil {
		page.Error = "Die gespeicherte Policy ist nicht lesbar. Bitte im Policy-Editor prüfen."
		page.Writable = false
	}

	page.Enabled = model != nil
	if model == nil {
		model = &types.NetworkZones{}
	}

	page.Model = *model
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

		h.renderZones(w, r, status, err.Error())

		return
	}

	target := "/zones"
	if id := r.PostForm.Get("zone"); zoneIndex(model, id) >= 0 {
		target += "?zone=" + url.QueryEscape(id)
	}

	h.redirect(w, r, target, success("Zonenentwurf gespeichert und geprüft. Erst Aktivieren wendet die Änderungen an."))
}

func (h *Handler) mutateZones(model *types.NetworkZones, r *http.Request) error {
	form := r.PostForm

	index := zoneIndex(model, form.Get("zone"))
	switch form.Get("op") {
	case "zone":
		return h.upsertNetworkZone(model, r, index)

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
	zone := types.NetworkZone{ID: form.Get("zone"), Name: strings.TrimSpace(form.Get("name")), Subnets: dnsLines(form.Get("subnets")), Pool4: strings.TrimSpace(form.Get("pool4")), Pool6: strings.TrimSpace(form.Get("pool6")), Exits: form["exits"]}

	var err error

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
		model.Zones[index] = zone
	} else {
		if zone.ID != "" {
			return fmt.Errorf("%w: Zone existiert nicht mehr", errZoneForm)
		}

		zone.ID = randomToken()
		form.Set("zone", zone.ID)
		model.Zones = append(model.Zones, zone)
	}

	return nil
}
