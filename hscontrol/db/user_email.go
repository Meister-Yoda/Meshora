package db

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrEmailChange     = errors.New("E-Mail-Änderung nicht erlaubt oder Bestätigung abgelaufen")
	ErrEmailChangeRate = errors.New("bitte vor einer weiteren Bestätigungs-E-Mail eine Minute warten")
)

func ensureUserEmailChanges(tx *gorm.DB) error {
	stamp := "datetime"
	if tx.Name() == types.DatabasePostgres {
		stamp = "timestamptz"
	}

	return tx.Transaction(func(tx *gorm.DB) error {
		if !tx.Migrator().HasColumn(&types.UserAuthPolicy{}, "email_change_blocked") {
			err := tx.Exec("ALTER TABLE user_auth_policies ADD COLUMN email_change_blocked boolean NOT NULL DEFAULT false").Error
			if err != nil {
				return err
			}
		}

		return tx.Exec("CREATE TABLE IF NOT EXISTS user_email_changes (user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, email text NOT NULL, token_hash text NOT NULL, account_revision bigint NOT NULL, requested_at " + stamp + " NOT NULL, expires_at " + stamp + " NOT NULL)").Error
	})
}

func emailChangeUser(tx *gorm.DB, id types.UserID, revision uint64) (*types.User, error) {
	lockErr := lockSecurityAccount(tx, id, revision)
	if lockErr != nil {
		return nil, lockErr
	}

	policy, err := userAuthPolicy(tx, id)
	if err != nil {
		return nil, err
	}

	user, err := GetUserByID(tx, id)
	if err != nil {
		return nil, err
	}
	// Provider-owned addresses must be changed at the identity provider.
	if policy.EmailChangeBlocked || user.ProviderIdentifier.Valid || (user.Provider != "" && user.Provider != "cli") {
		return nil, ErrEmailChange
	}

	return user, nil
}

func (hsdb *HSDatabase) BeginUserEmailChange(id types.UserID, revision uint64, email, digest string) error {
	normalized, err := NormalizeAccountEmail(email)
	if err != nil || len(normalized) > 254 || len(digest) != 64 {
		return ErrEmailChange
	}

	return hsdb.Write(func(tx *gorm.DB) error {
		user, err := emailChangeUser(tx, id, revision)
		if err != nil {
			return err
		}

		if strings.EqualFold(user.Email, normalized) {
			return ErrEmailChange
		}

		var count int64

		err = tx.Model(&types.User{}).Where("id <> ? AND lower(trim(email)) = ?", id, normalized).Count(&count).Error
		if err != nil {
			return err
		}

		if count != 0 {
			return ErrEmailChange
		}

		now := time.Now().UTC()

		var previous types.UserEmailChange

		err = tx.First(&previous, "user_id = ?", id).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		if err == nil && now.Before(previous.RequestedAt.Add(time.Minute)) {
			return ErrEmailChangeRate
		}

		request := types.UserEmailChange{UserID: uint64(id), Email: normalized, TokenHash: digest, AccountRevision: revision, RequestedAt: now, ExpiresAt: now.Add(20 * time.Minute)}

		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}}, UpdateAll: true}).Create(&request).Error
	})
}

func (hsdb *HSDatabase) PendingUserEmailChange(id types.UserID) (*types.UserEmailChange, error) {
	var pending types.UserEmailChange

	err := hsdb.DB.First(&pending, "user_id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // No live request is a normal account state.
	}

	if err == nil && !time.Now().Before(pending.ExpiresAt) {
		return nil, nil //nolint:nilnil // No live request is a normal account state.
	}

	return &pending, err
}

func (hsdb *HSDatabase) CancelUserEmailChange(id types.UserID, digest string) error {
	return hsdb.Write(func(tx *gorm.DB) error {
		return tx.Where("user_id = ? AND token_hash = ?", id, digest).Delete(&types.UserEmailChange{}).Error
	})
}

func (hsdb *HSDatabase) ConfirmUserEmailChange(id types.UserID, revision uint64, digest string) (*types.User, error) {
	return Write(hsdb.DB, func(tx *gorm.DB) (*types.User, error) {
		user, err := emailChangeUser(tx, id, revision)
		if err != nil {
			return nil, err
		}

		var pending types.UserEmailChange

		err = tx.First(&pending, "user_id = ? AND token_hash = ? AND account_revision = ?", id, digest, revision).Error
		if err != nil || !time.Now().Before(pending.ExpiresAt) {
			return nil, ErrEmailChange
		}

		oldEmail := user.Email
		user.Email = pending.Email
		// This enforces the existing unique login address in the same transaction.
		err = EnsureUserAccount(tx, user)
		if err != nil {
			return nil, err
		}

		err = tx.Model(&types.User{}).Where("id = ?", id).Update("email", pending.Email).Error
		if err != nil {
			return nil, err
		}

		if oldEmail != "" {
			err := tx.Model(&types.UserAccount{}).Where("user_id = ? AND lower(legacy_login) = ?", id, strings.ToLower(oldEmail)).Update("legacy_login", nil).Error
			if err != nil {
				return nil, err
			}
		}

		err = advanceSecurityRevision(tx, id)
		if err != nil {
			return nil, err
		}

		err = tx.Where("user_id = ?", id).Delete(&types.UserEmailChange{}).Error
		if err != nil {
			return nil, err
		}

		return user, nil
	})
}

func (hsdb *HSDatabase) SetUserEmailChangeBlocked(actor, target types.UserID, revision uint64, blocked bool) error {
	return hsdb.Write(func(tx *gorm.DB) error {
		ids := []types.UserID{actor, target}
		slices.Sort(ids)

		for _, id := range slices.Compact(ids) {
			if id == actor {
				err := lockSecurityAccount(tx, actor, revision)
				if err != nil {
					return err
				}
			} else {
				result := tx.Model(&types.UserAccount{}).Where("user_id = ?", id).Update("revision", gorm.Expr("revision"))
				if result.Error != nil {
					return result.Error
				}

				if result.RowsAffected != 1 {
					return ErrAccountChanged
				}
			}
		}

		var account types.UserAccount

		err := tx.First(&account, "user_id = ?", actor).Error
		if err != nil {
			return err
		}

		if !account.IsAdmin {
			return ErrEmailChange
		}

		_, err = GetUserByID(tx, target)
		if err != nil {
			return err
		}

		policy, err := userAuthPolicy(tx, target)
		if err != nil {
			return err
		}

		policy.EmailChangeBlocked = blocked
		policy.UpdatedBy = uint64(actor)

		policy.UpdatedAt = time.Now().UTC()

		err = tx.Save(policy).Error
		if err != nil {
			return err
		}

		err = tx.Where("user_id = ?", target).Delete(&types.UserEmailChange{}).Error
		if err != nil {
			return err
		}

		return advanceSecurityRevision(tx, target)
	})
}
