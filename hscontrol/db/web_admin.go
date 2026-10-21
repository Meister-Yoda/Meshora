package db

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/util"
	"golang.org/x/crypto/argon2"
	"gorm.io/gorm"
)

// Argon2id parameters for admin passwords (OWASP minimum: 19 MiB, t=2, p=1).
// Unlike credential secrets, passwords are user-chosen and guessable, so they
// need a deliberately slow hash (see the note on hashPrefixSHA256).
const (
	webAdminArgon2Memory  = 19 * 1024
	webAdminArgon2Time    = 2
	webAdminArgon2Threads = 1
	webAdminSaltLength    = 16

	// WebAdminMinPasswordLength is the shortest password accepted.
	WebAdminMinPasswordLength = 12
)

var (
	ErrWebAdminExists             = errors.New("web admin already exists")
	ErrWebAdminNotFound           = errors.New("web admin not found")
	ErrWebAdminInvalidCredentials = errors.New("invalid username or password")
	ErrWebAdminUsernameInvalid    = errors.New("invalid web admin username")
	ErrWebAdminPasswordTooShort   = fmt.Errorf(
		"password must be at least %d characters",
		WebAdminMinPasswordLength,
	)
)

// webAdminHashLimiter bounds concurrent Argon2id runs: the login form is
// unauthenticated, and each run allocates webAdminArgon2Memory.
var webAdminHashLimiter = make(chan struct{}, max(2, runtime.GOMAXPROCS(0)))

// dummyWebAdminHash is verified against when the username is unknown, so a
// login for a missing account costs as much as one with a wrong password.
var dummyWebAdminHash = hashWebAdminPassword("meshora-dummy-password")

const webAdminsDDLSQLite = `CREATE TABLE web_admins(
  id integer PRIMARY KEY AUTOINCREMENT,
  username text NOT NULL,
  password_hash blob NOT NULL,
  created_at datetime,
  updated_at datetime,
  last_login_at datetime
)`

// webAdminsDDLPostgres is the Postgres form of [webAdminsDDLSQLite].
const webAdminsDDLPostgres = `CREATE TABLE web_admins(
  id bigserial PRIMARY KEY,
  username text NOT NULL,
  password_hash bytea NOT NULL,
  created_at timestamptz,
  updated_at timestamptz,
  last_login_at timestamptz
)`

// ensureWebAdminsTable creates the web_admins table and its index, and is a
// no-op once the table exists. Both InitSchema and the migration use it, so
// fresh and upgraded databases match.
func ensureWebAdminsTable(tx *gorm.DB) error {
	if tx.Migrator().HasTable(&types.WebAdmin{}) {
		return nil
	}

	return tx.Transaction(func(tx *gorm.DB) error {
		ddl := webAdminsDDLSQLite
		if tx.Name() == types.DatabasePostgres {
			ddl = webAdminsDDLPostgres
		}

		err := tx.Exec(ddl).Error
		if err != nil {
			return fmt.Errorf("creating web_admins table: %w", err)
		}

		err = tx.Exec(`CREATE UNIQUE INDEX idx_web_admins_username ON web_admins(username)`).Error
		if err != nil {
			return fmt.Errorf("creating web_admins index: %w", err)
		}

		return nil
	})
}

