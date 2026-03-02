package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"callcentrix/internal/auth"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CRMCatalogHandler struct {
	DB *pgxpool.Pool
}

// =========================
// MODELS
// =========================

type CatalogRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Active      *bool  `json:"active"`
}

type CatalogResponse struct {
	ID          int       `json:"id"`
	TenantID    int       `json:"tenantId"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"createdAt"`
}

type CategoryRequest struct {
	CatalogID int    `json:"catalogId"`
	Name      string `json:"name"`
	SortOrder int    `json:"sortOrder"`
	Active    *bool  `json:"active"`
}

type CategoryResponse struct {
	ID        int       `json:"id"`
	CatalogID int       `json:"catalogId"`
	TenantID  int       `json:"tenantId"`
	Name      string    `json:"name"`
	SortOrder int       `json:"sortOrder"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"createdAt"`
}

type AssignCatalogRequest struct {
	UserID    int `json:"userId"`
	CatalogID int `json:"catalogId"`
}

type UserCatalogResponse struct {
	UserID      int    `json:"userId"`
	Username    string `json:"username"`
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	CatalogID   *int   `json:"catalogId"`
	CatalogName string `json:"catalogName"`
}

type StatusRequest struct {
	Code      string  `json:"code"`
	SortOrder int     `json:"sortOrder"`
	IsDefault bool    `json:"isDefault"`
	IsClosed  bool    `json:"isClosed"`
	Color     *string `json:"color"`
}

type StatusResponse struct {
	ID        int     `json:"id"`
	TenantID  int     `json:"tenantId"`
	Code      string  `json:"code"`
	SortOrder int     `json:"sortOrder"`
	IsDefault bool    `json:"isDefault"`
	IsClosed  bool    `json:"isClosed"`
	Color     *string `json:"color"`
	CreatedAt time.Time `json:"createdAt"`
}

// ============================================================
// CATALOGS
// ============================================================

