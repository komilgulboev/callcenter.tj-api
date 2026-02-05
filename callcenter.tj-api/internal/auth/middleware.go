package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type ctxKey string

const userKey ctxKey = "user"

type AuthContext struct {
	UserID   int `json:"id"`
	Username string
	TenantID int
	UserType int
}

func Middleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") {
				http.Error(w, "unauthorized", 401)
				return
			}

			t := strings.TrimPrefix(h, "Bearer ")
			claims := jwt.MapClaims{}
			_, err := jwt.ParseWithClaims(t, claims, func(*jwt.Token) (any, error) {
				return []byte(secret), nil
			})
			if err != nil {
				http.Error(w, "unauthorized", 401)
				return
			}

			ctx := context.WithValue(r.Context(), userKey, AuthContext{
				UserID:   int(claims["sub"].(float64)),
				Username: claims["username"].(string),
				TenantID: int(claims["tenantId"].(float64)),
				UserType: int(claims["userType"].(float64)),
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func FromContext(ctx context.Context) AuthContext {
	return ctx.Value(userKey).(AuthContext)
}
