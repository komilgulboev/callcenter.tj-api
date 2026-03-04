package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"callcentrix/internal/auth"

	"github.com/jackc/pgx/v5/pgxpool"
)

type CDRHandler struct {
	DB           *pgxpool.Pool
	RecordingURL string // публичный URL к записям, например http://host:8080/recordings
}

type CDRRecord struct {
	ID           int       `json:"id"`
	Uniqueid     string    `json:"uniqueid"`
	Src          string    `json:"src"`
	Dst          string    `json:"dst"`
	Channel      string    `json:"channel"`
	DstChannel   string    `json:"dstChannel"`
	LastApp      string    `json:"lastApp"`
	CallDate     time.Time `json:"callDate"`
	Duration     int       `json:"duration"`
	Billsec      int       `json:"billsec"`
	Disposition  string    `json:"disposition"`
	Clid         string    `json:"clid"`
	AgentName    *string   `json:"agentName"`
	RecordingURL *string   `json:"recordingUrl"`
}

type CDRStats struct {
	Total    int     `json:"total"`
	Answered int     `json:"answered"`
	Missed   int     `json:"missed"`
	AvgDur   float64 `json:"avgDuration"`
	TotalDur int     `json:"totalDuration"`
}

type CDRResponse struct {
	Records []CDRRecord `json:"records"`
	Stats   CDRStats    `json:"stats"`
	Total   int         `json:"total"`
	Page    int         `json:"page"`
	PerPage int         `json:"perPage"`
}

// GetCDR godoc
// @Summary      Отчёт звонков из ast_cdr
// @Tags         Reports
// @Security     BearerAuth
// @Produce      json
// @Router       /api/reports/calls [get]
func (h *CDRHandler) GetCDR(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	q := r.URL.Query()

	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(q.Get("perPage"))
	if perPage < 1 || perPage > 200 {
		perPage = 50
	}
	offset := (page - 1) * perPage

	// Фильтруем по tenant_id через JOIN users по sipno
	// Убираем дублирующие строки с lastapp=Hangup
	where := `
		WHERE c.lastapp != 'Hangup'
		AND (
			EXISTS (SELECT 1 FROM users WHERE sipno::text = c.src AND tenant_id = $1)
			OR
			EXISTS (SELECT 1 FROM users WHERE sipno::text = c.dst AND tenant_id = $1)
		)`

	args := []any{user.TenantID}
	idx := 2

	if v := q.Get("dateFrom"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			where += " AND c.calldate AT TIME ZONE 'UTC' >= $" + strconv.Itoa(idx)
			args = append(args, t.UTC())
			idx++
		}
	}
	if v := q.Get("dateTo"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			where += " AND c.calldate AT TIME ZONE 'UTC' <= $" + strconv.Itoa(idx)
			args = append(args, t.UTC())
			idx++
		}
	}
	if v := q.Get("src"); v != "" {
		where += " AND c.src ILIKE $" + strconv.Itoa(idx)
		args = append(args, "%"+v+"%")
		idx++
	}
	if v := q.Get("dst"); v != "" {
		where += " AND c.dst ILIKE $" + strconv.Itoa(idx)
		args = append(args, "%"+v+"%")
		idx++
	}
	if v := q.Get("disposition"); v != "" {
		where += " AND c.disposition = $" + strconv.Itoa(idx)
		args = append(args, v)
		idx++
	}

	// Статистика
	var stats CDRStats
	err := h.DB.QueryRow(r.Context(), `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE disposition = 'ANSWERED'),
			COUNT(*) FILTER (WHERE disposition != 'ANSWERED'),
			COALESCE(AVG(billsec) FILTER (WHERE disposition = 'ANSWERED'), 0),
			COALESCE(SUM(billsec) FILTER (WHERE disposition = 'ANSWERED'), 0)
		FROM ast_cdr c `+where, args...,
	).Scan(&stats.Total, &stats.Answered, &stats.Missed, &stats.AvgDur, &stats.TotalDur)
	if err != nil {
		log.Printf("❌ GetCDR stats: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Записи
	rows, err := h.DB.Query(r.Context(), `
		SELECT
			c.id,
			COALESCE(c.uniqueid, ''),
			COALESCE(c.src, ''),
			COALESCE(c.dst, ''),
			COALESCE(c.channel, ''),
			COALESCE(c.dstchannel, ''),
			COALESCE(c.lastapp, ''),
			c.calldate,
			c.duration,
			c.billsec,
			COALESCE(c.disposition, ''),
			COALESCE(c.clid, ''),
			(SELECT COALESCE(NULLIF(TRIM(COALESCE(first_name,'') || ' ' || COALESCE(last_name,'')), ''), username)
			 FROM users WHERE sipno::text = c.src AND tenant_id = $1 LIMIT 1),
			NULLIF(TRIM(c.userfield), '')
		FROM ast_cdr c
		`+where+`
		ORDER BY c.calldate DESC
		LIMIT $`+strconv.Itoa(idx)+` OFFSET $`+strconv.Itoa(idx+1),
		append(args, perPage, offset)...,
	)
	if err != nil {
		log.Printf("❌ GetCDR records: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	records := make([]CDRRecord, 0)
	for rows.Next() {
		var rec CDRRecord
		var userfield *string
		if err := rows.Scan(
			&rec.ID, &rec.Uniqueid,
			&rec.Src, &rec.Dst,
			&rec.Channel, &rec.DstChannel, &rec.LastApp,
			&rec.CallDate, &rec.Duration, &rec.Billsec,
			&rec.Disposition, &rec.Clid,
			&rec.AgentName,
			&userfield,
		); err != nil {
			log.Printf("❌ GetCDR scan: %v", err)
			continue
		}
		// Если userfield содержит имя файла записи
		if userfield != nil && *userfield != "" {
			url := h.RecordingURL + "/" + *userfield
			rec.RecordingURL = &url
		}
		records = append(records, rec)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(CDRResponse{
		Records: records,
		Stats:   stats,
		Total:   stats.Total,
		Page:    page,
		PerPage: perPage,
	})
}