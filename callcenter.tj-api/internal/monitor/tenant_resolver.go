package monitor

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type TenantResolver struct {
	db    *pgxpool.Pool
	cache map[string]int
}

func NewTenantResolver(db *pgxpool.Pool) *TenantResolver {
	return &TenantResolver{
		db:    db,
		cache: make(map[string]int),
	}
}

func (r *TenantResolver) Resolve(event map[string]string) int {

	ext := extractExt(event["Channel"])
	if ext == "" {
		ext = extractExt(event["Device"])
	}

	if ext == "" {
		return 0
	}

	// 1️⃣ cache
	if t, ok := r.cache[ext]; ok {
		return t
	}

	// 2️⃣ db lookup
	var tenantID int
	err := r.db.QueryRow(
		context.Background(),
		`SELECT tenant_id FROM ast_ps_endpoints WHERE id = $1`,
		ext,
	).Scan(&tenantID)

	if err != nil {
		return 0
	}

	r.cache[ext] = tenantID
	return tenantID
}

func extractExt(v string) string {
	if v == "" {
		return ""
	}
	v = strings.TrimPrefix(v, "PJSIP/")
	if i := strings.Index(v, "-"); i > 0 {
		v = v[:i]
	}
	if _, err := strconv.Atoi(v); err == nil {
		return v
	}
	return ""
}
