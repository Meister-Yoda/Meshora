package db

import (
	"testing"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/require"
)

func TestUnifiedAccountPasswordAndAdmin(t *testing.T) {
	database, err := newSQLiteTestDB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	first, err := database.CreateUser(types.User{Name: "first", Email: "First@Example.test"})
	require.NoError(t, err)
	second, err := database.CreateUser(types.User{Name: "second", Email: "second@example.test"})
	require.NoError(t, err)
	_, err = database.CreateUser(types.User{Name: "duplicate", Email: "first@example.test"})
	require.ErrorIs(t, err, ErrAccountConflict)
	account, err := database.GetUserAccount(types.UserID(first.ID))
	require.NoError(t, err)
	require.False(t, account.IsAdmin)
	require.Empty(t, account.PasswordHash)

	temporary, err := database.ResetUserPassword(types.UserID(first.ID))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(temporary), 24)
	account, err = database.AuthenticateUserAccount("FIRST@example.test", temporary)
	require.NoError(t, err)
	require.True(t, account.MustChangePassword)
	require.ErrorIs(t, database.ChangeUserPassword(types.UserID(first.ID), account.Revision, temporary, temporary, false), ErrPasswordReused)
	require.NoError(t, database.ChangeUserPassword(types.UserID(first.ID), account.Revision, temporary, "new personal password", false))
	_, err = database.AuthenticateUserAccount("first@example.test", temporary)
	require.ErrorIs(t, err, ErrWebAdminInvalidCredentials)
	account, err = database.AuthenticateUserAccount("first@example.test", "new personal password")
	require.NoError(t, err)
	require.False(t, account.MustChangePassword)
	require.NoError(t, database.SetUserAdmin(types.UserID(first.ID), true))
	require.ErrorIs(t, database.SetUserAdmin(types.UserID(first.ID), false), ErrLastAdmin)
	require.NoError(t, database.SetUserAdmin(types.UserID(second.ID), true))
	require.NoError(t, database.SetUserAdmin(types.UserID(first.ID), false))
}

func TestUnifiedAccountMigrationKeepsLegacyPasswordAndUIDs(t *testing.T) {
	database, err := newSQLiteTestDB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	user, err := database.CreateUser(types.User{Name: "existing", Email: "existing@example.test"})
	require.NoError(t, err)

	legacy := types.WebAdmin{Username: "old-admin", PasswordHash: hashWebAdminPassword(testWebAdminPassword)}
	require.NoError(t, database.DB.Create(&legacy).Error)
	require.NoError(t, database.DB.Exec("DROP TABLE user_accounts").Error)
	require.NoError(t, ensureUserAccounts(database.DB))
	unchanged, err := database.GetUserByID(types.UserID(user.ID))
	require.NoError(t, err)
	require.Equal(t, user.ID, unchanged.ID)

	account, err := database.AuthenticateUserAccount("old-admin", testWebAdminPassword)
	require.NoError(t, err)
	require.True(t, account.IsAdmin)
	require.NotEqual(t, uint64(user.ID), account.UserID)
	require.NoError(t, ensureUserAccounts(database.DB))

	var count int64
	require.NoError(t, database.DB.Model(&types.UserAccount{}).Where("legacy_login = ?", "old-admin").Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestUnifiedAccountExplicitMerge(t *testing.T) {
	database, err := newSQLiteTestDB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	source, err := database.CreateUser(types.User{Name: "legacy-source"})
	require.NoError(t, err)
	target, err := database.CreateUser(types.User{Name: "canonical", Email: "person@example.test"})
	require.NoError(t, err)
	password, err := database.ResetUserPassword(types.UserID(source.ID))
	require.NoError(t, err)
	require.NoError(t, database.SetUserAdmin(types.UserID(source.ID), true))
	before, err := database.GetUserAccount(types.UserID(target.ID))
	require.NoError(t, err)
	merged, err := database.MergeUsers(types.UserID(source.ID), types.UserID(target.ID), false)
	require.NoError(t, err)
	require.Equal(t, target.ID, merged.ID)
	_, err = database.GetUserByID(types.UserID(source.ID))
	require.Error(t, err)
	account, err := database.AuthenticateUserAccount(target.Email, password)
	require.NoError(t, err)
	require.EqualValues(t, target.ID, account.UserID)
	require.True(t, account.IsAdmin)
	require.True(t, account.MustChangePassword)
	require.Greater(t, account.Revision, before.Revision)

	_, err = database.CreateUser(types.User{Name: "new", Email: "next@example.test"})
	require.NoError(t, err)
}
