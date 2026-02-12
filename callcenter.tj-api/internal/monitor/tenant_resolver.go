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

	// Пробуем извлечь extension из разных полей
	candidates := []string{
		extractExt(event["Channel"]),
		extractExt(event["Device"]),
		extractExt(event["ConnectedLineNum"]), // ДОБАВЛЕНО: номер куда звонят
		extractExt(event["CallerIDNum"]),      // ДОБАВЛЕНО: номер откуда звонят
	}

	for _, ext := range candidates {
		if ext == "" {
			continue
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

		if err == nil {
			r.cache[ext] = tenantID
			return tenantID
		}
	}

	return 0
}

// ResolveByExtension находит tenantID по номеру extension
func (r *TenantResolver) ResolveByExtension(ext string) int {
	ext = extractExt(ext)
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