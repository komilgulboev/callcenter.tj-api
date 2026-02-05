package auth

import (
	"errors"

	"github.com/golang-jwt/jwt/v5"
)

func ParseToken(tokenStr string, secret string) (*AuthContext, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}

	claims := token.Claims.(jwt.MapClaims)

	return &AuthContext{
		UserID:   int(claims["sub"].(float64)),
		TenantID: int(claims["tenantId"].(float64)),
		Username: claims["username"].(string),
	}, nil
}
