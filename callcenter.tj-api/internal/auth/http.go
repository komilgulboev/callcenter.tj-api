package auth

import (
	"errors"
	"net/http"
	"strings"
)

func ParseJWTFromRequest(r *http.Request, secret string) (*AuthContext, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return nil, errors.New("missing Authorization header")
	}

	if !strings.HasPrefix(h, "Bearer ") {
		return nil, errors.New("invalid Authorization header")
	}

	token := strings.TrimPrefix(h, "Bearer ")
	return ParseJWT(token, secret)
}
