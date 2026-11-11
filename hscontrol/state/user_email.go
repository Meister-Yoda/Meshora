package state

import (
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
)

func (s *State) BeginUserEmailChange(id types.UserID, revision uint64, email, digest string) error {
	return s.db.BeginUserEmailChange(id, revision, email, digest)
}

func (s *State) PendingUserEmailChange(id types.UserID) (*types.UserEmailChange, error) {
	return s.db.PendingUserEmailChange(id)
}

func (s *State) CancelUserEmailChange(id types.UserID, digest string) error {
	return s.db.CancelUserEmailChange(id, digest)
}

func (s *State) ConfirmUserEmailChange(id types.UserID, revision uint64, digest string) (change.Change, error) {
	s.userUpdateMu.Lock()
	defer s.userUpdateMu.Unlock()

	previous, err := s.db.GetUserByID(id)
	if err != nil {
		return change.Change{}, err
	}

	user, err := s.db.ConfirmUserEmailChange(id, revision, digest)
	if err != nil {
		return change.Change{}, err
	}

	return s.refreshUpdatedUser(*previous, user)
}

func (s *State) SetUserEmailChangeBlocked(actor, target types.UserID, revision uint64, blocked bool) error {
	return s.db.SetUserEmailChangeBlocked(actor, target, revision, blocked)
}
