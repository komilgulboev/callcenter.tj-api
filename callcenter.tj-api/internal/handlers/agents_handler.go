package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"callcentrix/internal/auth"
	"callcentrix/internal/monitor"

	"github.com/jackc/pgx/v5/pgxpool"
)

type AgentsInfoHandler struct {
	DB     *pgxpool.Pool
	Agents *monitor.Store
}

type AgentInfo struct {
	SIPNo     string `json:"sipno"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

// GetAgentsInfo возвращает информацию о всех агентах tenant'а
func (h *AgentsInfoHandler) GetAgentsInfo(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	tenantID := user.TenantID

	log.Printf("👤 GetAgentsInfo: tenantID=%d", tenantID)

	// Получаем список агентов из Store (это SIP номера)
	agents := h.Agents.GetAgents(tenantID)
	
	// Получаем SIP номера агентов
	sipNumbers := make([]string, 0, len(agents))
	for name := range agents {
		sipNumbers = append(sipNumbers, name)
	}

	log.Printf("👤 SIP numbers from Store: %v", sipNumbers)

	if len(sipNumbers) == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string][]AgentInfo{"agents": {}})
		return
	}

	// Запрашиваем информацию из БД по SIP номеру
	query := `
		SELECT sipno, first_name, last_name
		FROM users
		WHERE sipno = ANY($1) AND tenant_id = $2
	`

	log.Printf("👤 SQL query with sipno: %v, tenantID: %d", sipNumbers, tenantID)

	rows, err := h.DB.Query(context.Background(), query, sipNumbers, tenantID)
	if err != nil {
		log.Printf("❌ SQL error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	agentsInfo := make([]AgentInfo, 0)
	for rows.Next() {
		var info AgentInfo
		if err := rows.Scan(&info.SIPNo, &info.FirstName, &info.LastName); err != nil {
			log.Printf("⚠️ Scan error: %v", err)
			continue
		}
		
		log.Printf("✅ Found agent: sipno=%s, name=%s %s", 
			info.SIPNo, info.FirstName, info.LastName)
		agentsInfo = append(agentsInfo, info)
	}

	log.Printf("👤 Returning %d agents", len(agentsInfo))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string][]AgentInfo{"agents": agentsInfo})
}