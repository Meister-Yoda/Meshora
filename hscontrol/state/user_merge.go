package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
	"github.com/tailscale/hujson"
)

var (
	errMergePolicyReference = errors.New("Policy-Verweis vor dem Zusammenführen auf die gemeinsame E-Mail umstellen")
	errMerge1               = errors.New("die Quell-UID ist noch einer Zone zugeordnet; Zuordnung vor dem Zusammenführen übertragen")
	errMerge2               = errors.New("Exit-Einschränkung der Quell-UID zuerst übertragen")
	errMerge3               = errors.New("Exit-Regel der Quell-UID zuerst übertragen")
	errMerge4               = errors.New("Exit-Anbietersperre der Quell-UID zuerst übertragen")
)

func (s *State) MergeUserAccounts(sourceID, targetID types.UserID, keepTargetPassword bool) (change.Change, error) {
	s.policyMu.Lock()
	defer s.policyMu.Unlock()

	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	source, err := s.db.GetUserByID(sourceID)
	if err != nil {
		return change.Change{}, err
	}

	target, err := s.db.GetUserByID(targetID)
	if err != nil {
		return change.Change{}, err
	}

	draft, err := s.policyEditorSource()
	if err != nil {
		return change.Change{}, err
	}

	for _, text := range []string{draft.Text, s.polMan.PolicySnapshot().Text} {
		err = checkMergePolicy(text, source, target)
		if err != nil {
			return change.Change{}, err
		}
	}

	kept, err := s.db.MergeUsers(sourceID, targetID, keepTargetPassword)
	if err != nil {
		return change.Change{}, err
	}

	generation := s.polMan.NodesGeneration()
	for _, node := range s.ListNodes().All() {
		if node.TypedUserID() == sourceID {
			s.nodeStore.UpdateNode(node.ID(), func(n *types.Node) { uid := kept.ID; n.UserID = &uid; n.User = kept })
		}
	}

	_, err = s.updatePolicyManagerUsers()
	if err != nil {
		return change.PolicyChange(), err
	}

	_, err = s.updatePolicyManagerNodes(generation)

	return change.PolicyChange(), err
}

func checkMergePolicy(text string, source, target *types.User) error {
	model, err := types.NetworkZonesFromPolicy(text)
	if err != nil {
		return err
	}

	if model != nil {
		id := types.UserID(source.ID)

		for _, zone := range model.Zones {
			if slices.Contains(zone.Users, id) {
				return errMerge1
			}
		}

		for _, limit := range model.ExitLimits {
			if limit.User == id {
				return errMerge2
			}
		}

		for _, rule := range model.OwnExitOverrides {
			if rule.User == id {
				return errMerge3
			}
		}

		if slices.Contains(model.ExitProviderUsers, id) {
			return errMerge4
		}
	}

	if strings.TrimSpace(text) == "" {
		return nil
	}

	raw, err := hujson.Standardize([]byte(text))
	if err != nil {
		return err
	}

	var document any

	err = json.Unmarshal(raw, &document)
	if err != nil {
		return err
	}

	selectors := map[string]bool{source.Name: true, source.Name + "@": true}
	if source.ProviderIdentifier.Valid {
		selectors[source.ProviderIdentifier.String] = true
	}

	if source.Email != "" && !strings.EqualFold(source.Email, target.Email) {
		selectors[source.Email] = true
	}

	if !target.ProviderIdentifier.Valid && source.ProviderIdentifier.Valid {
		selectors[target.Name] = true
		selectors[target.Name+"@"] = true
	}

	var visit func(any) error

	visit = func(value any) error {
		switch v := value.(type) {
		case string:
			if v != "" && selectors[v] {
				return fmt.Errorf("%w: %q", errMergePolicyReference, v)
			}
		case []any:
			for _, child := range v {
				err := visit(child)
				if err != nil {
					return err
				}
			}
		case map[string]any:
			for _, child := range v {
				err := visit(child)
				if err != nil {
					return err
				}
			}
		}

		return nil
	}

	return visit(document)
}
