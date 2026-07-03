package csilservices

import (
	"context"

	"github.com/catalystcommunity/firepit/api/internal/csil"
	"github.com/catalystcommunity/firepit/api/internal/store"
)

// authService is the stub AuthService implementation (task B1). B2 replaces
// every method body below with the linkkeys RP login flow; see the package
// doc comment (doc.go) for the error-handling contract every replacement
// must follow.
type authService struct {
	store *store.Store
}

// NewAuthService constructs the AuthService implementation.
func NewAuthService(st *store.Store) csil.AuthService {
	return &authService{store: st}
}

func (s *authService) BeginLogin(ctx context.Context, req csil.BeginLoginRequest) (csil.BeginLoginResponse, error) {
	return csil.BeginLoginResponse{}, Unimplemented("AuthService.begin-login")
}

func (s *authService) Logout(ctx context.Context, req csil.Empty) (csil.Empty, error) {
	// logout has no declared ServiceError arm (csil/firepit.csil: "never
	// errors even if already logged out"), so this stub error becomes a
	// transport-level failure until B2 replaces it with the real (always
	// succeeding) implementation.
	return csil.Empty{}, Unimplemented("AuthService.logout")
}

func (s *authService) Whoami(ctx context.Context, req csil.Empty) (csil.UserProfile, error) {
	return csil.UserProfile{}, Unimplemented("AuthService.whoami")
}
