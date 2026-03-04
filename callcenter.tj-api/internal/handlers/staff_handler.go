package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"callcentrix/internal/auth"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type StaffHandler struct {
	DB         *pgxpool.Pool
	UploadDir  string // например "./uploads"
	PublicBase string // например "http://localhost:8080"
}

// StaffMember — объединённые данные из users + user_profiles
type StaffMember struct {
	ID          int        `json:"id"`
	Username    string     `json:"username"`
	FirstName   string     `json:"firstName"`
	LastName    string     `json:"lastName"`
	SipNo       string     `json:"sipNo"`
	Status      string     `json:"status"`
	// Из user_profiles
	Email       *string    `json:"email"`
	Phone       *string    `json:"phone"`
	Address     *string    `json:"address"`
	Position    *string    `json:"position"`
	AvatarURL   *string    `json:"avatarUrl"`
	LastSeenAt  *time.Time `json:"lastSeenAt"`
}

type UpdateProfileRequest struct {
	FirstName string  `json:"firstName"`
	LastName  string  `json:"lastName"`
	Email     *string `json:"email"`
	Phone     *string `json:"phone"`
	Address   *string `json:"address"`
	Position  *string `json:"position"`
}

// =========================
// GET STAFF LIST
// =========================

// GetStaff godoc
// @Summary      Список сотрудников тенанта
// @Router       /api/staff [get]
func (h *StaffHandler) GetStaff(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())

	// Обновляем last_seen_at для текущего пользователя
	h.updateLastSeen(r.Context(), user.UserID, user.TenantID)

	rows, err := h.DB.Query(r.Context(), `
		SELECT
			u.id, u.username,
			COALESCE(u.first_name, ''), COALESCE(u.last_name, ''),
			COALESCE(u.sipno::text, ''), u.status,
			p.email, p.phone, p.address, p.position,
			p.avatar_url, p.last_seen_at
		FROM users u
		LEFT JOIN user_profiles p ON p.user_id = u.id
		WHERE u.tenant_id = $1
		ORDER BY COALESCE(NULLIF(TRIM(COALESCE(u.first_name,'') || ' ' || COALESCE(u.last_name,'')), ''), u.username)`,
		user.TenantID,
	)
	if err != nil {
		log.Printf("❌ GetStaff: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	list := make([]StaffMember, 0)
	for rows.Next() {
		var m StaffMember
		if err := rows.Scan(
			&m.ID, &m.Username, &m.FirstName, &m.LastName,
			&m.SipNo, &m.Status,
			&m.Email, &m.Phone, &m.Address, &m.Position,
			&m.AvatarURL, &m.LastSeenAt,
		); err != nil {
			log.Printf("❌ GetStaff scan error: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Добавляем базовый URL к относительному пути аватара
		if m.AvatarURL != nil && *m.AvatarURL != "" && !strings.HasPrefix(*m.AvatarURL, "http") {
			full := h.PublicBase + "/" + strings.TrimPrefix(*m.AvatarURL, "/")
			m.AvatarURL = &full
		}
		list = append(list, m)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// =========================
// UPDATE PROFILE
// =========================

// UpdateProfile godoc
// @Summary      Обновить профиль сотрудника
// @Router       /api/staff/{id}/profile [put]
func (h *StaffHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var req UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	// Обновляем first_name/last_name в таблице users
	_, err = h.DB.Exec(r.Context(),
		`UPDATE users SET first_name=$1, last_name=$2
		 WHERE id=$3 AND tenant_id=$4`,
		nullStr(req.FirstName), nullStr(req.LastName), id, user.TenantID,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Upsert user_profiles
	_, err = h.DB.Exec(r.Context(), `
		INSERT INTO user_profiles (user_id, tenant_id, email, phone, address, position, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			email      = EXCLUDED.email,
			phone      = EXCLUDED.phone,
			address    = EXCLUDED.address,
			position   = EXCLUDED.position,
			updated_at = NOW()`,
		id, user.TenantID,
		req.Email, req.Phone, req.Address, req.Position,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// =========================
// UPLOAD AVATAR
// =========================

// UploadAvatar godoc
// @Summary      Загрузить фото сотрудника
// @Router       /api/staff/{id}/avatar [post]
func (h *StaffHandler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	log.Printf("📷 UploadAvatar called, Authorization: %s", r.Header.Get("Authorization"))
	user := auth.FromContext(r.Context())
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	// Проверяем что сотрудник из того же тенанта
	var exists bool
	h.DB.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND tenant_id=$2)`,
		id, user.TenantID,
	).Scan(&exists)
	if !exists {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Максимум 5MB
	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		http.Error(w, "file too large (max 5MB)", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("avatar")
	if err != nil {
		http.Error(w, "avatar field required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Проверяем тип файла
	ext := strings.ToLower(filepath.Ext(header.Filename))
	allowed := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true}
	if !allowed[ext] {
		http.Error(w, "only jpg, png, webp allowed", http.StatusBadRequest)
		return
	}

	// Создаём директорию
	dir := filepath.Join(h.UploadDir, "avatars", strconv.Itoa(user.TenantID))
	if err := os.MkdirAll(dir, 0755); err != nil {
		http.Error(w, "failed to create upload dir", http.StatusInternalServerError)
		return
	}

	// Сохраняем файл: {uploadDir}/avatars/{tenantId}/{userId}{ext}
	filename := fmt.Sprintf("%d%s", id, ext)
	dst := filepath.Join(dir, filename)
	out, err := os.Create(dst)
	if err != nil {
		http.Error(w, "failed to save file", http.StatusInternalServerError)
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, file); err != nil {
		http.Error(w, "failed to write file", http.StatusInternalServerError)
		return
	}

	// Относительный URL для хранения в БД
	relPath := fmt.Sprintf("uploads/avatars/%d/%s", user.TenantID, filename)

	// Upsert avatar_url в user_profiles
	_, err = h.DB.Exec(r.Context(), `
		INSERT INTO user_profiles (user_id, tenant_id, avatar_url, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			avatar_url = EXCLUDED.avatar_url,
			updated_at = NOW()`,
		id, user.TenantID, relPath,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fullURL := h.PublicBase + "/" + relPath
	log.Printf("✅ Avatar uploaded: user=%d path=%s", id, relPath)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"avatarUrl": fullURL})
}

// =========================
// DELETE AVATAR
// =========================

// DeleteAvatar godoc
// @Summary      Удалить фото сотрудника
// @Router       /api/staff/{id}/avatar [delete]
func (h *StaffHandler) DeleteAvatar(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var relPath *string
	h.DB.QueryRow(r.Context(),
		`SELECT avatar_url FROM user_profiles WHERE user_id=$1 AND tenant_id=$2`,
		id, user.TenantID,
	).Scan(&relPath)

	if relPath != nil && *relPath != "" {
		os.Remove(filepath.Join(h.UploadDir, strings.TrimPrefix(*relPath, "uploads/")))
	}

	h.DB.Exec(r.Context(),
		`UPDATE user_profiles SET avatar_url=NULL, updated_at=NOW()
		 WHERE user_id=$1 AND tenant_id=$2`,
		id, user.TenantID,
	)

	w.WriteHeader(http.StatusNoContent)
}


// =========================
// DELETE STAFF
// =========================

// DeleteStaff godoc
// @Summary      Удалить сотрудника (только admin)
// @Router       /api/staff/{id} [delete]
func (h *StaffHandler) DeleteStaff(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	if user.UserType != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	// Нельзя удалить самого себя
	if id == user.UserID {
		http.Error(w, "cannot delete yourself", http.StatusBadRequest)
		return
	}
	tag, err := h.DB.Exec(r.Context(),
		`DELETE FROM users WHERE id=$1 AND tenant_id=$2`,
		id, user.TenantID,
	)
	if err != nil || tag.RowsAffected() == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	log.Printf("🗑️ Staff deleted: user=%d by admin=%d", id, user.UserID)
	w.WriteHeader(http.StatusNoContent)
}

// =========================
// HELPERS
// =========================

func (h *StaffHandler) updateLastSeen(ctx context.Context, userID, tenantID int) {
	h.DB.Exec(ctx, `
		INSERT INTO user_profiles (user_id, tenant_id, last_seen_at, updated_at)
		VALUES ($1, $2, NOW(), NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			last_seen_at = NOW(),
			updated_at   = NOW()`,
		userID, tenantID,
	)
}