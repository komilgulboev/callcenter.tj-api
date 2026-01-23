package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type JWTClaims struct {
	UserID    int    `json:"sub"`
	Username  string `json:"username"`
	TenantID  int    `json:"tenantId"`
	UserType  int    `json:"userType"`
	Role      string `json:"role"`
	ExpiresAt time.Time
}

func (c JWTClaims) ToMapClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"sub":      c.UserID,
		"username": c.Username,
		"tenantId": c.TenantID,
		"userType": c.UserType,
		"role":     c.Role,
		"exp":      c.ExpiresAt.Unix(),
	}
}

func GenerateJWT(c JWTClaims, secret string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c.ToMapClaims())
	return token.SignedString([]byte(secret))
}
