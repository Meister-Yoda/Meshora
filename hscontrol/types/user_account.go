package types

import (
	"database/sql"
	"time"
)

// UserAccount stores login methods and privileges for one immutable User.ID.
// It is never an independent identity and never appears in client maps.
type UserAccount struct {
	UserID             uint64 `gorm:"primaryKey;autoIncrement:false"`
	LoginEmail         sql.NullString
	LegacyLogin        sql.NullString
	IsAdmin            bool
	PasswordHash       []byte `json:"-"`
	MustChangePassword bool
	Revision           uint64
	LastLoginAt        *time.Time
}

// UserSecurity never holds a plaintext TOTP secret or recovery code.
type UserSecurity struct {
	UserID              uint64 `gorm:"primaryKey;autoIncrement:false"`
	EncryptedTOTPSecret []byte `json:"-"`
	LastTOTPStep        int64
	RecoveryHashes      string `json:"-"`
}

// UserPasskey stores public credential data only; the private key stays on the authenticator.
type UserPasskey struct {
	ID             string `gorm:"primaryKey"`
	UserID         uint64
	Name           string
	CredentialJSON string `gorm:"column:credential" json:"-"`
	CreatedAt      time.Time
}

// UserAuthPolicy keeps mandatory authentication and support exceptions separate from credentials.
type UserAuthPolicy struct {
	EmailChangeBlocked     bool
	UserID                 uint64 `gorm:"primaryKey;autoIncrement:false"`
	RequireStrongAuth      bool
	TOTPSuspendedUntil     *time.Time
	PasskeysSuspendedUntil *time.Time
	UpdatedBy              uint64
	UpdatedAt              time.Time
	Reason                 string
}

func (p UserAuthPolicy) TOTPBypassedAt(now time.Time) bool {
	return p.TOTPSuspendedUntil != nil && now.Before(*p.TOTPSuspendedUntil)
}

func (p UserAuthPolicy) PasskeysSuspendedAt(now time.Time) bool {
	return p.PasskeysSuspendedUntil != nil && now.Before(*p.PasskeysSuspendedUntil)
}

// UserEmailChange stores only the digest of a short-lived, single-use proof.
// The requested address is not an identity until the proof is consumed.
type UserEmailChange struct {
	UserID          uint64 `gorm:"primaryKey;autoIncrement:false"`
	Email           string
	TokenHash       string `json:"-"`
	AccountRevision uint64
	RequestedAt     time.Time
	ExpiresAt       time.Time
}

// UserInvitation stores only a hash of the single-use acceptance token.
type UserInvitation struct {
	ID        string `gorm:"primaryKey"`
	Email     string
	Mode      string
	TokenHash string `json:"-"`
	CreatedBy uint64
	CreatedAt time.Time
	ExpiresAt time.Time
}
