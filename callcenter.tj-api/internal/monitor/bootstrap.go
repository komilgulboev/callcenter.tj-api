package monitor

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

var db *pgxpool.Pool

func InitDB(pool *pgxpool.Pool) {
	db = pool
}

// PJSIP endpoints = агенты
func LoadAgentsByTenant(ctx context.Context, tenantID int) ([]string, error) {
	rows, err := db.Query(ctx, `
		SELECT id
		FROM ast_ps_endpoints
		WHERE tenant_id = $1
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			agents = append(agents, id)
		}
	}
	return agents, nil
}
