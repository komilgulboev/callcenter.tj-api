package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"callcentrix/internal/auth"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RecordingHandler struct {
	DB              *pgxpool.Pool
	AsteriskBaseURL string // http://172.20.40.2:8088/recordings
	SignSecret      string
}

// =========================
// GET /api/recordings/{uniqueid}
// Проксирует файл с Asterisk сервера — только для своего тенанта
// =========================
func (h *RecordingHandler) Stream(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	uniqueid := chi.URLParam(r, "uniqueid")

	// Проверяем принадлежность записи тенанту
	if !h.checkAccess(r, uniqueid, user.TenantID) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Ищем файл на Asterisk сервере перебирая расширения
	for _, ext := range []string{".wav", ".mp3", ".gsm", ".ogg"} {
		fileURL := h.AsteriskBaseURL + "/" + uniqueid + ext
		resp, err := http.Get(fileURL)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		defer resp.Body.Close()

		// Передаём заголовки
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.Header().Set("Content-Length", resp.Header.Get("Content-Length"))
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s%s"`, uniqueid, ext))

		log.Printf("🎵 Recording proxied: %s%s → user=%d tenant=%d", uniqueid, ext, user.UserID, user.TenantID)
		io.Copy(w, resp.Body)
		return
	}

	http.Error(w, "recording not found", http.StatusNotFound)
}

// =========================
// GET /api/recordings/{uniqueid}/link
// Временная подписанная ссылка (15 мин)
// =========================
func (h *RecordingHandler) GetSignedLink(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	uniqueid := chi.URLParam(r, "uniqueid")

	if !h.checkAccess(r, uniqueid, user.TenantID) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	expires := time.Now().Add(15 * time.Minute).Unix()
	sig := sign(h.SignSecret, uniqueid, expires)

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"url":"/api/recordings/%s/play?expires=%d&sig=%s"}`,
		uniqueid, expires, sig)
}

// =========================
// GET /api/recordings/{uniqueid}/play?expires=...&sig=...
// Публичный роут для <audio src> — проверяет HMAC подпись
// =========================
func (h *RecordingHandler) PlaySigned(w http.ResponseWriter, r *http.Request) {
	uniqueid := chi.URLParam(r, "uniqueid")
	expiresStr := r.URL.Query().Get("expires")
	sig := r.URL.Query().Get("sig")

	expires, err := strconv.ParseInt(expiresStr, 10, 64)
	if err != nil || time.Now().Unix() > expires {
		http.Error(w, "link expired", http.StatusForbidden)
		return
	}

	expected := sign(h.SignSecret, uniqueid, expires)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}

	for _, ext := range []string{".wav", ".mp3", ".gsm", ".ogg"} {
		fileURL := h.AsteriskBaseURL + "/" + uniqueid + ext
		resp, err := http.Get(fileURL)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		defer resp.Body.Close()

		ct := resp.Header.Get("Content-Type")
		if ct == "" {
			ct = contentTypeByExt(ext)
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "no-store")
		io.Copy(w, resp.Body)
		return
	}

	http.Error(w, "file not found", http.StatusNotFound)
}

// =========================
// HELPERS
// =========================

func (h *RecordingHandler) checkAccess(r *http.Request, uniqueid string, tenantID int) bool {
	var count int
	err := h.DB.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM ast_cdr
		WHERE uniqueid = $1
		AND (
			EXISTS (SELECT 1 FROM users WHERE sipno::text = src AND tenant_id = $2)
			OR
			EXISTS (SELECT 1 FROM users WHERE sipno::text = dst AND tenant_id = $2)
		)`,
		uniqueid, tenantID,
	).Scan(&count)
	return err == nil && count > 0
}

func sign(secret, uniqueid string, expires int64) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(fmt.Sprintf("%s:%d", uniqueid, expires)))
	return hex.EncodeToString(h.Sum(nil))
}

func contentTypeByExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp3":
		return "audio/mpeg"
	case ".ogg":
		return "audio/ogg"
	case ".gsm":
		return "audio/x-gsm"
	default:
		return "audio/wav"
	}
}