// GetCatalogs godoc
// @Summary      Список каталогов
// @Description  Возвращает все каталоги тенанта
// @Tags         CRM Catalogs
// @Security     BearerAuth
// @Produce      json
// @Success      200  {array}   CatalogResponse
// @Failure      401  {string}  string  "unauthorized"
// @Failure      500  {string}  string  "internal error"
// @Router       /api/crm/catalogs [get]
func (h *CRMCatalogHandler) GetCatalogs(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	rows, err := h.DB.Query(context.Background(),
		`SELECT id, tenant_id, name, COALESCE(description,''), active, created_at
		 FROM crm_catalogs WHERE tenant_id=$1 ORDER BY name`,
		user.TenantID)
	if err != nil {
		log.Printf("❌ GetCatalogs: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	result := make([]CatalogResponse, 0)
	for rows.Next() {
		var c CatalogResponse
		rows.Scan(&c.ID, &c.TenantID, &c.Name, &c.Description, &c.Active, &c.CreatedAt)
		result = append(result, c)
	}
	jsonResp(w, result)
}

// GetCatalog godoc
// @Summary      Получить каталог
// @Description  Возвращает каталог по ID вместе с его категориями
// @Tags         CRM Catalogs
// @Security     BearerAuth
// @Produce      json
// @Param        id  path  int  true  "ID каталога"
// @Success      200  {object}  map[string]any
// @Failure      401  {string}  string  "unauthorized"
// @Failure      404  {string}  string  "not found"
// @Router       /api/crm/catalogs/{id} [get]
func (h *CRMCatalogHandler) GetCatalog(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	id, err := chiID(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var c CatalogResponse
	err = h.DB.QueryRow(context.Background(),
		`SELECT id, tenant_id, name, COALESCE(description,''), active, created_at
		 FROM crm_catalogs WHERE id=$1 AND tenant_id=$2`,
		id, user.TenantID).
		Scan(&c.ID, &c.TenantID, &c.Name, &c.Description, &c.Active, &c.CreatedAt)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	rows, err := h.DB.Query(context.Background(),
		`SELECT id, catalog_id, tenant_id, name, sort_order, active, created_at
		 FROM crm_categories WHERE catalog_id=$1 ORDER BY sort_order, name`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	categories := make([]CategoryResponse, 0)
	for rows.Next() {
		var cat CategoryResponse
		rows.Scan(&cat.ID, &cat.CatalogID, &cat.TenantID, &cat.Name, &cat.SortOrder, &cat.Active, &cat.CreatedAt)
		categories = append(categories, cat)
	}
	jsonResp(w, map[string]any{"catalog": c, "categories": categories})
}

// CreateCatalog godoc
// @Summary      Создать каталог
// @Description  Создаёт новый каталог для тенанта
// @Tags         CRM Catalogs
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body  CatalogRequest  true  "Данные каталога"
// @Success      201  {object}  CatalogResponse
// @Failure      400  {string}  string  "invalid request"
// @Failure      401  {string}  string  "unauthorized"
// @Failure      409  {string}  string  "catalog with this name already exists"
// @Failure      500  {string}  string  "internal error"
// @Router       /api/crm/catalogs [post]
func (h *CRMCatalogHandler) CreateCatalog(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	var req CatalogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	var c CatalogResponse
	err := h.DB.QueryRow(context.Background(),
		`INSERT INTO crm_catalogs (tenant_id, name, description, active)
		 VALUES ($1,$2,$3,$4)
		 RETURNING id, tenant_id, name, COALESCE(description,''), active, created_at`,
		user.TenantID, req.Name, nullStr(req.Description), active).
		Scan(&c.ID, &c.TenantID, &c.Name, &c.Description, &c.Active, &c.CreatedAt)
	if err != nil {
		if isDuplicate(err) {
			http.Error(w, "catalog with this name already exists", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("✅ Catalog created: id=%d tenant=%d name=%s", c.ID, c.TenantID, c.Name)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(c)
}

// UpdateCatalog godoc
// @Summary      Обновить каталог
// @Description  Обновляет название, описание или статус каталога
// @Tags         CRM Catalogs
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id    path  int             true  "ID каталога"
// @Param        body  body  CatalogRequest  true  "Данные каталога"
// @Success      200  {object}  CatalogResponse
// @Failure      400  {string}  string  "invalid request"
// @Failure      401  {string}  string  "unauthorized"
// @Failure      404  {string}  string  "not found"
// @Failure      409  {string}  string  "catalog with this name already exists"
// @Router       /api/crm/catalogs/{id} [put]
func (h *CRMCatalogHandler) UpdateCatalog(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	id, err := chiID(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req CatalogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	var c CatalogResponse
	err = h.DB.QueryRow(context.Background(),
		`UPDATE crm_catalogs SET name=$1, description=$2, active=$3
		 WHERE id=$4 AND tenant_id=$5
		 RETURNING id, tenant_id, name, COALESCE(description,''), active, created_at`,
		req.Name, nullStr(req.Description), active, id, user.TenantID).
		Scan(&c.ID, &c.TenantID, &c.Name, &c.Description, &c.Active, &c.CreatedAt)
	if err != nil {
		if isDuplicate(err) {
			http.Error(w, "catalog with this name already exists", http.StatusConflict)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	jsonResp(w, c)
}

// DeleteCatalog godoc
// @Summary      Удалить каталог
// @Description  Удаляет каталог и все его категории (CASCADE)
// @Tags         CRM Catalogs
// @Security     BearerAuth
// @Param        id  path  int  true  "ID каталога"
// @Success      204  "No Content"
// @Failure      401  {string}  string  "unauthorized"
// @Failure      404  {string}  string  "not found"
// @Router       /api/crm/catalogs/{id} [delete]
func (h *CRMCatalogHandler) DeleteCatalog(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	id, err := chiID(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	tag, err := h.DB.Exec(context.Background(),
		`DELETE FROM crm_catalogs WHERE id=$1 AND tenant_id=$2`, id, user.TenantID)
	if err != nil || tag.RowsAffected() == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ============================================================
// CATEGORIES
// ============================================================

// GetCategories godoc
// @Summary      Список категорий каталога
// @Description  Возвращает все категории для указанного каталога
// @Tags         CRM Categories
// @Security     BearerAuth
// @Produce      json
// @Param        catalogId  query  int  true  "ID каталога"
// @Success      200  {array}   CategoryResponse
// @Failure      400  {string}  string  "catalogId required"
// @Failure      401  {string}  string  "unauthorized"
// @Failure      500  {string}  string  "internal error"
// @Router       /api/crm/categories [get]
func (h *CRMCatalogHandler) GetCategories(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	catalogID, err := strconv.Atoi(r.URL.Query().Get("catalogId"))
	if err != nil {
		http.Error(w, "catalogId is required", http.StatusBadRequest)
		return
	}
	rows, err := h.DB.Query(context.Background(),
		`SELECT c.id, c.catalog_id, c.tenant_id, c.name, c.sort_order, c.active, c.created_at
		 FROM crm_categories c
		 JOIN crm_catalogs cat ON cat.id=c.catalog_id
		 WHERE c.catalog_id=$1 AND c.tenant_id=$2
		 ORDER BY c.sort_order, c.name`,
		catalogID, user.TenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	result := make([]CategoryResponse, 0)
	for rows.Next() {
		var c CategoryResponse
		rows.Scan(&c.ID, &c.CatalogID, &c.TenantID, &c.Name, &c.SortOrder, &c.Active, &c.CreatedAt)
		result = append(result, c)
	}
	jsonResp(w, result)
}

// CreateCategory godoc
// @Summary      Создать категорию
// @Description  Создаёт новую категорию внутри каталога
// @Tags         CRM Categories
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body  CategoryRequest  true  "Данные категории"
// @Success      201  {object}  CategoryResponse
// @Failure      400  {string}  string  "invalid request"
// @Failure      401  {string}  string  "unauthorized"
// @Failure      403  {string}  string  "catalog does not belong to tenant"
// @Failure      409  {string}  string  "category with this name already exists"
// @Failure      500  {string}  string  "internal error"
// @Router       /api/crm/categories [post]
func (h *CRMCatalogHandler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	var req CategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.CatalogID == 0 {
		http.Error(w, "name and catalogId are required", http.StatusBadRequest)
		return
	}
	var exists bool
	h.DB.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM crm_catalogs WHERE id=$1 AND tenant_id=$2)`,
		req.CatalogID, user.TenantID).Scan(&exists)
	if !exists {
		http.Error(w, "catalog does not belong to tenant", http.StatusForbidden)
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	var c CategoryResponse
	err := h.DB.QueryRow(context.Background(),
		`INSERT INTO crm_categories (catalog_id, tenant_id, name, sort_order, active)
		 VALUES ($1,$2,$3,$4,$5)
		 RETURNING id, catalog_id, tenant_id, name, sort_order, active, created_at`,
		req.CatalogID, user.TenantID, req.Name, req.SortOrder, active).
		Scan(&c.ID, &c.CatalogID, &c.TenantID, &c.Name, &c.SortOrder, &c.Active, &c.CreatedAt)
	if err != nil {
		if isDuplicate(err) {
			http.Error(w, "category with this name already exists in this catalog", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("✅ Category created: id=%d catalog=%d name=%s", c.ID, c.CatalogID, c.Name)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(c)
}

// UpdateCategory godoc
// @Summary      Обновить категорию
// @Description  Обновляет название, порядок сортировки или статус категории
// @Tags         CRM Categories
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id    path  int              true  "ID категории"
// @Param        body  body  CategoryRequest  true  "Данные категории"
// @Success      200  {object}  CategoryResponse
// @Failure      400  {string}  string  "invalid request"
// @Failure      401  {string}  string  "unauthorized"
// @Failure      404  {string}  string  "not found"
// @Failure      409  {string}  string  "category with this name already exists"
// @Router       /api/crm/categories/{id} [put]
func (h *CRMCatalogHandler) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	id, err := chiID(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req CategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	var c CategoryResponse
	err = h.DB.QueryRow(context.Background(),
		`UPDATE crm_categories SET name=$1, sort_order=$2, active=$3
		 WHERE id=$4 AND tenant_id=$5
		 RETURNING id, catalog_id, tenant_id, name, sort_order, active, created_at`,
		req.Name, req.SortOrder, active, id, user.TenantID).
		Scan(&c.ID, &c.CatalogID, &c.TenantID, &c.Name, &c.SortOrder, &c.Active, &c.CreatedAt)
	if err != nil {
		if isDuplicate(err) {
			http.Error(w, "category with this name already exists in this catalog", http.StatusConflict)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	jsonResp(w, c)
}

// DeleteCategory godoc
// @Summary      Удалить категорию
// @Description  Удаляет категорию. Заявки с этой категорией переходят в category_id = NULL
// @Tags         CRM Categories
// @Security     BearerAuth
// @Param        id  path  int  true  "ID категории"
// @Success      204  "No Content"
// @Failure      401  {string}  string  "unauthorized"
// @Failure      404  {string}  string  "not found"
// @Router       /api/crm/categories/{id} [delete]
func (h *CRMCatalogHandler) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	id, err := chiID(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	tag, err := h.DB.Exec(context.Background(),
		`DELETE FROM crm_categories WHERE id=$1 AND tenant_id=$2`, id, user.TenantID)
	if err != nil || tag.RowsAffected() == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ============================================================
// USER → CATALOG ASSIGNMENTS
// ============================================================

// GetUserCatalogAssignments godoc
// @Summary      Список назначений каталогов
// @Description  Возвращает всех пользователей тенанта с информацией о назначенном каталоге
// @Tags         CRM Catalog Assignments
// @Security     BearerAuth
// @Produce      json
// @Success      200  {array}   UserCatalogResponse
// @Failure      401  {string}  string  "unauthorized"
// @Failure      500  {string}  string  "internal error"
// @Router       /api/crm/catalog-assignments [get]
func (h *CRMCatalogHandler) GetUserCatalogAssignments(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	rows, err := h.DB.Query(context.Background(),
		`SELECT u.id, u.username, COALESCE(u.first_name,''), COALESCE(u.last_name,''),
		        uc.catalog_id, COALESCE(c.name,'')
		 FROM users u
		 LEFT JOIN crm_user_catalog uc ON uc.user_id=u.id AND uc.tenant_id=u.tenant_id
		 LEFT JOIN crm_catalogs c ON c.id=uc.catalog_id
		 WHERE u.tenant_id=$1
		 ORDER BY u.first_name, u.last_name`,
		user.TenantID)
	if err != nil {
		log.Printf("❌ GetUserCatalogAssignments: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	result := make([]UserCatalogResponse, 0)
	for rows.Next() {
		var u UserCatalogResponse
		rows.Scan(&u.UserID, &u.Username, &u.FirstName, &u.LastName, &u.CatalogID, &u.CatalogName)
		result = append(result, u)
	}
	jsonResp(w, result)
}

// AssignCatalogToUser godoc
// @Summary      Назначить каталог пользователю
// @Description  Назначает каталог пользователю. Если уже был назначен — заменяет.
// @Tags         CRM Catalog Assignments
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body  AssignCatalogRequest  true  "userId и catalogId"
// @Success      200  {string}  string  "ok"
// @Failure      400  {string}  string  "invalid request"
// @Failure      401  {string}  string  "unauthorized"
// @Failure      403  {string}  string  "user or catalog does not belong to tenant"
// @Failure      500  {string}  string  "internal error"
// @Router       /api/crm/catalog-assignments [post]
func (h *CRMCatalogHandler) AssignCatalogToUser(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	var req AssignCatalogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == 0 || req.CatalogID == 0 {
		http.Error(w, "userId and catalogId are required", http.StatusBadRequest)
		return
	}
	var userOK, catalogOK bool
	h.DB.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND tenant_id=$2)`,
		req.UserID, user.TenantID).Scan(&userOK)
	h.DB.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM crm_catalogs WHERE id=$1 AND tenant_id=$2)`,
		req.CatalogID, user.TenantID).Scan(&catalogOK)
	if !userOK || !catalogOK {
		http.Error(w, "user or catalog does not belong to tenant", http.StatusForbidden)
		return
	}
	_, err := h.DB.Exec(context.Background(),
		`INSERT INTO crm_user_catalog (user_id, catalog_id, tenant_id)
		 VALUES ($1,$2,$3)
		 ON CONFLICT (user_id, tenant_id) DO UPDATE SET catalog_id=$2, assigned_at=NOW()`,
		req.UserID, req.CatalogID, user.TenantID)
	if err != nil {
		log.Printf("❌ AssignCatalogToUser: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("✅ Assigned catalog=%d to user=%d (tenant=%d)", req.CatalogID, req.UserID, user.TenantID)
	w.WriteHeader(http.StatusOK)
}

// UnassignCatalogFromUser godoc
// @Summary      Снять каталог с пользователя
// @Description  Удаляет назначение каталога у пользователя
// @Tags         CRM Catalog Assignments
// @Security     BearerAuth
// @Param        userId  path  int  true  "ID пользователя"
// @Success      204  "No Content"
// @Failure      401  {string}  string  "unauthorized"
// @Failure      404  {string}  string  "assignment not found"
// @Router       /api/crm/catalog-assignments/{userId} [delete]
func (h *CRMCatalogHandler) UnassignCatalogFromUser(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	userID, err := chiID(r, "userId")
	if err != nil {
		http.Error(w, "invalid userId", http.StatusBadRequest)
		return
	}
	tag, err := h.DB.Exec(context.Background(),
		`DELETE FROM crm_user_catalog WHERE user_id=$1 AND tenant_id=$2`,
		userID, user.TenantID)
	if err != nil || tag.RowsAffected() == 0 {
		http.Error(w, "assignment not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ============================================================
// STATUSES
// ============================================================

// GetStatusList godoc
// @Summary      Список статусов заявок
// @Description  Возвращает все статусы тенанта упорядоченные по sort_order
// @Tags         CRM Statuses
// @Security     BearerAuth
// @Produce      json
// @Success      200  {array}   StatusResponse
// @Failure      401  {string}  string  "unauthorized"
// @Router       /api/crm/statuses [get]
func (h *CRMCatalogHandler) GetStatusList(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	rows, err := h.DB.Query(context.Background(),
		`SELECT id, tenant_id, code, sort_order, is_default, is_closed, color, created_at
		 FROM crm_statuses WHERE tenant_id=$1 ORDER BY sort_order`,
		user.TenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	result := make([]StatusResponse, 0)
	for rows.Next() {
		var s StatusResponse
		rows.Scan(&s.ID, &s.TenantID, &s.Code, &s.SortOrder, &s.IsDefault, &s.IsClosed, &s.Color, &s.CreatedAt)
		result = append(result, s)
	}
	jsonResp(w, result)
}

// CreateStatus godoc
// @Summary      Создать статус
// @Description  Создаёт новый статус заявок для тенанта
// @Tags         CRM Statuses
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body  StatusRequest  true  "Данные статуса"
// @Success      201  {object}  StatusResponse
// @Failure      400  {string}  string  "code is required"
// @Failure      401  {string}  string  "unauthorized"
// @Failure      409  {string}  string  "status with this code already exists"
// @Failure      500  {string}  string  "internal error"
// @Router       /api/crm/statuses [post]
func (h *CRMCatalogHandler) CreateStatus(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	var req StatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		http.Error(w, "code is required", http.StatusBadRequest)
		return
	}
	var s StatusResponse
	err := h.DB.QueryRow(context.Background(),
		`INSERT INTO crm_statuses (tenant_id, code, sort_order, is_default, is_closed, color)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 RETURNING id, tenant_id, code, sort_order, is_default, is_closed, color, created_at`,
		user.TenantID, req.Code, req.SortOrder, req.IsDefault, req.IsClosed, req.Color).
		Scan(&s.ID, &s.TenantID, &s.Code, &s.SortOrder, &s.IsDefault, &s.IsClosed, &s.Color, &s.CreatedAt)
	if err != nil {
		if isDuplicate(err) {
			http.Error(w, "status with this code already exists", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("✅ Status created: id=%d tenant=%d code=%s", s.ID, s.TenantID, s.Code)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(s)
}

// UpdateStatus godoc
// @Summary      Обновить статус
// @Description  Обновляет данные статуса
// @Tags         CRM Statuses
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id    path  int            true  "ID статуса"
// @Param        body  body  StatusRequest  true  "Данные статуса"
// @Success      200  {object}  StatusResponse
// @Failure      400  {string}  string  "invalid request"
// @Failure      401  {string}  string  "unauthorized"
// @Failure      404  {string}  string  "not found"
// @Failure      409  {string}  string  "status with this code already exists"
// @Router       /api/crm/statuses/{id} [put]
func (h *CRMCatalogHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	id, err := chiID(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req StatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		http.Error(w, "code is required", http.StatusBadRequest)
		return
	}
	var s StatusResponse
	err = h.DB.QueryRow(context.Background(),
		`UPDATE crm_statuses SET code=$1, sort_order=$2, is_default=$3, is_closed=$4, color=$5
		 WHERE id=$6 AND tenant_id=$7
		 RETURNING id, tenant_id, code, sort_order, is_default, is_closed, color, created_at`,
		req.Code, req.SortOrder, req.IsDefault, req.IsClosed, req.Color, id, user.TenantID).
		Scan(&s.ID, &s.TenantID, &s.Code, &s.SortOrder, &s.IsDefault, &s.IsClosed, &s.Color, &s.CreatedAt)
	if err != nil {
		if isDuplicate(err) {
			http.Error(w, "status with this code already exists", http.StatusConflict)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	jsonResp(w, s)
}

// DeleteStatus godoc
// @Summary      Удалить статус
// @Description  Удаляет статус. Нельзя удалить если на него ссылаются заявки.
// @Tags         CRM Statuses
// @Security     BearerAuth
// @Param        id  path  int  true  "ID статуса"
// @Success      204  "No Content"
// @Failure      401  {string}  string  "unauthorized"
// @Failure      404  {string}  string  "not found"
// @Failure      409  {string}  string  "status is in use by tickets"
// @Router       /api/crm/statuses/{id} [delete]
func (h *CRMCatalogHandler) DeleteStatus(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	id, err := chiID(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var inUse bool
	h.DB.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM crm_tickets WHERE status_id=$1)`, id).Scan(&inUse)
	if inUse {
		http.Error(w, "status is in use by tickets", http.StatusConflict)
		return
	}
	tag, err := h.DB.Exec(context.Background(),
		`DELETE FROM crm_statuses WHERE id=$1 AND tenant_id=$2`, id, user.TenantID)
	if err != nil || tag.RowsAffected() == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ============================================================
// HELPERS
// ============================================================

func jsonResp(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func chiID(r *http.Request, param string) (int, error) {
	return strconv.Atoi(chi.URLParam(r, param))
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "unique constraint")
}