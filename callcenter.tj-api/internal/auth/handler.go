package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
    "log"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
}

type User struct {
	ID       int
	Username string
	PasswordHash string
	TenantID int
	UserType int
	Status   string
}

type Handler struct {
	DB     DB
	Secret string
	TTL    time.Duration
}

type DB interface {
	QueryRow(ctx context.Context, query string, args ...any) pgx.Row
}

// Login godoc
//
// @Summary      User login
// @Description  Authenticate user and return JWT token
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        credentials body LoginRequest true "Login credentials"
// @Success      200 {object} LoginResponse
// @Failure      400 {string} string "invalid request"
// @Failure      401 {string} string "invalid username or password"
// @Failure      403 {string} string "user disabled"
// @Router       /api/auth/login [post]
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	var u User

	err := h.DB.QueryRow(
		r.Context(),
		`
		SELECT
			id,
			username,
			password_hash,
			tenant_id,
			type,
			status
		FROM users
		WHERE username = $1
		`,
		req.Username,
	).Scan(
		&u.ID,
		&u.Username,
		&u.PasswordHash,
		&u.TenantID,
		&u.UserType,
		&u.Status,
	)

	if err != nil {
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
		return
	}

	if u.Status != "enable" {
		http.Error(w, "user disabled", http.StatusForbidden)
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
		return
	}

expTime := time.Now().Add(h.TTL)

log.Println("JWT GENERATE:")
log.Println("  NOW (server):", time.Now())
log.Println("  NOW (unix):  ", time.Now().Unix())
log.Println("  EXP (time):  ", expTime)
log.Println("  EXP (unix):  ", expTime.Unix())

token, err := GenerateJWT(JWTClaims{
	UserID:    u.ID,
	Username: u.Username,
	TenantID: u.TenantID,
	UserType: u.UserType,
	ExpiresAt: expTime,
}, h.Secret)

	if err != nil {
		http.Error(w, "failed to generate token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(LoginResponse{Token: token})
}
