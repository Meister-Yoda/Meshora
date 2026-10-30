package db

import (
	"errors"
	"strings"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"gorm.io/gorm"
)

const (
	invitationSQLiteTimestamp   = "datetime"
	invitationPostgresTimestamp = "timestamptz"
)

var ErrInvitation = errors.New("einladung ungültig, abgelaufen oder Konto bereits vorhanden")

func ensureUserInvitations(tx *gorm.DB) error {
	stamp := invitationSQLiteTimestamp
	if tx.Name() == types.DatabasePostgres {
		stamp = invitationPostgresTimestamp
	}

	return tx.Exec("CREATE TABLE IF NOT EXISTS user_invitations (id text PRIMARY KEY, email text NOT NULL UNIQUE, mode text NOT NULL CHECK (mode IN ('local','oidc')), token_hash text NOT NULL UNIQUE, created_by bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE, created_at " + stamp + " NOT NULL, expires_at " + stamp + " NOT NULL)").Error
}

func invitationEmailAvailable(tx *gorm.DB, email string) error {
	var count int64

	err := tx.Model(&types.User{}).Where("lower(trim(email)) = ?", email).Count(&count).Error
	if err != nil {
		return err
	}

	if count != 0 {
		return ErrInvitation
	}

	return nil
}

func (hsdb *HSDatabase) CreateUserInvitation(actor types.UserID, revision uint64, email, mode, id, digest string) error {
	normalized, err := NormalizeAccountEmail(email)
	if err != nil || len(normalized) > 254 || (mode != "local" && mode != "oidc") || len(digest) != 64 || len(id) < 32 {
		return ErrInvitation
	}

	return hsdb.Write(func(tx *gorm.DB) error {
		var err error

		err = lockSecurityAccount(tx, actor, revision)
		if err != nil {
			return err
		}

		var account types.UserAccount

		err = tx.First(&account, "user_id = ?", actor).Error
		if err != nil || !account.IsAdmin {
			return ErrInvitation
		}

		err = invitationEmailAvailable(tx, normalized)
		if err != nil {
			return err
		}

		err = tx.Where("email = ?", normalized).Delete(&types.UserInvitation{}).Error
		if err != nil {
			return err
		}

		return tx.Create(&types.UserInvitation{ID: id, Email: normalized, Mode: mode, TokenHash: digest, CreatedBy: uint64(actor), CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(24 * time.Hour)}).Error
	})
}

func (hsdb *HSDatabase) UserInvitations() ([]types.UserInvitation, error) {
	var result []types.UserInvitation

	err := hsdb.DB.Order("created_at desc").Find(&result).Error

	return result, err
}

func (hsdb *HSDatabase) UserInvitation(digest string) (*types.UserInvitation, error) {
	var invite types.UserInvitation

	err := hsdb.DB.First(&invite, "token_hash = ?", digest).Error
	if err != nil || !time.Now().Before(invite.ExpiresAt) {
		return nil, ErrInvitation
	}

	return &invite, nil
}

func (hsdb *HSDatabase) RevokeUserInvitation(actor types.UserID, revision uint64, id string) error {
	return hsdb.Write(func(tx *gorm.DB) error {
		var err error

		err = lockSecurityAccount(tx, actor, revision)
		if err != nil {
			return err
		}

		var account types.UserAccount

		err = tx.First(&account, "user_id = ?", actor).Error
		if err != nil || !account.IsAdmin {
			return ErrInvitation
		}

		return tx.Delete(&types.UserInvitation{}, "id = ?", id).Error
	})
}

func (hsdb *HSDatabase) CancelUserInvitation(id, digest string) error {
	return hsdb.DB.Delete(&types.UserInvitation{}, "id = ? AND token_hash = ?", id, digest).Error
}

func (hsdb *HSDatabase) AcceptUserInvitation(digest, mode, password string, claims *types.OIDCClaims) (*types.User, error) {
	var hash []byte

	if mode == "local" {
		err := validateWebAdminPassword(password)
		if err != nil {
			return nil, err
		}

		webAdminHashLimiter <- struct{}{}

		hash = hashWebAdminPassword(password)

		<-webAdminHashLimiter
	} else if mode != "oidc" || claims == nil || !bool(claims.EmailVerified) || claims.Sub == "" || claims.Iss == "" {
		return nil, ErrInvitation
	}

	var user *types.User

	err := hsdb.Write(func(tx *gorm.DB) error {
		var (
			err    error
			invite types.UserInvitation
		)

		loadErr := tx.First(&invite, "token_hash = ? AND mode = ?", digest, mode).Error
		if loadErr != nil || !time.Now().Before(invite.ExpiresAt) {
			return ErrInvitation
		}

		if mode == "oidc" {
			normalized, err := NormalizeAccountEmail(claims.Email)
			if err != nil || normalized != invite.Email {
				return ErrInvitation
			}

			var count int64

			lookupErr := tx.Model(&types.User{}).Where("provider_identifier = ?", claims.Identifier()).Count(&count).Error
			if lookupErr != nil || count != 0 {
				return ErrInvitation
			}
		}

		err = invitationEmailAvailable(tx, invite.Email)
		if err != nil {
			return err
		}
		// Consuming the link and creating the account are one transaction.
		deleted := tx.Delete(&types.UserInvitation{}, "id = ? AND token_hash = ?", invite.ID, digest)
		if deleted.Error != nil {
			return deleted.Error
		}

		if deleted.RowsAffected != 1 {
			return ErrInvitation
		}

		candidate := types.User{Name: "invite-" + digest[:20], Email: invite.Email}
		if mode == "oidc" {
			candidate.FromClaim(claims, true)
			candidate.Email = invite.Email
		}

		created, err := CreateUser(tx, candidate)
		if err != nil {
			return err
		}

		if mode == "local" {
			err = tx.Model(&types.UserAccount{}).Where("user_id = ?", created.ID).Update("password_hash", hash).Error
			if err != nil {
				return err
			}
		}

		user = created

		return nil
	})
	if err != nil {
		return nil, err
	}

	return user, nil
}

// Normalize caller-owned input before comparing it to stored invitations.
var _ = strings.EqualFold
