package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type JWTClaims struct {
	UserID    int
	Username  string
	TenantID  int
	UserType  int
	ExpiresAt time.Time
}

func (c JWTClaims) ToMapClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"sub":      c.UserID,
		"username": c.Username,
		"tenantId": c.TenantID,
		"userType": c.UserType,
		"exp":      c.ExpiresAt.Unix(),
	}
}

func GenerateJWT(c JWTClaims, secret string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c.ToMapClaims())
	return token.SignedString([]byte(secret))
}

func ParseJWT(tokenStr string, secret string) (*AuthContext, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return nil, err
	}

	claims := token.Claims.(jwt.MapClaims)

	return &AuthContext{
		UserID:   int(claims["sub"].(float64)),
		Username: claims["username"].(string),
		TenantID: int(claims["tenantId"].(float64)),
		UserType: int(claims["userType"].(float64)),
	}, nil
}


