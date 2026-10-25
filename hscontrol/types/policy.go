package types

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

var (
	ErrPolicyNotFound         = errors.New("acl policy not found")
	ErrPolicyUpdateIsDisabled = errors.New("update is disabled for modes other than 'database'")
)

// Policy represents a policy in the database.
type Policy struct {
	gorm.Model

	// Data contains the policy in HuJSON format.
	Data string
}

// PolicySnapshot is the original input accepted by the running policy manager.
// Revision is process-local; source persistence and client delivery are separate.
type PolicySnapshot struct {
	Text     string
	SHA256   string
	Revision uint64
	LoadedAt time.Time
	Implicit bool
}
