package state

import "github.com/juanfont/headscale/hscontrol/types"

func (s *State) CreateUserInvitation(actor types.UserID, revision uint64, email, mode, id, digest string) error {
	return s.db.CreateUserInvitation(actor, revision, email, mode, id, digest)
}
func (s *State) UserInvitations() ([]types.UserInvitation, error) { return s.db.UserInvitations() }
func (s *State) UserInvitation(digest string) (*types.UserInvitation, error) {
	return s.db.UserInvitation(digest)
}

func (s *State) RevokeUserInvitation(actor types.UserID, revision uint64, id string) error {
	return s.db.RevokeUserInvitation(actor, revision, id)
}

func (s *State) CancelUserInvitation(id, digest string) error {
	return s.db.CancelUserInvitation(id, digest)
}

func (s *State) AcceptUserInvitation(digest, mode, password string, claims *types.OIDCClaims) (*types.User, error) {
	user, err := s.db.AcceptUserInvitation(digest, mode, password, claims)
	if err != nil {
		return nil, err
	}

	_, err = s.updatePolicyManagerUsers()

	return user, err
}
