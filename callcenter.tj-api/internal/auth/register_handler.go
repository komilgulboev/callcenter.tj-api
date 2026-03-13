package auth

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type RegisterRequest struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Address   string `json:"address"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

// Register godoc
// @Summary      Register new agent
// @Description  Register a new user with type=3 (Agent), status=disable (pending admin approval)
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        body body RegisterRequest true "Registration data"
// @Success      201 {string} string "created"
// @Failure      400 {string} string "invalid request"
// @Failure      409 {string} string "username already exists"
// @Router       /api/auth/register [post]
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	req.Username  = strings.TrimSpace(req.Username)
	req.FirstName = strings.TrimSpace(req.FirstName)
	req.LastName  = strings.TrimSpace(req.LastName)
	req.Phone     = strings.TrimSpace(req.Phone)
	req.Email     = strings.TrimSpace(req.Email)
	req.Address   = strings.TrimSpace(req.Address)

	if req.Username == "" || req.Password == "" || req.FirstName == "" || req.LastName == "" {
		http.Error(w, "firstName, lastName, username and password are required", http.StatusBadRequest)
		return
	}

	// Телефон — ровно 9 цифр
	if req.Phone != "" {
		if matched, _ := regexp.MatchString(`^\d{9}$`, req.Phone); !matched {
			http.Error(w, "phone must be exactly 9 digits", http.StatusBadRequest)
			return
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// tenant_id = NULL — пользователь ожидает активации и назначения в компанию
	_, err = h.DB.Exec(
		r.Context(),
		`INSERT INTO users (tenant_id, username, password_hash, first_name, last_name, contact, type, status, create_date)
		 VALUES (NULL, $1, $2, $3, $4, $5, 3, 'disable', NOW())`,
		req.Username,
		string(hash),
		req.FirstName,
		req.LastName,
		req.Phone,
	)
	if err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			http.Error(w, "username already exists", http.StatusConflict)
			return
		}
		http.Error(w, "failed to create user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"message": "registered successfully"})
}