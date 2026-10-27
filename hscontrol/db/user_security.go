package db

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"gorm.io/gorm"
)

var (
	ErrSecondFactor  = errors.New("der Anmeldecode ungültig oder bereits verwendet")
	ErrSecurityMerge = errors.New("die Anmeldesicherheit des Quellkontos (Faktoren, Pflicht und Ausnahmen) vor dem Zusammenführen prüfen")
	ErrPasskeyLimit  = errors.New("höchstens 20 Passkeys pro Konto erlaubt")
	ErrSecurityKey   = errors.New("TOTP-Verschlüsselungsschlüssel fehlt oder ist ungültig")
)

// totpCipher accepts only an AES-256 key supplied from the server's existing private key.
func totpCipher(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, ErrSecurityKey
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	return cipher.NewGCM(block)
}

func sealTOTP(secret string, key []byte, id types.UserID) ([]byte, error) {
	aead, err := totpCipher(key)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, aead.NonceSize())

	_, err = rand.Read(nonce)
	if err != nil {
		return nil, err
	}

	return aead.Seal(nonce, nonce, []byte(secret), []byte(fmt.Sprintf("Meshora-TOTP:%d", id))), nil
}

// OpenTOTP authenticates the encrypted secret and binds it to its immutable account UID.
func OpenTOTP(record *types.UserSecurity, key []byte) (string, error) {
	aead, err := totpCipher(key)
	if err != nil {
		return "", err
	}

	encrypted := record.EncryptedTOTPSecret
	if len(encrypted) < aead.NonceSize() {
		return "", ErrSecurityKey
	}

	plaintext, err := aead.Open(nil, encrypted[:aead.NonceSize()], encrypted[aead.NonceSize():], []byte(fmt.Sprintf("Meshora-TOTP:%d", record.UserID)))
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

func ensureUserSecurity(tx *gorm.DB) error {
	return tx.Transaction(func(tx *gorm.DB) error {
		blob, stamp := "blob", "datetime"
		if tx.Name() == types.DatabasePostgres {
			blob, stamp = "bytea", "timestamptz"
		}

		statements := []string{
			"CREATE TABLE IF NOT EXISTS user_securities (user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, encrypted_totp_secret " + blob + ", last_totp_step bigint NOT NULL DEFAULT -1, recovery_hashes text NOT NULL DEFAULT '[]')",
			"CREATE TABLE IF NOT EXISTS user_passkeys (id text PRIMARY KEY, user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE, name text NOT NULL, credential text NOT NULL, created_at " + stamp + " NOT NULL)",
			"CREATE INDEX IF NOT EXISTS idx_user_passkeys_user_id ON user_passkeys(user_id)",
		}
		for _, statement := range statements {
			err := tx.Exec(statement).Error
			if err != nil {
				return err
			}
		}

		return nil
	})
}

func (hsdb *HSDatabase) UserSecurity(id types.UserID) (*types.UserSecurity, error) {
	result := types.UserSecurity{UserID: uint64(id), LastTOTPStep: -1, RecoveryHashes: "[]"}

	err := hsdb.DB.First(&result, "user_id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &result, nil
	}

	return &result, err
}

func (hsdb *HSDatabase) UserPasskeys(id types.UserID) ([]types.UserPasskey, error) {
	var records []types.UserPasskey

	err := hsdb.DB.Where("user_id = ?", id).Order("created_at, id").Find(&records).Error

	return records, err
}

func lockSecurityAccount(tx *gorm.DB, id types.UserID, revision uint64) error {
	result := tx.Model(&types.UserAccount{}).Where("user_id = ? AND revision = ?", id, revision).Update("revision", gorm.Expr("revision"))
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected != 1 {
		return ErrAccountChanged
	}

	return nil
}

func advanceSecurityRevision(tx *gorm.DB, id types.UserID) error {
	return tx.Model(&types.UserAccount{}).Where("user_id = ?", id).Update("revision", gorm.Expr("revision + 1")).Error
}

// EnableUserTOTP encrypts before persistence; callers cannot accidentally save the plaintext secret.
func (hsdb *HSDatabase) EnableUserTOTP(id types.UserID, revision uint64, secret string, key []byte, step int64, hashes []string) error {
	encrypted, err := sealTOTP(secret, key, id)
	if err != nil {
		return err
	}

	return hsdb.Write(func(tx *gorm.DB) error {
		err := lockSecurityAccount(tx, id, revision)
		if err != nil {
			return err
		}

		var existing types.UserSecurity

		err = tx.First(&existing, "user_id = ?", id).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		policy, err := userAuthPolicy(tx, id)
		if err != nil {
			return err
		}

		if len(existing.EncryptedTOTPSecret) > 0 && !policy.TOTPBypassedAt(time.Now()) {
			return ErrAccountChanged
		}

		data, err := json.Marshal(hashes)
		if err != nil {
			return err
		}

		err = tx.Save(&types.UserSecurity{UserID: uint64(id), EncryptedTOTPSecret: encrypted, LastTOTPStep: step, RecoveryHashes: string(data)}).Error
		if err != nil {
			return err
		}

		if policy.TOTPBypassedAt(time.Now()) {
			err = tx.Model(&types.UserAuthPolicy{}).Where("user_id = ?", id).Update("totp_suspended_until", nil).Error
			if err != nil {
				return err
			}
		}

		return advanceSecurityRevision(tx, id)
	})
}

func (hsdb *HSDatabase) DisableUserTOTP(id types.UserID, revision uint64) error {
	return hsdb.Write(func(tx *gorm.DB) error {
		err := lockSecurityAccount(tx, id, revision)
		if err != nil {
			return err
		}

		err = tx.Delete(&types.UserSecurity{}, "user_id = ?", id).Error
		if err != nil {
			return err
		}

		return advanceSecurityRevision(tx, id)
	})
}

// ConsumeSecondFactor serializes TOTP steps and single-use recovery codes.
func (hsdb *HSDatabase) ConsumeSecondFactor(id types.UserID, revision uint64, step int64, recoveryHash string) error {
	return hsdb.Write(func(tx *gorm.DB) error {
		err := lockSecurityAccount(tx, id, revision)
		if err != nil {
			return err
		}

		var security types.UserSecurity

		err = tx.First(&security, "user_id = ?", id).Error
		if err != nil {
			return ErrSecondFactor
		}

		if len(security.EncryptedTOTPSecret) == 0 {
			return ErrSecondFactor
		}

		if recoveryHash == "" {
			if step <= security.LastTOTPStep {
				return ErrSecondFactor
			}

			return tx.Model(&security).Update("last_totp_step", step).Error
		}

		var hashes []string

		err = json.Unmarshal([]byte(security.RecoveryHashes), &hashes)
		if err != nil {
			return err
		}

		for i, hash := range hashes {
			if hash == recoveryHash {
				hashes = append(hashes[:i], hashes[i+1:]...)

				data, err := json.Marshal(hashes)
				if err != nil {
					return err
				}

				return tx.Model(&security).Update("recovery_hashes", string(data)).Error
			}
		}

		return ErrSecondFactor
	})
}

func (hsdb *HSDatabase) AddUserPasskey(id types.UserID, revision uint64, record types.UserPasskey) error {
	return hsdb.Write(func(tx *gorm.DB) error {
		err := lockSecurityAccount(tx, id, revision)
		if err != nil {
			return err
		}

		var count int64

		err = tx.Model(&types.UserPasskey{}).Where("user_id = ?", id).Count(&count).Error
		if err != nil {
			return err
		}

		if count >= 20 {
			return ErrPasskeyLimit
		}

		record.UserID = uint64(id)

		record.CreatedAt = time.Now()

		err = tx.Create(&record).Error
		if err != nil {
			return err
		}

		return advanceSecurityRevision(tx, id)
	})
}

func (hsdb *HSDatabase) DeleteUserPasskey(id types.UserID, revision uint64, keyID string) error {
	return hsdb.Write(func(tx *gorm.DB) error {
		err := lockSecurityAccount(tx, id, revision)
		if err != nil {
			return err
		}

		result := tx.Where("user_id = ? AND id = ?", id, keyID).Delete(&types.UserPasskey{})
		if result.Error != nil {
			return result.Error
		}

		if result.RowsAffected != 1 {
			return ErrAccountChanged
		}

		return advanceSecurityRevision(tx, id)
	})
}

func (hsdb *HSDatabase) UseUserPasskey(id types.UserID, revision uint64, keyID, previous, updated string) error {
	return hsdb.Write(func(tx *gorm.DB) error {
		err := lockSecurityAccount(tx, id, revision)
		if err != nil {
			return err
		}

		policy, err := userAuthPolicy(tx, id)
		if err != nil {
			return err
		}

		if policy.PasskeysSuspendedAt(time.Now()) {
			return ErrAuthPolicy
		}

		result := tx.Model(&types.UserPasskey{}).Where("user_id = ? AND id = ? AND credential = ?", id, keyID, previous).Update("credential", updated)
		if result.Error != nil {
			return result.Error
		}

		if result.RowsAffected != 1 {
			return ErrAccountChanged
		}

		return nil
	})
}

var ErrAuthPolicy = errors.New("ungültige oder abgelaufene Sicherheitsrichtlinie")

func ensureUserAuthPolicy(tx *gorm.DB) error {
	stamp := "datetime"
	if tx.Name() == types.DatabasePostgres {
		stamp = "timestamptz"
	}

	return tx.Transaction(func(tx *gorm.DB) error {
		return tx.Exec("CREATE TABLE IF NOT EXISTS user_auth_policies (user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, require_strong_auth boolean NOT NULL DEFAULT false, totp_suspended_until " + stamp + ", passkeys_suspended_until " + stamp + ", updated_by bigint NOT NULL DEFAULT 0, updated_at " + stamp + ", reason text NOT NULL DEFAULT '')").Error
	})
}

func userAuthPolicy(tx *gorm.DB, id types.UserID) (*types.UserAuthPolicy, error) {
	policy := types.UserAuthPolicy{UserID: uint64(id)}

	err := tx.First(&policy, "user_id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &policy, nil
	}

	return &policy, err
}

func (hsdb *HSDatabase) UserAuthPolicy(id types.UserID) (*types.UserAuthPolicy, error) {
	return userAuthPolicy(hsdb.DB, id)
}

// SetUserAuthPolicy revokes all sessions through the account revision in the same transaction.
func (hsdb *HSDatabase) SetUserAuthPolicy(actorID, targetID types.UserID, actorRevision uint64, required bool, totpAction, passkeyAction string, reason string) error {
	return hsdb.Write(func(tx *gorm.DB) error {
		var actor types.UserAccount
		// Serialize security changes, including concurrent demotions of the acting administrator.
		ids := []types.UserID{actorID, targetID}
		slices.Sort(ids)

		ids = slices.Compact(ids)
		for _, id := range ids {
			if id == actorID {
				err := lockSecurityAccount(tx, actorID, actorRevision)
				if err != nil {
					return err
				}
			} else {
				locked := tx.Model(&types.UserAccount{}).Where("user_id = ?", id).Update("revision", gorm.Expr("revision"))
				if locked.Error != nil {
					return locked.Error
				}

				if locked.RowsAffected != 1 {
					return ErrAccountChanged
				}
			}
		}

		var err error

		err = tx.First(&actor, "user_id = ?", actorID).Error
		if err != nil {
			return err
		}

		if !actor.IsAdmin {
			return ErrAuthPolicy
		}
		// Target and actor locks are acquired in one transaction; a failure leaves both unchanged.
		var target types.UserAccount

		err = tx.First(&target, "user_id = ?", targetID).Error
		if err != nil {
			return err
		}

		if len(reason) > 256 {
			return ErrAuthPolicy
		}

		policy, err := userAuthPolicy(tx, targetID)
		if err != nil {
			return err
		}

		now := time.Now().UTC()

		policy.TOTPSuspendedUntil, err = authPauseUntil(totpAction, policy.TOTPSuspendedUntil, now)
		if err != nil {
			return err
		}

		policy.PasskeysSuspendedUntil, err = authPauseUntil(passkeyAction, policy.PasskeysSuspendedUntil, now)
		if err != nil {
			return err
		}

		policy.RequireStrongAuth = required
		policy.UpdatedBy = uint64(actorID)
		policy.UpdatedAt = now
		policy.Reason = reason

		err = tx.Save(policy).Error
		if err != nil {
			return err
		}

		return advanceSecurityRevision(tx, targetID)
	})
}

func authPauseUntil(action string, current *time.Time, now time.Time) (*time.Time, error) {
	if action == "keep" {
		return current, nil
	}

	if action == "resume" {
		//nolint:nilnil // A nil deadline explicitly means resume immediately.
		return nil, nil
	}

	durations := map[string]time.Duration{"15m": 15 * time.Minute, "1h": time.Hour, "4h": 4 * time.Hour, "24h": 24 * time.Hour}

	duration, ok := durations[action]
	if !ok {
		return nil, ErrAuthPolicy
	}

	until := now.Add(duration)

	return &until, nil
}