// hashWebAdminPassword returns the PHC-encoded Argon2id hash of password.
func hashWebAdminPassword(password string) []byte {
	salt := make([]byte, webAdminSaltLength)
	_, _ = rand.Read(salt) // crypto/rand.Read never fails

	key := argon2.IDKey(
		[]byte(password),
		salt,
		webAdminArgon2Time,
		webAdminArgon2Memory,
		webAdminArgon2Threads,
		argon2KeyLen,
	)

	return fmt.Appendf(nil, "$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		webAdminArgon2Memory,
		webAdminArgon2Time,
		webAdminArgon2Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

func validateWebAdminPassword(password string) error {
	if len([]rune(password)) < WebAdminMinPasswordLength {
		return ErrWebAdminPasswordTooShort
	}

	return nil
}

// CreateWebAdmin creates a [types.WebAdmin] with the given password.
func (hsdb *HSDatabase) CreateWebAdmin(username, password string) (*types.WebAdmin, error) {
	err := util.ValidateUsername(username)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrWebAdminUsernameInvalid, err)
	}

	err = validateWebAdminPassword(password)
	if err != nil {
		return nil, err
	}

	admin := types.WebAdmin{
		Username:     username,
		PasswordHash: hashWebAdminPassword(password),
	}

	return Write(hsdb.DB, func(tx *gorm.DB) (*types.WebAdmin, error) {
		var count int64

		err := tx.Model(&types.WebAdmin{}).Where("username = ?", username).Count(&count).Error
		if err != nil {
			return nil, err
		}

		if count > 0 {
			return nil, ErrWebAdminExists
		}

		err = tx.Create(&admin).Error
		if err != nil {
			return nil, fmt.Errorf("creating web admin: %w", err)
		}

		return &admin, nil
	})
}

// ListWebAdmins returns all [types.WebAdmin] accounts ordered by id.
func (hsdb *HSDatabase) ListWebAdmins() ([]types.WebAdmin, error) {
	return Read(hsdb.DB, func(rx *gorm.DB) ([]types.WebAdmin, error) {
		var admins []types.WebAdmin

		err := rx.Order("id").Find(&admins).Error

		return admins, err
	})
}

// GetWebAdminByID returns the [types.WebAdmin] with the given id.
func (hsdb *HSDatabase) GetWebAdminByID(id uint64) (*types.WebAdmin, error) {
	var admin types.WebAdmin

	err := hsdb.DB.Where("id = ?", id).First(&admin).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrWebAdminNotFound
	}

	if err != nil {
		return nil, err
	}

	return &admin, nil
}

// DeleteWebAdmin deletes the [types.WebAdmin] named username.
func (hsdb *HSDatabase) DeleteWebAdmin(username string) error {
	return hsdb.Write(func(tx *gorm.DB) error {
		res := tx.Where("username = ?", username).Delete(&types.WebAdmin{})
		if res.Error != nil {
			return res.Error
		}

		if res.RowsAffected == 0 {
			return ErrWebAdminNotFound
		}

		return nil
	})
}

// SetWebAdminPassword replaces the password of the [types.WebAdmin] named
// username.
func (hsdb *HSDatabase) SetWebAdminPassword(username, password string) error {
	err := validateWebAdminPassword(password)
	if err != nil {
		return err
	}

	hash := hashWebAdminPassword(password)

	return hsdb.Write(func(tx *gorm.DB) error {
		res := tx.Model(&types.WebAdmin{}).
			Where("username = ?", username).
			Updates(map[string]any{"password_hash": hash, "updated_at": time.Now()})
		if res.Error != nil {
			return res.Error
		}

		if res.RowsAffected == 0 {
			return ErrWebAdminNotFound
		}

		return nil
	})
}

// AuthenticateWebAdmin verifies username and password and records the login.
// An unknown username and a wrong password both return
// [ErrWebAdminInvalidCredentials] after the same amount of work.
func (hsdb *HSDatabase) AuthenticateWebAdmin(username, password string) (*types.WebAdmin, error) {
	var admin types.WebAdmin

	err := hsdb.DB.Where("username = ?", username).First(&admin).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	found := err == nil

	hash := dummyWebAdminHash
	if found {
		hash = admin.PasswordHash
	}

	webAdminHashLimiter <- struct{}{}

	verifyErr := verifyArgon2id(hash, password)

	<-webAdminHashLimiter

	if !found || verifyErr != nil {
		return nil, ErrWebAdminInvalidCredentials
	}

	now := time.Now()

	err = hsdb.DB.Model(&admin).UpdateColumn("last_login_at", now).Error
	if err != nil {
		return nil, fmt.Errorf("recording web admin login: %w", err)
	}

	admin.LastLoginAt = &now

	return &admin, nil
}
