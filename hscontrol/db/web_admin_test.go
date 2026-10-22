package db

import (
	"bytes"
	"errors"
	"testing"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testWebAdminPassword = "correct horse battery"

func TestWebAdminLifecycle(t *testing.T) {
	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	admin, err := db.CreateWebAdmin("alice", testWebAdminPassword)
	require.NoError(t, err)
	assert.Equal(t, "alice", admin.Username)
	assert.True(t, bytes.HasPrefix(admin.PasswordHash, []byte("$argon2id$")))
	assert.NotContains(t, string(admin.PasswordHash), testWebAdminPassword)

	_, err = db.CreateWebAdmin("alice", testWebAdminPassword)
	require.ErrorIs(t, err, ErrWebAdminExists)

	got, err := db.AuthenticateWebAdmin("alice", testWebAdminPassword)
	require.NoError(t, err)
	assert.Equal(t, admin.ID, got.ID)
	assert.NotNil(t, got.LastLoginAt)

	_, err = db.AuthenticateWebAdmin("alice", "wrong password!!")
	require.ErrorIs(t, err, ErrWebAdminInvalidCredentials)

	_, err = db.AuthenticateWebAdmin("nobody", testWebAdminPassword)
	require.ErrorIs(t, err, ErrWebAdminInvalidCredentials)

	require.NoError(t, db.SetWebAdminPassword("alice", "another long password"))
	_, err = db.AuthenticateWebAdmin("alice", testWebAdminPassword)
	require.ErrorIs(t, err, ErrWebAdminInvalidCredentials)
	_, err = db.AuthenticateWebAdmin("alice", "another long password")
	require.NoError(t, err)

	admins, err := db.ListWebAdmins()
	require.NoError(t, err)
	assert.Len(t, admins, 1)

	require.NoError(t, db.DeleteWebAdmin("alice"))
	require.ErrorIs(t, db.DeleteWebAdmin("alice"), ErrWebAdminNotFound)
	require.ErrorIs(t, db.SetWebAdminPassword("alice", testWebAdminPassword), ErrWebAdminNotFound)
}

func TestWebAdminValidation(t *testing.T) {
	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	_, err = db.CreateWebAdmin("bob", "short")
	require.ErrorIs(t, err, ErrWebAdminPasswordTooShort)

	_, err = db.CreateWebAdmin("", testWebAdminPassword)
	require.Error(t, err)

	require.ErrorIs(t, db.SetWebAdminPassword("bob", "short"), ErrWebAdminPasswordTooShort)
}

func TestWebAdminHashIsVerifiable(t *testing.T) {
	hash := hashWebAdminPassword(testWebAdminPassword)
	require.NoError(t, verifyArgon2id(hash, testWebAdminPassword))
	require.Error(t, verifyArgon2id(hash, "not the password"))
	assert.NotEqual(t, hash, hashWebAdminPassword(testWebAdminPassword), "salt must differ")
}

var errSetupConfigWrite = errors.New("config write failed")

func TestInitialWebAdminClosesSetupAndRollsBackConfigFailure(t *testing.T) {
	database, err := newSQLiteTestDB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	available, err := database.InitialSetupAvailable()
	require.NoError(t, err)
	require.True(t, available)

	failure := errSetupConfigWrite
	err = database.CreateInitialWebAdmin("owner", testWebAdminPassword, func() error { return failure })
	require.ErrorIs(t, err, failure)
	admins, err := database.ListWebAdmins()
	require.NoError(t, err)
	require.Empty(t, admins)
	require.NoError(t, database.CreateInitialWebAdmin("owner", testWebAdminPassword, func() error { return nil }))
	err = database.CreateInitialWebAdmin("attacker", testWebAdminPassword, func() error { t.Fatal("closed setup must not write configuration"); return nil })
	require.ErrorIs(t, err, ErrInitialSetupClosed)
	admins, err = database.ListWebAdmins()
	require.NoError(t, err)
	require.Len(t, admins, 1)
}

func TestInitialWebAdminDoesNotTakeOverExistingTailnet(t *testing.T) {
	database, err := newSQLiteTestDB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	_, err = database.CreateUser(types.User{Name: "existing"})
	require.NoError(t, err)
	available, err := database.InitialSetupAvailable()
	require.NoError(t, err)
	require.False(t, available)

	err = database.CreateInitialWebAdmin("attacker", testWebAdminPassword, func() error { t.Fatal("existing tailnet must not be modified"); return nil })
	require.ErrorIs(t, err, ErrInitialSetupClosed)
}
