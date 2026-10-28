package db

import (
	"strings"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/require"
)

func TestUserEmailConfirmationIsAtomicAndSingleUse(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			var database *HSDatabase

			if backend == "sqlite" {
				var err error

				database, err = newSQLiteTestDB()
				require.NoError(t, err)
				t.Cleanup(func() { _ = database.Close() })
			} else {
				database = newPostgresTestDB(t)
			}

			user, err := database.CreateUser(types.User{Name: "email-user", Email: "old@example.test"})
			require.NoError(t, err)
			other, err := database.CreateUser(types.User{Name: "other-email-user"})
			require.NoError(t, err)

			id := types.UserID(user.ID)
			account, err := database.GetUserAccount(id)
			require.NoError(t, err)

			tokenHash := strings.Repeat("a", 64)
			require.NoError(t, database.BeginUserEmailChange(id, account.Revision, "NEW@example.test", tokenHash))
			unchanged, err := database.GetUserByID(id)
			require.NoError(t, err)
			require.Equal(t, "old@example.test", unchanged.Email)

			_, err = database.ConfirmUserEmailChange(types.UserID(other.ID), 1, tokenHash)
			require.Error(t, err)
			_, err = database.ConfirmUserEmailChange(id, account.Revision, strings.Repeat("b", 64))
			require.Error(t, err)
			confirmed, err := database.ConfirmUserEmailChange(id, account.Revision, tokenHash)
			require.NoError(t, err)
			require.Equal(t, user.ID, confirmed.ID)
			require.Equal(t, "new@example.test", confirmed.Email)

			changed, err := database.GetUserAccount(id)
			require.NoError(t, err)
			require.Greater(t, changed.Revision, account.Revision)
			require.Equal(t, "new@example.test", changed.LoginEmail.String)
			_, err = database.ConfirmUserEmailChange(id, changed.Revision, tokenHash)
			require.Error(t, err)
			pending, err := database.PendingUserEmailChange(id)
			require.NoError(t, err)
			require.Nil(t, pending)
		})
	}
}

func TestUserEmailConfirmationExpiryReissueAndConflict(t *testing.T) {
	database, err := newSQLiteTestDB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	user, err := database.CreateUser(types.User{Name: "email-expiry", Email: "old@example.test"})
	require.NoError(t, err)

	id := types.UserID(user.ID)
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	require.NoError(t, database.BeginUserEmailChange(id, 1, "new@example.test", a))
	require.ErrorIs(t, database.BeginUserEmailChange(id, 1, "next@example.test", b), ErrEmailChangeRate)
	require.NoError(t, database.DB.Model(&types.UserEmailChange{}).Where("user_id = ?", id).Updates(map[string]any{"requested_at": time.Now().Add(-2 * time.Minute), "expires_at": time.Now().Add(-time.Minute)}).Error)
	_, err = database.ConfirmUserEmailChange(id, 1, a)
	require.Error(t, err)
	require.NoError(t, database.BeginUserEmailChange(id, 1, "next@example.test", b))
	_, err = database.ConfirmUserEmailChange(id, 1, a)
	require.Error(t, err)
	_, err = database.CreateUser(types.User{Name: "reserved-email", Email: "next@example.test"})
	require.NoError(t, err)
	_, err = database.ConfirmUserEmailChange(id, 1, b)
	require.Error(t, err)
	unchanged, err := database.GetUserByID(id)
	require.NoError(t, err)
	require.Equal(t, "old@example.test", unchanged.Email)

	account, err := database.GetUserAccount(id)
	require.NoError(t, err)
	require.Equal(t, uint64(1), account.Revision)
}

func TestUserEmailAdminBlockRevokesPendingProof(t *testing.T) {
	database, err := newSQLiteTestDB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	admin, err := database.CreateUser(types.User{Name: "email-admin"})
	require.NoError(t, err)
	require.NoError(t, database.SetUserAdmin(types.UserID(admin.ID), true))
	actor, err := database.GetUserAccount(types.UserID(admin.ID))
	require.NoError(t, err)
	user, err := database.CreateUser(types.User{Name: "email-target"})
	require.NoError(t, err)

	id := types.UserID(user.ID)
	digest := strings.Repeat("c", 64)
	require.NoError(t, database.BeginUserEmailChange(id, 1, "new@example.test", digest))
	require.Error(t, database.SetUserEmailChangeBlocked(id, types.UserID(admin.ID), 1, true))
	require.NoError(t, database.SetUserEmailChangeBlocked(types.UserID(admin.ID), id, actor.Revision, true))
	target, err := database.GetUserAccount(id)
	require.NoError(t, err)
	_, err = database.ConfirmUserEmailChange(id, target.Revision, digest)
	require.Error(t, err)
	require.ErrorIs(t, database.BeginUserEmailChange(id, target.Revision, "new@example.test", digest), ErrEmailChange)
	policy, err := database.UserAuthPolicy(id)
	require.NoError(t, err)
	require.True(t, policy.EmailChangeBlocked)
	require.NoError(t, database.SetUserEmailChangeBlocked(types.UserID(admin.ID), id, actor.Revision, false))
	target, err = database.GetUserAccount(id)
	require.NoError(t, err)
	require.NoError(t, database.BeginUserEmailChange(id, target.Revision, "new@example.test", digest))
}

func TestUserEmailMigrationPreservesExistingPolicy(t *testing.T) {
	database, err := newSQLiteTestDB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	user, err := database.CreateUser(types.User{Name: "email-upgrade"})
	require.NoError(t, err)
	require.NoError(t, database.DB.Create(&types.UserAuthPolicy{UserID: uint64(user.ID), RequireStrongAuth: true}).Error)
	require.NoError(t, database.DB.Exec("DROP TABLE user_email_changes").Error)
	require.NoError(t, database.DB.Exec("ALTER TABLE user_auth_policies DROP COLUMN email_change_blocked").Error)
	require.NoError(t, ensureUserEmailChanges(database.DB))
	require.NoError(t, ensureUserEmailChanges(database.DB))
	policy, err := database.UserAuthPolicy(types.UserID(user.ID))
	require.NoError(t, err)
	require.True(t, policy.RequireStrongAuth)
	require.False(t, policy.EmailChangeBlocked)
	require.NoError(t, database.BeginUserEmailChange(types.UserID(user.ID), 1, "upgrade@example.test", strings.Repeat("d", 64)))
}

func TestUserEmailConcurrentConfirmationAndProviderProtection(t *testing.T) {
	database, err := newSQLiteTestDB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	user, err := database.CreateUser(types.User{Name: "email-concurrent"})
	require.NoError(t, err)

	id := types.UserID(user.ID)
	digest := strings.Repeat("d", 64)
	require.NoError(t, database.BeginUserEmailChange(id, 1, "concurrent@example.test", digest))

	results := make(chan error, 2)

	for range 2 {
		go func() { _, err := database.ConfirmUserEmailChange(id, 1, digest); results <- err }()
	}

	successes := 0

	for range 2 {
		if <-results == nil {
			successes++
		}
	}

	require.Equal(t, 1, successes)
	require.NoError(t, database.DB.Model(&types.User{}).Where("id = ?", id).Update("provider_identifier", "https://identity.example/subject").Error)
	account, err := database.GetUserAccount(id)
	require.NoError(t, err)
	require.ErrorIs(t, database.BeginUserEmailChange(id, account.Revision, "other@example.test", digest), ErrEmailChange)
}
