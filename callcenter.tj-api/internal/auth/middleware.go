package auth

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const userContextKey contextKey = "user"

type AuthContext struct {
	UserID   int
	Username string
	TenantID int
	UserType int
}

func Middleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				http.Error(w, "missing token", http.StatusUnauthorized)
				return
			}

			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

			token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (any, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(secret), nil
			})

			if err != nil {
				log.Println("JWT PARSE ERROR:", err)
			}

			if err != nil || !token.Valid {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				http.Error(w, "invalid token claims", http.StatusUnauthorized)
				return
			}

			// 🔎 ЛОГИ ВРЕМЕНИ
			exp, _ := claims["exp"].(float64)

			log.Println("JWT VERIFY:")
			log.Println("  NOW (server):", time.Now())
			log.Println("  NOW (unix):  ", time.Now().Unix())
			log.Println("  EXP (unix):  ", int64(exp))

			ctx := context.WithValue(r.Context(), userContextKey, AuthContext{
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
	return ctx.Value(userContextKey).(AuthContext)
}
