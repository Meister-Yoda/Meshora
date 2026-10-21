package db

import (
	"bytes"
	"testing"

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
