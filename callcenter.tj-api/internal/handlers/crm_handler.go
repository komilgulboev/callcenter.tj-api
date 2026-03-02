package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"callcentrix/internal/auth"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CRMHandler struct {
	DB *pgxpool.Pool
}

// =========================
// MODELS
// =========================

type CreateTicketRequest struct {
	CallUniqueid string `json:"callUniqueid"`
	CallFrom     string `json:"callFrom"`
	Subject      string `json:"subject"`
	Description  string `json:"description"`
	CategoryID   *int   `json:"categoryId"`
}

type CreateTicketResponse struct {
	ID int `json:"id"`
}

type TicketItem struct {
	ID             int       `json:"id"`
	CallUniqueid   string    `json:"callUniqueid"`
	CallFrom       string    `json:"callFrom"`
	Subject        string    `json:"subject"`
	Description    *string   `json:"description"`
	CategoryID     *int      `json:"categoryId"`
	CategoryName   *string   `json:"categoryName"`
	StatusID       int       `json:"statusId"`
	StatusCode     string    `json:"statusCode"`
	StatusColor    *string   `json:"statusColor"`
	CreatedBy      int       `json:"createdBy"`
	CreatedByName  string    `json:"createdByName"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	AssignedTo     *int      `json:"assignedTo"`
	AssignedToName string    `json:"assignedToName"`
}

type TicketUpdate struct {
	ID             int       `json:"id"`
	CallUniqueid   *string   `json:"callUniqueid"`
	CallFrom       *string   `json:"callFrom"`
	Description    *string   `json:"description"`
	StatusID       *int      `json:"statusId"`
	StatusCode     *string   `json:"statusCode"`
	AssignedTo     *int      `json:"assignedTo"`
	AssignedToName *string   `json:"assignedToName"`
	CreatedBy      int       `json:"createdBy"`
	CreatedByName  string    `json:"createdByName"`
	CreatedAt      time.Time `json:"createdAt"`
}

type AddTicketUpdateRequest struct {
	CallUniqueid *string `json:"callUniqueid"`
	CallFrom     *string `json:"callFrom"`
	Description  *string `json:"description"`
	StatusID     *int    `json:"statusId"`
}

type ChangeStatusRequest struct {
	StatusID int `json:"statusId"`
}

// =========================
// CREATE TICKET
// =========================

func (h *CRMHandler) CreateTicket(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())

	var req CreateTicketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.CallUniqueid == "" || req.CallFrom == "" || req.Subject == "" {
		http.Error(w, "callUniqueid, callFrom and subject are required", http.StatusBadRequest)
		return
	}

	var statusID int
	err := h.DB.QueryRow(
		context.Background(),
		`SELECT id FROM crm_statuses WHERE tenant_id=$1 AND is_default=TRUE ORDER BY sort_order LIMIT 1`,
		user.TenantID,
	).Scan(&statusID)
	if err != nil {
		http.Error(w, "no default status found for tenant", http.StatusInternalServerError)
		return
	}

	var id int
	err = h.DB.QueryRow(
		context.Background(),
		`INSERT INTO crm_tickets (tenant_id, call_uniqueid, call_from, subject, description, category_id, status_id, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 RETURNING id`,
		user.TenantID, req.CallUniqueid, req.CallFrom, req.Subject,
		nullableString(req.Description), req.CategoryID, statusID, user.UserID,
	).Scan(&id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(CreateTicketResponse{ID: id})
}

// =========================
// GET TICKETS LIST
// =========================

func (h *CRMHandler) GetTickets(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())

	limit, offset := 20, 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	statusFilter := r.URL.Query().Get("statusId")
	search       := r.URL.Query().Get("search")
	dateFrom     := r.URL.Query().Get("dateFrom")
	dateTo       := r.URL.Query().Get("dateTo")
	assignedTo   := r.URL.Query().Get("assignedTo")

	where := "WHERE t.tenant_id = $1"
	args := []any{user.TenantID}
	idx := 2

	if statusFilter != "" {
		where += " AND t.status_id = $" + strconv.Itoa(idx)
		args = append(args, statusFilter)
		idx++
	}
	if search != "" {
		where += " AND (t.subject ILIKE $" + strconv.Itoa(idx) +
			" OR t.call_from ILIKE $" + strconv.Itoa(idx) +
			" OR t.description ILIKE $" + strconv.Itoa(idx) +
			" OR t.id::text = $" + strconv.Itoa(idx+1) + ")"
		args = append(args, "%"+search+"%", search)
		idx += 2
	}
	if dateFrom != "" {
		where += " AND t.created_at >= $" + strconv.Itoa(idx)
		args = append(args, dateFrom)
		idx++
	}
	if dateTo != "" {
		where += " AND t.created_at < ($" + strconv.Itoa(idx) + "::date + interval '1 day')"
		args = append(args, dateTo)
		idx++
	}
	if assignedTo != "" {
		where += " AND t.assigned_to = $" + strconv.Itoa(idx)
		args = append(args, assignedTo)
		idx++
	}

	var total int
	if err := h.DB.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM crm_tickets t `+where, args...,
	).Scan(&total); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	dataQuery := `
		SELECT
			t.id, t.call_uniqueid, t.call_from, t.subject, t.description,
			t.category_id, cat.name,
			t.status_id, s.code, s.color, s.is_closed,
			t.created_by, COALESCE(u.first_name || ' ' || u.last_name, u.username),
			t.created_at, t.updated_at,
			t.assigned_to,
			COALESCE(NULLIF(TRIM(COALESCE(ua.first_name,'') || ' ' || COALESCE(ua.last_name,'')), ''), ua.username, '')
		FROM crm_tickets t
		LEFT JOIN crm_categories cat ON cat.id = t.category_id
		JOIN  crm_statuses s         ON s.id   = t.status_id
		JOIN  users u                ON u.id   = t.created_by
		LEFT JOIN users ua           ON ua.id  = t.assigned_to
		` + where + `
		ORDER BY s.is_closed ASC, t.updated_at DESC
		LIMIT $` + strconv.Itoa(idx) + ` OFFSET $` + strconv.Itoa(idx+1)

	rows, err := h.DB.Query(context.Background(), dataQuery, append(args, limit, offset)...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	tickets := make([]TicketItem, 0)
	for rows.Next() {
		var t TicketItem
		var isClosed bool
		if err := rows.Scan(
			&t.ID, &t.CallUniqueid, &t.CallFrom, &t.Subject, &t.Description,
			&t.CategoryID, &t.CategoryName,
			&t.StatusID, &t.StatusCode, &t.StatusColor, &isClosed,
			&t.CreatedBy, &t.CreatedByName,
			&t.CreatedAt, &t.UpdatedAt,
			&t.AssignedTo, &t.AssignedToName,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		tickets = append(tickets, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"tickets": tickets,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}

// =========================
// GET TICKET BY ID
// =========================

func (h *CRMHandler) GetTicket(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	id, err := crmTicketID(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var t TicketItem
	err = h.DB.QueryRow(
		context.Background(),
		`SELECT
			t.id, t.call_uniqueid, t.call_from, t.subject, t.description,
			t.category_id, cat.name,
			t.status_id, s.code, s.color,
			t.created_by, COALESCE(u.first_name || ' ' || u.last_name, u.username),
			t.created_at, t.updated_at,
			t.assigned_to,
			COALESCE(NULLIF(TRIM(COALESCE(ua.first_name,'') || ' ' || COALESCE(ua.last_name,'')), ''), ua.username, '')
		 FROM crm_tickets t
		 LEFT JOIN crm_categories cat ON cat.id = t.category_id
		 JOIN  crm_statuses s         ON s.id   = t.status_id
		 JOIN  users u                ON u.id   = t.created_by
		 LEFT JOIN users ua           ON ua.id  = t.assigned_to
		 WHERE t.id=$1 AND t.tenant_id=$2`,
		id, user.TenantID,
	).Scan(
		&t.ID, &t.CallUniqueid, &t.CallFrom, &t.Subject, &t.Description,
		&t.CategoryID, &t.CategoryName,
		&t.StatusID, &t.StatusCode, &t.StatusColor,
		&t.CreatedBy, &t.CreatedByName,
		&t.CreatedAt, &t.UpdatedAt,
		&t.AssignedTo, &t.AssignedToName,
	)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	updates, err := h.getTicketUpdates(context.Background(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ticket": t, "updates": updates})
}

// =========================
// ADD UPDATE TO TICKET
// =========================

func (h *CRMHandler) AddTicketUpdate(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	ticketID, err := crmTicketID(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var exists bool
	h.DB.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM crm_tickets WHERE id=$1 AND tenant_id=$2)`,
		ticketID, user.TenantID,
	).Scan(&exists)
	if !exists {
		http.Error(w, "ticket not found", http.StatusNotFound)
		return
	}

	var req AddTicketUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	_, err = h.DB.Exec(context.Background(),
		`INSERT INTO crm_ticket_updates (ticket_id, tenant_id, call_uniqueid, call_from, description, status_id, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		ticketID, user.TenantID,
		req.CallUniqueid, req.CallFrom, req.Description, req.StatusID, user.UserID,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if req.StatusID != nil {
		h.DB.Exec(context.Background(),
			`UPDATE crm_tickets SET status_id=$1, updated_at=NOW() WHERE id=$2`,
			req.StatusID, ticketID,
		)
	}

	w.WriteHeader(http.StatusOK)
}

// =========================
// CHANGE STATUS
// =========================

func (h *CRMHandler) ChangeTicketStatus(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	ticketID, err := crmTicketID(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var req ChangeStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	tag, err := h.DB.Exec(context.Background(),
		`UPDATE crm_tickets SET status_id=$1, updated_at=NOW()
		 WHERE id=$2 AND tenant_id=$3`,
		req.StatusID, ticketID, user.TenantID,
	)
	if err != nil || tag.RowsAffected() == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// =========================
// ASSIGN TICKET
// =========================

func (h *CRMHandler) AssignTicket(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	id, err := crmTicketID(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var req struct {
		UserID *int `json:"userId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	_, err = h.DB.Exec(context.Background(),
		`UPDATE crm_tickets SET assigned_to=$1, updated_at=NOW()
		 WHERE id=$2 AND tenant_id=$3`,
		req.UserID, id, user.TenantID,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Записываем назначение в историю
	h.DB.Exec(context.Background(),
		`INSERT INTO crm_ticket_updates (ticket_id, tenant_id, assigned_to, created_by)
		 VALUES ($1, $2, $3, $4)`,
		id, user.TenantID, req.UserID, user.UserID,
	)

	w.WriteHeader(http.StatusOK)
}

// =========================
// AGENTS LIST
// =========================

func (h *CRMHandler) GetAgentsList(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())

	log.Printf("🔍 GetAgentsList: tenantID=%d", user.TenantID)

	rows, err := h.DB.Query(context.Background(),
		`SELECT id, username,
		        COALESCE(NULLIF(TRIM(COALESCE(first_name,'') || ' ' || COALESCE(last_name,'')), ''), username)
		 FROM users
		 WHERE tenant_id = $1 AND status = 'enable'
		 ORDER BY COALESCE(NULLIF(TRIM(COALESCE(first_name,'') || ' ' || COALESCE(last_name,'')), ''), username)`,
		user.TenantID,
	)
	if err != nil {
		log.Printf("❌ GetAgentsList error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Agent struct {
		ID       int    `json:"id"`
		Username string `json:"username"`
		Name     string `json:"name"`
	}
	list := make([]Agent, 0)
	for rows.Next() {
		var a Agent
		rows.Scan(&a.ID, &a.Username, &a.Name)
		list = append(list, a)
	}
	log.Printf("📋 GetAgentsList: returning %d agents for tenant %d", len(list), user.TenantID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// =========================
// MY CATALOG
// =========================

func (h *CRMHandler) GetMyCatalog(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())

	// Локальные типы чтобы не конфликтовать с crm_catalog_handler.go
	type myCatalogResult struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Active      bool   `json:"active"`
	}
	type myCategoryResult struct {
		ID        int    `json:"id"`
		CatalogID int    `json:"catalogId"`
		Name      string `json:"name"`
		SortOrder int    `json:"sortOrder"`
		Active    bool   `json:"active"`
	}

	var catalog myCatalogResult
	err := h.DB.QueryRow(context.Background(),
		`SELECT c.id, c.name, COALESCE(c.description,''), c.active
		 FROM crm_catalogs c
		 JOIN crm_user_catalog uc ON uc.catalog_id=c.id
		 WHERE uc.user_id=$1 AND uc.tenant_id=$2`,
		user.UserID, user.TenantID,
	).Scan(&catalog.ID, &catalog.Name, &catalog.Description, &catalog.Active)
	if err != nil {
		http.Error(w, "catalog not assigned", http.StatusNotFound)
		return
	}

	rows, err := h.DB.Query(context.Background(),
		`SELECT id, catalog_id, name, sort_order, active
		 FROM crm_categories
		 WHERE catalog_id=$1 AND active=TRUE
		 ORDER BY sort_order, name`,
		catalog.ID,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	categories := make([]myCategoryResult, 0)
	for rows.Next() {
		var c myCategoryResult
		rows.Scan(&c.ID, &c.CatalogID, &c.Name, &c.SortOrder, &c.Active)
		categories = append(categories, c)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"catalog": catalog, "categories": categories})
}

// =========================
// HELPERS
// =========================

func (h *CRMHandler) getTicketUpdates(ctx context.Context, ticketID int) ([]TicketUpdate, error) {
	rows, err := h.DB.Query(ctx,
		`SELECT
			u.id, u.call_uniqueid, u.call_from, u.description,
			u.status_id, s.code,
			u.assigned_to,
			COALESCE(NULLIF(TRIM(COALESCE(ua.first_name,'') || ' ' || COALESCE(ua.last_name,'')), ''), ua.username),
			u.created_by, COALESCE(usr.first_name || ' ' || usr.last_name, usr.username),
			u.created_at
		 FROM crm_ticket_updates u
		 LEFT JOIN crm_statuses s  ON s.id  = u.status_id
		 LEFT JOIN users ua        ON ua.id = u.assigned_to
		 JOIN  users usr           ON usr.id = u.created_by
		 WHERE u.ticket_id=$1
		 ORDER BY u.created_at ASC`,
		ticketID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	updates := make([]TicketUpdate, 0)
	for rows.Next() {
		var u TicketUpdate
		rows.Scan(
			&u.ID, &u.CallUniqueid, &u.CallFrom, &u.Description,
			&u.StatusID, &u.StatusCode,
			&u.AssignedTo, &u.AssignedToName,
			&u.CreatedBy, &u.CreatedByName,
			&u.CreatedAt,
		)
		updates = append(updates, u)
	}
	return updates, nil
}

func crmTicketID(r *http.Request) (int, error) {
	return strconv.Atoi(chi.URLParam(r, "id"))
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}