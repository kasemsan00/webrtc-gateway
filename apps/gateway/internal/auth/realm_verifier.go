package auth

import (
	"context"
	"fmt"
	"strings"
)

// TokenRealm identifies which JWT realm validated a token.
type TokenRealm string

const (
	TokenRealmUnknown  TokenRealm = "unknown"
	TokenRealmUser     TokenRealm = "user"
	TokenRealmEmployee TokenRealm = "employee"
)

// ParseTokenRealm normalizes realm hints from query/header inputs.
func ParseTokenRealm(raw string) TokenRealm {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "user", "users", "client":
		return TokenRealmUser
	case "employee", "employees", "agent":
		return TokenRealmEmployee
	default:
		return TokenRealmUnknown
	}
}

// RealmVerifier verifies tokens against user and employee JWT realms.
// Hybrid behavior:
// - If hint is provided, verify hinted realm first.
// - On failure or unknown hint, fallback to the other configured realm.
type RealmVerifier struct {
	userVerifier     *Verifier
	employeeVerifier *Verifier
}

// NewRealmVerifier creates a hybrid verifier. At least one verifier is required.
func NewRealmVerifier(userVerifier, employeeVerifier *Verifier) (*RealmVerifier, error) {
	if userVerifier == nil && employeeVerifier == nil {
		return nil, fmt.Errorf("at least one realm verifier must be configured")
	}
	return &RealmVerifier{
		userVerifier:     userVerifier,
		employeeVerifier: employeeVerifier,
	}, nil
}

// VerifyToken verifies a JWT and resolves which realm accepted it.
func (v *RealmVerifier) VerifyToken(ctx context.Context, rawToken string, hint TokenRealm) (*VerifiedClaims, error) {
	tryOrder := v.resolveTryOrder(hint)
	var lastErr error

	for _, realm := range tryOrder {
		claims, err := v.verifyWithRealm(ctx, rawToken, realm)
		if err == nil {
			claims.Realm = realm
			return claims, nil
		}
		lastErr = err
	}

	if lastErr == nil {
		return nil, fmt.Errorf("no verifier available")
	}
	return nil, fmt.Errorf("token verification failed for all configured realms: %w", lastErr)
}

func (v *RealmVerifier) resolveTryOrder(hint TokenRealm) []TokenRealm {
	if v.userVerifier != nil && v.employeeVerifier != nil {
		switch hint {
		case TokenRealmEmployee:
			return []TokenRealm{TokenRealmEmployee, TokenRealmUser}
		case TokenRealmUser:
			return []TokenRealm{TokenRealmUser, TokenRealmEmployee}
		default:
			return []TokenRealm{TokenRealmUser, TokenRealmEmployee}
		}
	}

	if v.userVerifier != nil {
		return []TokenRealm{TokenRealmUser}
	}
	if v.employeeVerifier != nil {
		return []TokenRealm{TokenRealmEmployee}
	}
	return []TokenRealm{}
}

func (v *RealmVerifier) verifyWithRealm(ctx context.Context, rawToken string, realm TokenRealm) (*VerifiedClaims, error) {
	switch realm {
	case TokenRealmUser:
		if v.userVerifier == nil {
			return nil, fmt.Errorf("user verifier is not configured")
		}
		return v.userVerifier.VerifyToken(ctx, rawToken)
	case TokenRealmEmployee:
		if v.employeeVerifier == nil {
			return nil, fmt.Errorf("employee verifier is not configured")
		}
		return v.employeeVerifier.VerifyToken(ctx, rawToken)
	default:
		return nil, fmt.Errorf("unsupported realm %q", realm)
	}
}
