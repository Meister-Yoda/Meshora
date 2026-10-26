package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrAccountEmail    = errors.New("eine eindeutige gültige E-Mail-Adresse ist erforderlich")
	ErrAccountConflict = errors.New("E-Mail-Adresse ist bereits einem anderen Benutzer zugeordnet")
	ErrLastAdmin       = errors.New("der letzte Administrator darf nicht entfernt werden")
	ErrAccountChanged  = errors.New("das Konto wurde geändert; bitte erneut anmelden")
	ErrPasswordReused  = errors.New("das neue Passwort muss sich vom bisherigen Passwort unterscheiden")
)

const (
	accountRevisionColumn = "revision"
	accountPasswordColumn = "password_hash"
)

func NormalizeAccountEmail(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))

	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || !strings.Contains(value, "@") {
		return "", ErrAccountEmail
	}

	return value, nil
}

// Explicit DDL is shared by fresh installs and the append-only migration.
func ensureUserAccounts(tx *gorm.DB) error {
	if tx.Migrator().HasTable(&types.UserAccount{}) {
		return nil
	}

	return tx.Transaction(func(tx *gorm.DB) error {
		blob := "blob"
		stamp := "datetime"

		if tx.Name() == types.DatabasePostgres {
			blob = "bytea"
			stamp = "timestamptz"
		}

		ddl := fmt.Sprintf(`CREATE TABLE user_accounts (
   user_id bigint PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
   login_email text UNIQUE, legacy_login text UNIQUE,
   is_admin boolean NOT NULL DEFAULT false,
   password_hash %s, must_change_password boolean NOT NULL DEFAULT false,
   revision bigint NOT NULL DEFAULT 1, last_login_at %s
  )`, blob, stamp)

		err := tx.Exec(ddl).Error
		if err != nil {
			return err
		}

		var users []types.User

		err = tx.Find(&users).Error
		if err != nil {
			return err
		}

		counts := map[string]int{}

		for _, user := range users {
			email, emailErr := NormalizeAccountEmail(user.Email)
			if emailErr == nil {
				counts[email]++
			}
		}

		for _, user := range users {
			account := types.UserAccount{UserID: uint64(user.ID), Revision: 1}

			email, emailErr := NormalizeAccountEmail(user.Email)
			if emailErr == nil && counts[email] == 1 {
				account.LoginEmail = sql.NullString{String: email, Valid: true}
			}

			err := tx.Create(&account).Error
			if err != nil {
				return err
			}
		}

		var admins []types.WebAdmin

		err = tx.Order("id").Find(&admins).Error
		if err != nil {
			return err
		}

		for _, admin := range admins {
			err := ensureLegacyAccount(tx, &admin)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// EnsureUserAccount synchronizes the unique login address without changing UID.
func EnsureUserAccount(tx *gorm.DB, user *types.User) error {
	account := types.UserAccount{UserID: uint64(user.ID), Revision: 1}
	if user.Email != "" {
		email, err := NormalizeAccountEmail(user.Email)
		if err != nil {
			return err
		}

		var count int64

		err = tx.Model(&types.User{}).Where("id <> ? AND lower(trim(email)) = ?", user.ID, email).Count(&count).Error
		if err != nil {
			return err
		}

		if count > 0 {
			return ErrAccountConflict
		}

		user.Email = email
		account.LoginEmail = sql.NullString{String: email, Valid: true}
	}

	err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}}, DoNothing: true}).Create(&account).Error
	if err != nil {
		return err
	}

	return tx.Model(&types.UserAccount{}).Where("user_id = ?", user.ID).Update("login_email", account.LoginEmail).Error
}

func (hsdb *HSDatabase) GetUserAccount(id types.UserID) (*types.UserAccount, error) {
	var account types.UserAccount

	err := hsdb.DB.Where("user_id = ?", id).First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		_, err = hsdb.GetUserByID(id)
		if err != nil {
			return nil, err
		}

		return &types.UserAccount{UserID: uint64(id)}, nil
	}

	return &account, err
}

func (hsdb *HSDatabase) AuthenticateUserAccount(login, password string) (*types.UserAccount, error) {
	var accounts []types.UserAccount

	key := strings.ToLower(strings.TrimSpace(login))

	err := hsdb.DB.Model(&types.UserAccount{}).Joins("JOIN users ON users.id = user_accounts.user_id AND users.deleted_at IS NULL").Where("user_accounts.login_email = ? OR lower(user_accounts.legacy_login) = ?", key, key).Find(&accounts).Error
	if err != nil {
		return nil, err
	}

	hash := dummyWebAdminHash
	if len(accounts) == 1 && len(accounts[0].PasswordHash) > 0 {
		hash = accounts[0].PasswordHash
	}

	webAdminHashLimiter <- struct{}{}

	verifyErr := verifyArgon2id(hash, password)

	<-webAdminHashLimiter

	if len(accounts) != 1 || len(accounts[0].PasswordHash) == 0 || verifyErr != nil {
		return nil, ErrWebAdminInvalidCredentials
	}

	now := time.Now()

	err = hsdb.DB.Model(&types.UserAccount{}).Where("user_id = ?", accounts[0].UserID).Update("last_login_at", now).Error
	if err != nil {
		return nil, err
	}

	accounts[0].LastLoginAt = &now

	return &accounts[0], nil
}

