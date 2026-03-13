package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"callcentrix/internal/auth"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CompaniesHandler struct {
	DB *pgxpool.Pool
}

type Company struct {
	ID                     int     `json:"id"`
	TenantID               int     `json:"tenantId"`
	Name                   string  `json:"name"`
	BusinessProfile        *string `json:"businessProfile"`
	Representatives        *string `json:"representatives"`
	TaxID                  *string `json:"taxId"`
	RepresentativesContact *string `json:"representativesContact"`
	Website                *string `json:"website"`
	CompanyContact         *string `json:"companyContact"`
	Location               *string `json:"location"`
	MaxUsers               *int    `json:"maxUsers"`
	Status                 bool    `json:"status"`
	UserCount              int     `json:"userCount"`
	TariffID               *int    `json:"tariffId"`
	TariffName             *string `json:"tariffName"`
	TariffMaxOps           *int    `json:"tariffMaxOperators"`
	TariffFee              *float64 `json:"tariffFee"`
}

type Tariff struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	MaxOperators int     `json:"maxOperators"`
	MonthlyFee   float64 `json:"monthlyFee"`
}

type UnassignedUser struct {
	ID        int     `json:"id"`
	Username  string  `json:"username"`
	FirstName *string `json:"firstName"`
	LastName  *string `json:"lastName"`
	Phone     *string `json:"phone"`
	Status    string  `json:"status"`
}

type AssignUserRequest struct {
	UserID   int `json:"userId"`
	TenantID int `json:"tenantId"`
}

type CreateCompanyRequest struct {
	Name                   string  `json:"name"`
	BusinessProfile        *string `json:"businessProfile"`
	Representatives        *string `json:"representatives"`
	TaxID                  *string `json:"taxId"`
	RepresentativesContact *string `json:"representativesContact"`
	Website                *string `json:"website"`
	CompanyContact         *string `json:"companyContact"`
	Location               *string `json:"location"`
	MaxUsers               int     `json:"maxUsers"`
	TariffID               *int    `json:"tariffId"`
}

