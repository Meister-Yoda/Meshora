package state

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/juanfont/headscale/hscontrol/db"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/util"
)

func (s *State) UserAccount(id types.UserID) (*types.UserAccount, error) {
	return s.db.GetUserAccount(id)
}

func (s *State) AuthenticateUserAccount(login, password string) (*types.UserAccount, error) {
	return s.db.AuthenticateUserAccount(login, password)
}

func (s *State) SetUserAdmin(id types.UserID, enabled bool) error {
	return s.db.SetUserAdmin(id, enabled)
}

func (s *State) ResetUserPassword(id types.UserID) (string, error) { return s.db.ResetUserPassword(id) }

func (s *State) ChangeUserPassword(id types.UserID, revision uint64, current, password string, oidc bool) error {
	return s.db.ChangeUserPassword(id, revision, current, password, oidc)
}

var (
	ErrOIDCEmailConflict   = errors.New("email matches multiple users or an already linked identity")
	ErrOIDCEmailUnverified = errors.New("OIDC provider has not verified the email address")
)

func (s *State) ResolveOIDCUser(claims *types.OIDCClaims, emailVerifiedRequired bool) (*types.User, error) {
	var (
		user    *types.User
		err     error
		newUser bool
	)

	user, err = s.GetUserByOIDCIdentifier(claims.Identifier())
	if err != nil && !errors.Is(err, db.ErrUserNotFound) {
		return nil, fmt.Errorf("creating or updating user: %w", err)
	}

	// Only a verified mailbox can link a previously local identity. Never
	// rebind an existing OIDC identity based on a mutable email claim.
	if user == nil && claims.EmailVerified && claims.Email != "" {
		address, parseErr := mail.ParseAddress(claims.Email)
		if parseErr == nil && address.Address == claims.Email {
			users, listErr := s.ListAllUsers()
			if listErr != nil {
				return nil, fmt.Errorf("finding user by email: %w", listErr)
			}

			for i := range users {
				candidate := &users[i]
				if !strings.EqualFold(strings.TrimSpace(candidate.Email), claims.Email) {
					continue
				}

				if user != nil || candidate.ProviderIdentifier.Valid ||
					(candidate.Provider != "" && candidate.Provider != util.RegisterMethodCLI) {
					return nil, ErrOIDCEmailConflict
				}

				user = candidate
			}
		}
	}

	// if the user is still not found, create a new empty user.
	// TODO(kradalby): This context is not inherited from the request, which is probably not ideal.
	// However, we need a context to use the OIDC provider.
	if user == nil {
		_, emailErr := db.NormalizeAccountEmail(claims.Email)
		if emailErr != nil {
			return nil, emailErr
		}

		if emailVerifiedRequired && (!bool(claims.EmailVerified) || claims.Email == "") {
			return nil, ErrOIDCEmailUnverified
		}

		newUser = true
		user = &types.User{}
	}

	user.FromClaim(claims, emailVerifiedRequired)

	if newUser {
		user, _, err = s.CreateUser(*user)
		if err != nil {
			return nil, fmt.Errorf("creating user: %w", err)
		}
	} else {
		_, _, err = s.UpdateUser(types.UserID(user.ID), func(u *types.User) error {
			// The account may have been linked by another login since lookup.
			if u.ProviderIdentifier.Valid && u.ProviderIdentifier != user.ProviderIdentifier {
				return ErrOIDCEmailConflict
			}

			*u = *user

			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("updating user: %w", err)
		}
	}

	return user, nil
}

func (s *State) UserSecurity(id types.UserID) (*types.UserSecurity, error) {
	return s.db.UserSecurity(id)
}

func (s *State) UserPasskeys(id types.UserID) ([]types.UserPasskey, error) {
	return s.db.UserPasskeys(id)
}

func (s *State) EnableUserTOTP(id types.UserID, rev uint64, secret string, key []byte, step int64, hashes []string) error {
	return s.db.EnableUserTOTP(id, rev, secret, key, step, hashes)
}

func (s *State) DisableUserTOTP(id types.UserID, rev uint64) error {
	return s.db.DisableUserTOTP(id, rev)
}

func (s *State) ConsumeSecondFactor(id types.UserID, rev uint64, step int64, hash string) error {
	return s.db.ConsumeSecondFactor(id, rev, step, hash)
}

func (s *State) AddUserPasskey(id types.UserID, rev uint64, record types.UserPasskey) error {
	return s.db.AddUserPasskey(id, rev, record)
}

func (s *State) DeleteUserPasskey(id types.UserID, rev uint64, key string) error {
	return s.db.DeleteUserPasskey(id, rev, key)
}

func (s *State) UseUserPasskey(id types.UserID, rev uint64, key, oldData, newData string) error {
	return s.db.UseUserPasskey(id, rev, key, oldData, newData)
}

func (s *State) UserAuthPolicy(id types.UserID) (*types.UserAuthPolicy, error) {
	return s.db.UserAuthPolicy(id)
}

func (s *State) SetUserAuthPolicy(actor, target types.UserID, revision uint64, required bool, totpAction, passkeyAction, reason string) error {
	return s.db.SetUserAuthPolicy(actor, target, revision, required, totpAction, passkeyAction, reason)
}