func (hsdb *HSDatabase) SetUserAdmin(id types.UserID, enabled bool) error {
	return hsdb.Write(func(tx *gorm.DB) error {
		// Acquire the write lock before counting, including on SQLite.
		err := tx.Exec("UPDATE user_accounts SET revision = revision WHERE is_admin = ?", true).Error
		if err != nil {
			return err
		}

		user, err := GetUserByID(tx, id)
		if err != nil {
			return err
		}

		err = EnsureUserAccount(tx, user)
		if err != nil {
			return err
		}

		var account types.UserAccount

		err = tx.Where("user_id = ?", id).First(&account).Error
		if err != nil {
			return err
		}

		if account.IsAdmin && !enabled {
			var count int64

			err = tx.Model(&types.UserAccount{}).Where("is_admin = ?", true).Count(&count).Error
			if err != nil {
				return err
			}

			if count <= 1 {
				return ErrLastAdmin
			}
		}

		return tx.Model(&account).Updates(map[string]any{"is_admin": enabled, accountRevisionColumn: gorm.Expr("revision + 1")}).Error
	})
}

func (hsdb *HSDatabase) ResetUserPassword(id types.UserID) (string, error) {
	var entropy [24]byte

	_, err := rand.Read(entropy[:])
	if err != nil {
		return "", err
	}

	password := base64.RawURLEncoding.EncodeToString(entropy[:])
	hash := hashWebAdminPassword(password)

	err = hsdb.Write(func(tx *gorm.DB) error {
		user, err := GetUserByID(tx, id)
		if err != nil {
			return err
		}

		err = EnsureUserAccount(tx, user)
		if err != nil {
			return err
		}

		return tx.Model(&types.UserAccount{}).Where("user_id = ?", id).Updates(map[string]any{accountPasswordColumn: hash, "must_change_password": true, accountRevisionColumn: gorm.Expr("revision + 1")}).Error
	})
	if err != nil {
		return "", err
	}

	return password, nil
}

func (hsdb *HSDatabase) ChangeUserPassword(id types.UserID, revision uint64, current, password string, oidc bool) error {
	err := validateWebAdminPassword(password)
	if err != nil {
		return err
	}

	account, err := hsdb.GetUserAccount(id)
	if err != nil {
		return err
	}

	if account.Revision != revision {
		return ErrAccountChanged
	}

	webAdminHashLimiter <- struct{}{}
	defer func() { <-webAdminHashLimiter }()

	if !oidc && verifyArgon2id(account.PasswordHash, current) != nil {
		return ErrWebAdminInvalidCredentials
	}

	if len(account.PasswordHash) > 0 && verifyArgon2id(account.PasswordHash, password) == nil {
		return ErrPasswordReused
	}

	hash := hashWebAdminPassword(password)

	result := hsdb.DB.Model(&types.UserAccount{}).Where("user_id = ? AND revision = ?", id, revision).Updates(map[string]any{accountPasswordColumn: hash, "must_change_password": false, accountRevisionColumn: gorm.Expr("revision + 1")})
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected != 1 {
		return ErrAccountChanged
	}

	return nil
}

func ensureLegacyAccount(tx *gorm.DB, admin *types.WebAdmin) error {
	var account types.UserAccount

	err := tx.Where("legacy_login = ?", admin.Username).First(&account).Error
	if err == nil {
		return nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	user := types.User{Name: fmt.Sprintf("legacy-admin-%d", admin.ID), DisplayName: admin.Username}

	for suffix := 1; ; suffix++ {
		var count int64

		err = tx.Model(&types.User{}).Where("name = ?", user.Name).Count(&count).Error
		if err != nil {
			return err
		}

		if count == 0 {
			break
		}

		user.Name = fmt.Sprintf("legacy-admin-%d-%d", admin.ID, suffix)
	}

	err = tx.Create(&user).Error
	if err != nil {
		return err
	}

	account = types.UserAccount{UserID: uint64(user.ID), LegacyLogin: sql.NullString{String: admin.Username, Valid: true}, IsAdmin: true, PasswordHash: admin.PasswordHash, Revision: 1, LastLoginAt: admin.LastLoginAt}

	return tx.Create(&account).Error
}
