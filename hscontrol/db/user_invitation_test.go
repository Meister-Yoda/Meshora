package db

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/require"
)

func TestUserInvitations(t *testing.T) {
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

			actor, err := database.CreateUser(types.User{Name: "invite-admin"})
			require.NoError(t, err)

			uid := types.UserID(actor.ID)
			require.NoError(t, database.SetUserAdmin(uid, true))
			account, err := database.GetUserAccount(uid)
			require.NoError(t, err)

			create := func(email, mode, id, hash string) {
				require.NoError(t, database.CreateUserInvitation(uid, account.Revision, email, mode, strings.Repeat(id, 43), strings.Repeat(hash, 64)))
			}
			create("Local@Example.test", "local", "a", "a")

			user, err := database.AcceptUserInvitation(strings.Repeat("a", 64), "local", "a long invited password", nil)
			require.NoError(t, err)
			require.Equal(t, "local@example.test", user.Email)
			localAccount, err := database.GetUserAccount(types.UserID(user.ID))
			require.NoError(t, err)
			require.False(t, localAccount.IsAdmin)
			require.False(t, localAccount.MustChangePassword)

			_, err = database.AuthenticateUserAccount(user.Email, "a long invited password")
			require.NoError(t, err)
			_, err = database.AcceptUserInvitation(strings.Repeat("a", 64), "local", "a long invited password", nil)
			require.Error(t, err)
			require.Error(t, database.CreateUserInvitation(uid, account.Revision, user.Email, "local", strings.Repeat("z", 43), strings.Repeat("z", 64)))
			create("oidc@example.test", "oidc", "b", "b")

			claims := &types.OIDCClaims{Iss: "https://issuer.example.test", Sub: "subject", Email: "wrong@example.test", EmailVerified: true}
			_, err = database.AcceptUserInvitation(strings.Repeat("b", 64), "oidc", "", claims)
			require.ErrorIs(t, err, ErrInvitation)

			claims.Email = "oidc@example.test"
			claims.EmailVerified = false
			_, err = database.AcceptUserInvitation(strings.Repeat("b", 64), "oidc", "", claims)
			require.ErrorIs(t, err, ErrInvitation)

			claims.EmailVerified = true
			user, err = database.AcceptUserInvitation(strings.Repeat("b", 64), "oidc", "", claims)
			require.NoError(t, err)
			require.True(t, user.ProviderIdentifier.Valid)
			oidcAccount, err := database.GetUserAccount(types.UserID(user.ID))
			require.NoError(t, err)
			require.Empty(t, oidcAccount.PasswordHash)
			require.False(t, oidcAccount.IsAdmin)
			create("expired@example.test", "local", "c", "c")
			require.NoError(t, database.DB.Model(&types.UserInvitation{}).Where("email = ?", "expired@example.test").Update("expires_at", time.Now().Add(-time.Minute)).Error)
			_, err = database.AcceptUserInvitation(strings.Repeat("c", 64), "local", "a long invited password", nil)
			require.ErrorIs(t, err, ErrInvitation)
			create("repeat@example.test", "local", "d", "d")
			create("repeat@example.test", "local", "e", "e")

			_, err = database.UserInvitation(strings.Repeat("d", 64))
			require.ErrorIs(t, err, ErrInvitation)
			require.NoError(t, database.RevokeUserInvitation(uid, account.Revision, strings.Repeat("e", 43)))
			_, err = database.UserInvitation(strings.Repeat("e", 64))
			require.ErrorIs(t, err, ErrInvitation)
			create("concurrent@example.test", "oidc", "f", "f")

			claims = &types.OIDCClaims{Iss: "https://issuer.example.test", Sub: "concurrent", Email: "concurrent@example.test", EmailVerified: true}

			var (
				count atomic.Int32
				group sync.WaitGroup
			)
			for range 2 {
				group.Go(func() {
					_, acceptErr := database.AcceptUserInvitation(strings.Repeat("f", 64), "oidc", "", claims)
					if acceptErr == nil {
						count.Add(1)
					}
				})
			}

			group.Wait()
			require.Equal(t, int32(1), count.Load())
		})
	}
}
