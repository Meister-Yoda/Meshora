package types

import "time"

// WebAdmin is a local account that can log in to the web admin dashboard.
// It is not a tailnet [User]: it owns no nodes and never appears in a
// netmap or the policy.
type WebAdmin struct {
	ID       uint64 `gorm:"primary_key"`
	Username string `gorm:"uniqueIndex:idx_web_admins_username"`

	// PasswordHash is a PHC-encoded Argon2id hash; it never leaves the db
	// package.
	PasswordHash []byte `json:"-"`

	CreatedAt   time.Time
	UpdatedAt   time.Time
	LastLoginAt *time.Time
}
