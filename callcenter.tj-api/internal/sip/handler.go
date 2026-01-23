package sip

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"

	"callcentrix/internal/auth"
)

type Handler struct {
	DB DB
}

type DB interface {
	QueryRow(ctx context.Context, query string, args ...any) pgx.Row
}

type CredentialsResponse struct {
	SipUser     string `json:"sipUser"`
	SipPassword string `json:"sipPassword"`
	Domain      string `json:"domain"`
	WsUrl       string `json:"wsUrl"`
}

// GetCredentials godoc
// @Summary      Get SIP credentials
// @Description  Returns SIP credentials for authenticated user (webphone)
// @Tags         SIP
// @Security     BearerAuth
// @Produce      json
// @Success      200 {object} CredentialsResponse
// @Failure      401 {string} string "unauthorized"
// @Failure      404 {string} string "sip account not found"
// @Router       /api/sip/credentials [get]
func (h *Handler) GetCredentials(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())

	var resp CredentialsResponse

	err := h.DB.QueryRow(
		r.Context(),
		`
		SELECT
			b.sip_username,
			pa.password,
			s.sip_domain,
			s.ws_url
		FROM user_sip_bindings b
		JOIN ps_auths pa
		  ON pa.username = b.sip_username
		 AND pa.tenant_id = b.tenant_id
		JOIN asterisk_servers s
		  ON s.id = b.asterisk_server_id
		WHERE
			b.user_id = $1
			AND b.tenant_id = $2
			AND b.type = 1          -- webphone
			AND b.active = true
			AND s.enabled = true
		LIMIT 1
		`,
		user.UserID,
		user.TenantID,
	).Scan(
		&resp.SipUser,
		&resp.SipPassword,
		&resp.Domain,
		&resp.WsUrl,
	)

	if err != nil {
		http.Error(w, "sip account not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
