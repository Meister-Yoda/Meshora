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
