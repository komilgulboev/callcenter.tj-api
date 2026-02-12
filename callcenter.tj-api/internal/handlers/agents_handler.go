package handlers

import (
	"context"
	"encoding/json"
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
	Username  string `json:"username"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

// GetAgentsInfo возвращает информацию о всех агентах tenant'а
func (h *AgentsInfoHandler) GetAgentsInfo(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	tenantID := user.TenantID

	// Получаем список агентов из Store
	agents := h.Agents.GetAgents(tenantID)
	
	// Получаем имена агентов
	agentNames := make([]string, 0, len(agents))
	for name := range agents {
		agentNames = append(agentNames, name)
	}

	if len(agentNames) == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string][]AgentInfo{"agents": {}})
		return
	}

	// Запрашиваем информацию из БД
	query := `
		SELECT username, first_name, last_name
		FROM users
		WHERE username = ANY($1) AND tenant_id = $2
	`

	rows, err := h.DB.Query(context.Background(), query, agentNames, tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	agentsInfo := make([]AgentInfo, 0)
	for rows.Next() {
		var info AgentInfo
		if err := rows.Scan(&info.Username, &info.FirstName, &info.LastName); err != nil {
			continue
		}
		agentsInfo = append(agentsInfo, info)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string][]AgentInfo{"agents": agentsInfo})
}