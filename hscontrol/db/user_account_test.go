package db

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/pquerna/otp/totp"
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

func TestAccountSecurityEncryptionAndSingleUse(t *testing.T) {
	database, err := newSQLiteTestDB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	user, err := database.CreateUser(types.User{Name: "secured", Email: "secured@example.test"})
	require.NoError(t, err)

	id := types.UserID(user.ID)
	account, err := database.GetUserAccount(id)
	require.NoError(t, err)

	generated, err := totp.Generate(totp.GenerateOpts{Issuer: "Meshora test", AccountName: "test@example.test"})
	require.NoError(t, err)

	secret := generated.Secret()
	key := bytes.Repeat([]byte{0x7b}, 32)
	recovery := sha256.Sum256([]byte("UNIQUE-RECOVERY"))
	hash := hex.EncodeToString(recovery[:])
	require.ErrorIs(t, database.EnableUserTOTP(id, account.Revision, secret, nil, 100, []string{hash}), ErrSecurityKey)
	require.NoError(t, database.EnableUserTOTP(id, account.Revision, secret, key, 100, []string{hash}))
	stored, err := database.UserSecurity(id)
	require.NoError(t, err)
	require.NotContains(t, string(stored.EncryptedTOTPSecret), secret)
	plaintext, err := OpenTOTP(stored, key)
	require.NoError(t, err)
	require.Equal(t, secret, plaintext)

	_, err = OpenTOTP(stored, bytes.Repeat([]byte{0x7c}, 32))
	require.Error(t, err)

	changed := *stored
	changed.UserID++
	_, err = OpenTOTP(&changed, key)
	require.Error(t, err)
	current, err := database.GetUserAccount(id)
	require.NoError(t, err)
	require.Greater(t, current.Revision, account.Revision)
	require.ErrorIs(t, database.ConsumeSecondFactor(id, current.Revision, 100, ""), ErrSecondFactor)
	require.NoError(t, database.ConsumeSecondFactor(id, current.Revision, 101, ""))
	require.ErrorIs(t, database.ConsumeSecondFactor(id, current.Revision, 101, ""), ErrSecondFactor)
	require.NoError(t, database.ConsumeSecondFactor(id, current.Revision, -1, hash))
	require.ErrorIs(t, database.ConsumeSecondFactor(id, current.Revision, -1, hash), ErrSecondFactor)
	_, err = database.ResetUserPassword(id)
	require.NoError(t, err)
	require.ErrorIs(t, database.ConsumeSecondFactor(id, current.Revision, 102, ""), ErrAccountChanged)
	still, err := database.UserSecurity(id)
	require.NoError(t, err)
	require.NotEmpty(t, still.EncryptedTOTPSecret)

	target, err := database.CreateUser(types.User{Name: "merge-target", Email: "other@example.test"})
	require.NoError(t, err)
	_, err = database.MergeUsers(id, types.UserID(target.ID), true)
	require.ErrorIs(t, err, ErrSecurityMerge)
}

func TestUserAuthPolicyRevocationAndRecoveryWindow(t *testing.T) {
	database, err := newSQLiteTestDB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	actor, err := database.CreateUser(types.User{Name: "support-admin", Email: "support@example.test"})
	require.NoError(t, err)
	target, err := database.CreateUser(types.User{Name: "support-target", Email: "target@example.test"})
	require.NoError(t, err)

	actorID, targetID := types.UserID(actor.ID), types.UserID(target.ID)
	actorAccount, err := database.GetUserAccount(actorID)
	require.NoError(t, err)
	require.ErrorIs(t, database.SetUserAuthPolicy(actorID, targetID, actorAccount.Revision, true, "keep", "keep", ""), ErrAuthPolicy)
	require.NoError(t, database.SetUserAdmin(actorID, true))
	actorAccount, err = database.GetUserAccount(actorID)
	require.NoError(t, err)
	before, err := database.GetUserAccount(targetID)
	require.NoError(t, err)
	require.ErrorIs(t, database.SetUserAuthPolicy(actorID, targetID, actorAccount.Revision, true, "365d", "keep", ""), ErrAuthPolicy)
	unchanged, err := database.GetUserAccount(targetID)
	require.NoError(t, err)
	require.Equal(t, before.Revision, unchanged.Revision)
	require.NoError(t, database.SetUserAuthPolicy(actorID, targetID, actorAccount.Revision, true, "15m", "1h", "Telefon verloren"))
	policy, err := database.UserAuthPolicy(targetID)
	require.NoError(t, err)
	require.True(t, policy.RequireStrongAuth)
	require.True(t, policy.TOTPBypassedAt(time.Now()))
	require.True(t, policy.PasskeysSuspendedAt(time.Now()))
	require.False(t, policy.TOTPBypassedAt(time.Now().Add(16*time.Minute)))
	require.EqualValues(t, actorID, policy.UpdatedBy)

	after, err := database.GetUserAccount(targetID)
	require.NoError(t, err)
	require.Greater(t, after.Revision, before.Revision)
	require.ErrorIs(t, database.ConsumeSecondFactor(targetID, before.Revision, 1, ""), ErrAccountChanged)

	until := *policy.TOTPSuspendedUntil

	require.NoError(t, database.SetUserAuthPolicy(actorID, targetID, actorAccount.Revision, true, "keep", "keep", "Pflicht beibehalten"))
	policy, err = database.UserAuthPolicy(targetID)
	require.NoError(t, err)
	require.True(t, until.Equal(*policy.TOTPSuspendedUntil))

	account, err := database.GetUserAccount(targetID)
	require.NoError(t, err)
	generated, err := totp.Generate(totp.GenerateOpts{Issuer: "Test", AccountName: target.Email})
	require.NoError(t, err)

	key := bytes.Repeat([]byte{0x7a}, 32)
	require.NoError(t, database.EnableUserTOTP(targetID, account.Revision, generated.Secret(), key, 1, nil))
	policy, err = database.UserAuthPolicy(targetID)
	require.NoError(t, err)
	require.Nil(t, policy.TOTPSuspendedUntil, "finishing recovery must resume TOTP immediately")
	require.True(t, policy.RequireStrongAuth)
	require.True(t, policy.PasskeysSuspendedAt(time.Now()))
	require.NoError(t, database.SetUserAuthPolicy(actorID, targetID, actorAccount.Revision, true, "resume", "resume", "Support beendet"))
	policy, err = database.UserAuthPolicy(targetID)
	require.NoError(t, err)
	require.Nil(t, policy.PasskeysSuspendedUntil)
}
