package apiv1

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/types"
)

func init() {
	registrations = append(registrations, registerWebAdmins)
}

// WebAdmin is a local account of the web admin dashboard. The password hash
// is never returned.
type WebAdmin struct {
	ID          string     `format:"uint64"    json:"id"`
	Username    string     `json:"username"`
	CreatedAt   *time.Time `json:"createdAt"   nullable:"true"`
	LastLoginAt *time.Time `json:"lastLoginAt" nullable:"true"`
}

// CreateWebAdminRequestBody is the body of POST /api/v1/webadmin.
type CreateWebAdminRequestBody struct {
	Username string `json:"username" minLength:"1"`
	Password string `json:"password" minLength:"1"`
}

// SetWebAdminPasswordRequestBody is the body of
// POST /api/v1/webadmin/{username}/password.
type SetWebAdminPasswordRequestBody struct {
	Password string `json:"password" minLength:"1"`
}

type (
	createWebAdminInput struct {
		Body CreateWebAdminRequestBody
	}
	createWebAdminOutput struct {
		Body struct {
			WebAdmin WebAdmin `json:"webAdmin"`
		}
	}
)

type (
	listWebAdminsOutput struct {
		Body struct {
			WebAdmins []WebAdmin `json:"webAdmins" nullable:"false"`
		}
	}
)

type (
	deleteWebAdminInput struct {
		Username string `path:"username"`
	}
	deleteWebAdminOutput struct {
		Body struct{}
	}
)

type (
	setWebAdminPasswordInput struct {
		Username string `path:"username"`
		Body     SetWebAdminPasswordRequestBody
	}
	setWebAdminPasswordOutput struct {
		Body struct{}
	}
)

func registerWebAdmins(api huma.API, b Backend) {
	huma.Register(api, huma.Operation{
		OperationID: "createWebAdmin",
		Method:      http.MethodPost,
		Path:        "/api/v1/webadmin",
		Summary:     "Create web admin account",
		Tags:        []string{"WebAdmins"},
		Security:    bearerAuth,
	}, func(ctx context.Context, in *createWebAdminInput) (*createWebAdminOutput, error) {
		admin, err := b.State.CreateWebAdmin(in.Body.Username, in.Body.Password)
		if err != nil {
			return nil, mapError("creating web admin", err)
		}

		out := &createWebAdminOutput{}
		out.Body.WebAdmin = webAdminFromState(admin)

		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listWebAdmins",
		Method:      http.MethodGet,
		Path:        "/api/v1/webadmin",
		Summary:     "List web admin accounts",
		Tags:        []string{"WebAdmins"},
		Security:    bearerAuth,
	}, func(ctx context.Context, _ *struct{}) (*listWebAdminsOutput, error) {
		admins, err := b.State.ListWebAdmins()
		if err != nil {
			return nil, mapError("listing web admins", err)
		}

		out := &listWebAdminsOutput{}

		out.Body.WebAdmins = make([]WebAdmin, len(admins))
		for i := range admins {
			out.Body.WebAdmins[i] = webAdminFromState(&admins[i])
		}

		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "deleteWebAdmin",
		Method:      http.MethodDelete,
		Path:        "/api/v1/webadmin/{username}",
		Summary:     "Delete web admin account",
		Tags:        []string{"WebAdmins"},
		Security:    bearerAuth,
	}, func(ctx context.Context, in *deleteWebAdminInput) (*deleteWebAdminOutput, error) {
		err := b.State.DeleteWebAdmin(in.Username)
		if err != nil {
			return nil, mapError("deleting web admin", err)
		}

		return &deleteWebAdminOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "setWebAdminPassword",
		Method:      http.MethodPost,
		Path:        "/api/v1/webadmin/{username}/password",
		Summary:     "Set web admin password",
		Tags:        []string{"WebAdmins"},
		Security:    bearerAuth,
	}, func(ctx context.Context, in *setWebAdminPasswordInput) (*setWebAdminPasswordOutput, error) {
		err := b.State.SetWebAdminPassword(in.Username, in.Body.Password)
		if err != nil {
			return nil, mapError("setting web admin password", err)
		}

		return &setWebAdminPasswordOutput{}, nil
	})
}

func webAdminFromState(a *types.WebAdmin) WebAdmin {
	return WebAdmin{
		ID:          formatID(a.ID),
		Username:    a.Username,
		CreatedAt:   &a.CreatedAt,
		LastLoginAt: a.LastLoginAt,
	}
}