// =========================
// GET COMPANIES
// =========================
func (h *CompaniesHandler) GetCompanies(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	if user.UserType != 0 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	rows, err := h.DB.Query(r.Context(), `
		SELECT
			t.id, t.tenant_id, t.name,
			t.business_profile, t.representatives, t.tax_id,
			t.representatives_contact, t.website, t.company_contact,
			t.location, t.max_users, t.status,
			COUNT(u.id) as user_count,
			t.tariff_id, tr.name, tr.max_operators, tr.monthly_fee
		FROM crm_tenants t
		LEFT JOIN users u ON u.tenant_id = t.tenant_id AND u.status = 'enable'
		LEFT JOIN crm_tarrifs tr ON tr.id = t.tariff_id
		GROUP BY t.id, t.tenant_id, t.name,
			t.business_profile, t.representatives, t.tax_id,
			t.representatives_contact, t.website, t.company_contact,
			t.location, t.max_users, t.status,
			t.tariff_id, tr.name, tr.max_operators, tr.monthly_fee
		ORDER BY t.name
	`)
	if err != nil {
		log.Printf("❌ GetCompanies: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	list := make([]Company, 0)
	for rows.Next() {
		var c Company
		if err := rows.Scan(
			&c.ID, &c.TenantID, &c.Name,
			&c.BusinessProfile, &c.Representatives, &c.TaxID,
			&c.RepresentativesContact, &c.Website, &c.CompanyContact,
			&c.Location, &c.MaxUsers, &c.Status,
			&c.UserCount,
			&c.TariffID, &c.TariffName, &c.TariffMaxOps, &c.TariffFee,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		list = append(list, c)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// =========================
// CREATE COMPANY
// =========================
func (h *CompaniesHandler) CreateCompany(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	if user.UserType != 0 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req CreateCompanyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	// Генерируем новый tenant_id: берём максимальный + 1
	var maxTenantID int
	err := h.DB.QueryRow(r.Context(), `SELECT COALESCE(MAX(tenant_id), 110000) FROM crm_tenants`).Scan(&maxTenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	newTenantID := maxTenantID + 1

	if req.MaxUsers == 0 {
		req.MaxUsers = 10
	}

	// Начинаем транзакцию
	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	// 1. Создаём компанию в crm_tenants
	var companyID int
	err = tx.QueryRow(r.Context(), `
		INSERT INTO crm_tenants (tenant_id, name, business_profile, representatives, tax_id,
			representatives_contact, website, company_contact, location, max_users, status, create_date, tariff_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, false, NOW(), $11)
		RETURNING id
	`,
		newTenantID, req.Name, req.BusinessProfile, req.Representatives, req.TaxID,
		req.RepresentativesContact, req.Website, req.CompanyContact, req.Location, req.MaxUsers,
		req.TariffID,
	).Scan(&companyID)
	if err != nil {
		log.Printf("❌ CreateCompany insert: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 2. Создаём очередь в ast_queues
	queueName := fmt.Sprintf("%d", newTenantID)
	_, err = tx.Exec(r.Context(), `
		INSERT INTO ast_queues (name, tenant_id, timeout, strategy)
		VALUES ($1, $2, 15, 'ringall')
		ON CONFLICT (name) DO NOTHING
	`, queueName, newTenantID)
	if err != nil {
		log.Printf("❌ CreateCompany queue: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Логируем создание компании
	changesJSON, _ := json.Marshal(map[string]interface{}{
		"name":      req.Name,
		"tenant_id": newTenantID,
		"tariff_id": req.TariffID,
		"max_users": req.MaxUsers,
	})
	h.DB.Exec(r.Context(), `
		INSERT INTO crm_log_company (company_id, tenant_id, action, changed_by, changes, created_at)
		VALUES ($1, $2, 'create', $3, $4, NOW())
	`, companyID, newTenantID, user.UserID, string(changesJSON))

	log.Printf("✅ Company created: id=%d tenantId=%d name=%s queue=%s", companyID, newTenantID, req.Name, queueName)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":       companyID,
		"tenantId": newTenantID,
		"name":     req.Name,
	})
}

// =========================
// GET UNASSIGNED USERS
// =========================
func (h *CompaniesHandler) GetUnassignedUsers(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	if user.UserType != 0 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	rows, err := h.DB.Query(r.Context(), `
		SELECT u.id, u.username, u.first_name, u.last_name, p.phone, u.status
		FROM users u
		LEFT JOIN user_profiles p ON p.user_id = u.id
		WHERE (u.tenant_id IS NULL OR u.tenant_id = 0) AND u.type = 3 AND u.status = 'enable'
		ORDER BY COALESCE(NULLIF(TRIM(COALESCE(u.first_name,'') || ' ' || COALESCE(u.last_name,'')), ''), u.username)
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	list := make([]UnassignedUser, 0)
	for rows.Next() {
		var u UnassignedUser
		if err := rows.Scan(&u.ID, &u.Username, &u.FirstName, &u.LastName, &u.Phone, &u.Status); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		list = append(list, u)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// =========================
// GET COMPANY USERS
// =========================
func (h *CompaniesHandler) GetCompanyUsers(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	if user.UserType != 0 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	tenantID, err := strconv.Atoi(chi.URLParam(r, "tenantId"))
	if err != nil {
		http.Error(w, "invalid tenantId", http.StatusBadRequest)
		return
	}

	rows, err := h.DB.Query(r.Context(), `
		SELECT u.id, u.username, u.first_name, u.last_name, p.phone, u.status
		FROM users u
		LEFT JOIN user_profiles p ON p.user_id = u.id
		WHERE u.tenant_id = $1 AND u.status = 'enable'
		ORDER BY COALESCE(NULLIF(TRIM(COALESCE(u.first_name,'') || ' ' || COALESCE(u.last_name,'')), ''), u.username)
	`, tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	list := make([]UnassignedUser, 0)
	for rows.Next() {
		var u UnassignedUser
		if err := rows.Scan(&u.ID, &u.Username, &u.FirstName, &u.LastName, &u.Phone, &u.Status); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		list = append(list, u)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// =========================
// ASSIGN USER TO COMPANY
// =========================
func (h *CompaniesHandler) AssignUser(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	if user.UserType != 0 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req AssignUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.UserID == 0 || req.TenantID == 0 {
		http.Error(w, "userId and tenantId are required", http.StatusBadRequest)
		return
	}

	// Проверяем компанию
	var companyExists bool
	h.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM crm_tenants WHERE tenant_id = $1)`, req.TenantID).Scan(&companyExists)
	if !companyExists {
		http.Error(w, "company not found", http.StatusNotFound)
		return
	}

	// Получаем username
	var username string
	err := h.DB.QueryRow(r.Context(), `SELECT username FROM users WHERE id = $1`, req.UserID).Scan(&username)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	// Проверяем лимит операторов по тарифу
	var currentCount int
	var tariffMaxOps *int
	h.DB.QueryRow(r.Context(), `
		SELECT COUNT(u.id), tr.max_operators
		FROM users u
		JOIN crm_tenants t ON t.tenant_id = $1
		LEFT JOIN crm_tarrifs tr ON tr.id = t.tariff_id
		WHERE u.tenant_id = $1
		GROUP BY tr.max_operators
	`, req.TenantID).Scan(&currentCount, &tariffMaxOps)

	if tariffMaxOps != nil && currentCount >= *tariffMaxOps {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":    "tariff_limit_exceeded",
			"message":  "Превышен лимит операторов по тарифу",
			"current":  currentCount,
			"maxAllowed": *tariffMaxOps,
		})
		return
	}

	// Транзакция
	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	// 1. Обновляем users: только tenant_id
	_, err = tx.Exec(r.Context(), `
		UPDATE users SET tenant_id = $1 WHERE id = $2
	`, req.TenantID, req.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 2. Добавляем в ast_queue_members
	queueName := fmt.Sprintf("%d", req.TenantID)
	iface := fmt.Sprintf("PJSIP/%s", username)
	_, err = tx.Exec(r.Context(), `
		INSERT INTO ast_queue_members (queue_name, interface, membername, state_interface, penalty, paused, wrapuptime, tenant_id)
		VALUES ($1, $2, $3, $4, 0, 0, 0, $5)
		ON CONFLICT DO NOTHING
	`, queueName, iface, username, iface, req.TenantID)
	if err != nil {
		log.Printf("❌ AssignUser queue_member: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Обновляем tenant_id в SIP таблицах
	authID := fmt.Sprintf("auth-%s", username)
	h.DB.Exec(r.Context(), `UPDATE ast_ps_endpoints SET tenant_id = $1 WHERE id = $2`, req.TenantID, username)
	h.DB.Exec(r.Context(), `UPDATE ast_ps_auths SET tenant_id = $1 WHERE id = $2`, req.TenantID, authID)
	h.DB.Exec(r.Context(), `UPDATE ast_ps_aors SET tenant_id = $1 WHERE id = $2`, req.TenantID, username)

	// Получаем company_id для лога
	var companyIDForLog int
	h.DB.QueryRow(r.Context(), `SELECT id FROM crm_tenants WHERE tenant_id = $1`, req.TenantID).Scan(&companyIDForLog)

	assignChanges, _ := json.Marshal(map[string]interface{}{
		"user_id":  req.UserID,
		"username": username,
	})
	h.DB.Exec(r.Context(), `
		INSERT INTO crm_log_company (company_id, tenant_id, action, changed_by, changes, created_at)
		VALUES ($1, $2, 'assign_user', $3, $4, NOW())
	`, companyIDForLog, req.TenantID, user.UserID, string(assignChanges))

	log.Printf("✅ User %s (%d) assigned to tenant %d", username, req.UserID, req.TenantID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "assigned successfully"})
}

// =========================
// UNASSIGN USER FROM COMPANY
// =========================
func (h *CompaniesHandler) UnassignUser(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	if user.UserType != 0 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req struct {
		UserID int `json:"userId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Получаем username и текущий tenant_id
	var username string
	var tenantID int
	err := h.DB.QueryRow(r.Context(),
		`SELECT username, tenant_id FROM users WHERE id = $1`, req.UserID,
	).Scan(&username, &tenantID)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	// 1. Очищаем tenant_id пользователя
	_, err = tx.Exec(r.Context(), `UPDATE users SET tenant_id = NULL WHERE id = $1`, req.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 2. Удаляем из ast_queue_members
	queueName := fmt.Sprintf("%d", tenantID)
	iface := fmt.Sprintf("PJSIP/%s", username)
	_, err = tx.Exec(r.Context(), `
		DELETE FROM ast_queue_members WHERE queue_name = $1 AND interface = $2
	`, queueName, iface)
	if err != nil {
		log.Printf("❌ UnassignUser queue_member: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Сбрасываем tenant_id в SIP таблицах
	authIDUnassign := fmt.Sprintf("auth-%s", username)
	h.DB.Exec(r.Context(), `UPDATE ast_ps_endpoints SET tenant_id = 0 WHERE id = $1`, username)
	h.DB.Exec(r.Context(), `UPDATE ast_ps_auths SET tenant_id = 0 WHERE id = $1`, authIDUnassign)
	h.DB.Exec(r.Context(), `UPDATE ast_ps_aors SET tenant_id = 0 WHERE id = $1`, username)

	// Получаем company_id для лога
	var companyIDUnassign int
	h.DB.QueryRow(r.Context(), `SELECT id FROM crm_tenants WHERE tenant_id = $1`, tenantID).Scan(&companyIDUnassign)

	unassignChanges, _ := json.Marshal(map[string]interface{}{
		"user_id":  req.UserID,
		"username": username,
	})
	h.DB.Exec(r.Context(), `
		INSERT INTO crm_log_company (company_id, tenant_id, action, changed_by, changes, created_at)
		VALUES ($1, $2, 'unassign_user', $3, $4, NOW())
	`, companyIDUnassign, tenantID, user.UserID, string(unassignChanges))

	log.Printf("✅ User %s unassigned from tenant %d", username, tenantID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "unassigned successfully"})
}



// =========================
// GET PENDING USERS
// =========================
func (h *CompaniesHandler) GetPendingUsers(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	if user.UserType != 0 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	rows, err := h.DB.Query(r.Context(), `
		SELECT u.id, u.username, u.first_name, u.last_name, p.phone, u.status
		FROM users u
		LEFT JOIN user_profiles p ON p.user_id = u.id
		WHERE u.status = 'disable' AND u.tenant_id IS NULL AND u.type = 3
		ORDER BY COALESCE(NULLIF(TRIM(COALESCE(u.first_name,'') || ' ' || COALESCE(u.last_name,'')), ''), u.username)
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	list := make([]UnassignedUser, 0)
	for rows.Next() {
		var u UnassignedUser
		if err := rows.Scan(&u.ID, &u.Username, &u.FirstName, &u.LastName, &u.Phone, &u.Status); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		list = append(list, u)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// =========================
// ACTIVATE USER
// =========================
func (h *CompaniesHandler) ActivateUser(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	if user.UserType != 0 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	userID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	// Получаем username пользователя
	var username string
	err = h.DB.QueryRow(r.Context(),
		`SELECT username FROM users WHERE id = $1 AND status = 'disable' AND tenant_id IS NULL AND type = 3`,
		userID,
	).Scan(&username)
	if err != nil {
		http.Error(w, "user not found or already active", http.StatusNotFound)
		return
	}

	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	// 1. Активируем пользователя
	_, err = tx.Exec(r.Context(),
		`UPDATE users SET status = 'enable' WHERE id = $1`, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 2. ast_ps_auths
	authID := fmt.Sprintf("auth-%s", username)
	_, err = tx.Exec(r.Context(), `
		INSERT INTO ast_ps_auths (id, auth_type, username, password, tenant_id)
		VALUES ($1, 'userpass', $2, $2, 0)
		ON CONFLICT (id) DO NOTHING
	`, authID, username)
	if err != nil {
		log.Printf("❌ ActivateUser ps_auths: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 3. ast_ps_aors
	_, err = tx.Exec(r.Context(), `
		INSERT INTO ast_ps_aors (id, max_contacts, remove_existing, qualify_frequency, tenant_id)
		VALUES ($1, 3, 'true', 60, 0)
		ON CONFLICT (id) DO NOTHING
	`, username)
	if err != nil {
		log.Printf("❌ ActivateUser ps_aors: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 4. ast_ps_endpoints
	_, err = tx.Exec(r.Context(), `
		INSERT INTO ast_ps_endpoints (
			id, transport, aors, auth, context,
			disallow, allow,
			webrtc, dtmf_mode,
			rtp_symmetric, rewrite_contact, force_rport, ice_support,
			tenant_id
		) VALUES (
			$1, 'transport-wss', $1, $2, $1,
			'all', 'ulaw,alaw',
			'yes', 'rfc4733',
			'yes', 'yes', 'yes', 'yes',
			0
		)
		ON CONFLICT (id) DO NOTHING
	`, username, authID)
	if err != nil {
		log.Printf("❌ ActivateUser ps_endpoints: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("✅ User %d (%s) activated by admin %d", userID, username, user.UserID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "activated"})
}

// =========================
// REJECT USER
// =========================
func (h *CompaniesHandler) RejectUser(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	if user.UserType != 0 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	userID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	tag, err := h.DB.Exec(r.Context(), `
		DELETE FROM users WHERE id = $1 AND status = 'disable' AND tenant_id IS NULL AND type = 3
	`, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tag.RowsAffected() == 0 {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	log.Printf("🗑️ User %d rejected by admin %d", userID, user.UserID)
	w.WriteHeader(http.StatusNoContent)
}

// =========================
// UPDATE COMPANY
// =========================
func (h *CompaniesHandler) UpdateCompany(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	if user.UserType != 0 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	tenantID, err := strconv.Atoi(chi.URLParam(r, "tenantId"))
	if err != nil {
		http.Error(w, "invalid tenantId", http.StatusBadRequest)
		return
	}

	var req struct {
		Name                   string  `json:"name"`
		BusinessProfile        *string `json:"businessProfile"`
		Representatives        *string `json:"representatives"`
		TaxID                  *string `json:"taxId"`
		RepresentativesContact *string `json:"representativesContact"`
		Website                *string `json:"website"`
		CompanyContact         *string `json:"companyContact"`
		Location               *string `json:"location"`
		MaxUsers               int     `json:"maxUsers"`
		TariffID               *int    `json:"tariffId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	// Получаем id компании
	var companyID int
	err = h.DB.QueryRow(r.Context(), `SELECT id FROM crm_tenants WHERE tenant_id = $1`, tenantID).Scan(&companyID)
	if err != nil {
		http.Error(w, "company not found", http.StatusNotFound)
		return
	}

	_, err = h.DB.Exec(r.Context(), `
		UPDATE crm_tenants SET
			name = $1, business_profile = $2, representatives = $3, tax_id = $4,
			representatives_contact = $5, website = $6, company_contact = $7,
			location = $8, max_users = $9, tariff_id = $10
		WHERE tenant_id = $11
	`, req.Name, req.BusinessProfile, req.Representatives, req.TaxID,
		req.RepresentativesContact, req.Website, req.CompanyContact,
		req.Location, req.MaxUsers, req.TariffID, tenantID)
	if err != nil {
		log.Printf("❌ UpdateCompany: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Логируем
	changes, _ := json.Marshal(map[string]interface{}{
		"name": req.Name, "tariff_id": req.TariffID, "max_users": req.MaxUsers,
	})
	h.DB.Exec(r.Context(), `
		INSERT INTO crm_log_company (company_id, tenant_id, action, changed_by, changes, created_at)
		VALUES ($1, $2, 'update', $3, $4, NOW())
	`, companyID, tenantID, user.UserID, string(changes))

	log.Printf("✅ Company %d updated by user %d", tenantID, user.UserID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "updated"})
}

// =========================
// TOGGLE COMPANY STATUS
// =========================
func (h *CompaniesHandler) ToggleStatus(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	if user.UserType != 0 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	tenantID, err := strconv.Atoi(chi.URLParam(r, "tenantId"))
	if err != nil {
		http.Error(w, "invalid tenantId", http.StatusBadRequest)
		return
	}

	var companyID int
	var currentStatus bool
	err = h.DB.QueryRow(r.Context(),
		`SELECT id, status FROM crm_tenants WHERE tenant_id = $1`, tenantID,
	).Scan(&companyID, &currentStatus)
	if err != nil {
		http.Error(w, "company not found", http.StatusNotFound)
		return
	}

	newStatus := !currentStatus
	_, err = h.DB.Exec(r.Context(),
		`UPDATE crm_tenants SET status = $1 WHERE tenant_id = $2`, newStatus, tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	action := "activate"
	if !newStatus {
		action = "deactivate"
	}

	changes, _ := json.Marshal(map[string]interface{}{
		"status_before": currentStatus,
		"status_after":  newStatus,
	})
	h.DB.Exec(r.Context(), `
		INSERT INTO crm_log_company (company_id, tenant_id, action, changed_by, changes, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`, companyID, tenantID, action, user.UserID, string(changes))

	log.Printf("✅ Company %d %sd by user %d", tenantID, action, user.UserID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": newStatus,
	})
}

// =========================
// GET TARIFFS
// =========================
func (h *CompaniesHandler) GetTariffs(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	if user.UserType != 0 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	rows, err := h.DB.Query(r.Context(), `
		SELECT id, name, max_operators, monthly_fee FROM crm_tarrifs ORDER BY monthly_fee
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	list := make([]Tariff, 0)
	for rows.Next() {
		var t Tariff
		if err := rows.Scan(&t.ID, &t.Name, &t.MaxOperators, &t.MonthlyFee); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		list = append(list, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}