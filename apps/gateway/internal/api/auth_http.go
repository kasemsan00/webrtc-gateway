package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"k2-gateway/internal/auth"
)

type authClaimsContextKey struct{}

// AuthClaimsFromContext returns verified claims from request context.
func AuthClaimsFromContext(ctx context.Context) (*auth.VerifiedClaims, bool) {
	claims, ok := ctx.Value(authClaimsContextKey{}).(*auth.VerifiedClaims)
	return claims, ok
}

func withAuthClaims(r *http.Request, claims *auth.VerifiedClaims) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), authClaimsContextKey{}, claims))
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || s.tokenVerifier == nil {
			next.ServeHTTP(w, r)
			return
		}

		token, ok := extractBearerToken(r.Header.Get("Authorization"))
		if !ok {
			writeUnauthorized(w)
			return
		}

		realmHint := extractAuthRealmHint(r)
		claims, err := s.tokenVerifier.VerifyToken(r.Context(), token, realmHint)
		if err != nil {
			log.Printf("REST auth rejected: path=%s hint=%s err=%v", r.URL.Path, realmHint, err)
			writeUnauthorized(w)
			return
		}
		log.Printf("REST auth accepted: path=%s hint=%s realm=%s sub=%s", r.URL.Path, realmHint, claims.Realm, claims.Subject)

		next.ServeHTTP(w, withAuthClaims(r, claims))
	})
}

func extractAuthRealmHint(r *http.Request) auth.TokenRealm {
	hint := strings.TrimSpace(r.Header.Get("X-Auth-Type"))
	if hint == "" {
		hint = strings.TrimSpace(r.URL.Query().Get("auth_type"))
	}
	return auth.ParseTokenRealm(hint)
}

func extractBearerToken(header string) (string, bool) {
	value := strings.TrimSpace(header)
	if value == "" {
		return "", false
	}

	parts := strings.SplitN(value, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", false
	}
	return token, true
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": "Unauthorized",
	})
}
