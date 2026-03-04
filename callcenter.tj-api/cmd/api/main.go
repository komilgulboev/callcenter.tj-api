package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	httpSwagger "github.com/swaggo/http-swagger"

	"callcentrix/internal/ami"
	"callcentrix/internal/auth"
	"callcentrix/internal/config"
	"callcentrix/internal/db"
	"callcentrix/internal/handlers"
	"callcentrix/internal/monitor"
	"callcentrix/internal/sip"
	"callcentrix/internal/ws"

	_ "callcentrix/docs"
)

// @title           CallCentrix API
// @version         1.0
// @description     API for call center monitoring and control
// @termsOfService  http://swagger.io/terms/

// @contact.name   API Support
// @contact.email  support@callcentrix.com

// @license.name  Apache 2.0
// @license.url   http://www.apache.org/licenses/LICENSE-2.0.html

// @host      localhost:8080
// @BasePath  /

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token

func main() {
	cfg := config.Load()
	log.Println("✅ Config loaded")

	pool, err := db.New(cfg.DB.DSN)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	// =========================
	// MONITOR STORES
	// =========================
	agentStore      := monitor.NewStore()
	callStore       := monitor.NewCallStore()
	queueStore      := monitor.NewQueueStore()
	tenantResolver  := monitor.NewTenantResolver(pool)

	// =========================
	// AMI
	// =========================
	amiHandler := &ami.Handler{
		Agents:   agentStore,
		Calls:    callStore,
		Queues:   queueStore,
		Resolver: tenantResolver,
	}

	amiService, err := ami.NewService(
		cfg.AMI.Addr,
		cfg.AMI.Username,
		cfg.AMI.Password,
		amiHandler.HandleEvent,
	)
	if err != nil {
		log.Fatal(err)
	}
	go amiService.Start()

	// =========================
	// HANDLERS
	// =========================
	actionsHandler := &ami.ActionsHandler{
		DB:     pool,
		AMI:    amiService,
		Calls:  callStore,
		Agents: agentStore,
	}

	agentsInfoHandler := &handlers.AgentsInfoHandler{
		DB:     pool,
		Agents: agentStore,
	}

	crmCatalogHandler := &handlers.CRMCatalogHandler{
		DB: pool,
	}

	crmHandler := &handlers.CRMHandler{
		DB: pool,
	}

	cdrHandler := &handlers.CDRHandler{
		DB:           pool,
	}

	recordingHandler := &handlers.RecordingHandler{
		DB:              pool,
		AsteriskBaseURL: cfg.Asterisk.RecordingURL,
		SignSecret:   cfg.JWT.Secret, // используем тот же секрет
	}

	staffHandler := &handlers.StaffHandler{
		DB:         pool,
		UploadDir:  "./uploads",
		PublicBase: cfg.HTTP.PublicBase,
	}


	// =========================
	// ROUTER
	// =========================
	r := chi.NewRouter()

	// CORS — должен быть первым
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{
			"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS",
		},
		AllowedHeaders: []string{
			"Accept",
			"Authorization",
			"Content-Type",
			"X-Requested-With",
			"multipart/form-data",
		},
		ExposedHeaders:   []string{"Authorization"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// =========================
	// PUBLIC ROUTES
	// =========================
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	authHandler := &auth.Handler{
		DB:     pool,
		Secret: cfg.JWT.Secret,
		TTL:    time.Minute * time.Duration(cfg.JWT.TTLMinutes),
	}
	sipHandler := &sip.Handler{DB: pool}

	r.Post("/api/auth/login", authHandler.Login)

	r.Get("/ws/monitor", ws.Monitor(
		agentStore,
		callStore,
		queueStore,
		cfg,
	))

	// =========================
	// PROTECTED ROUTES
	// =========================
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(cfg.JWT.Secret))

		// ── Общее ──────────────────────────────────────
		r.Get("/api/me", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(auth.FromContext(r.Context()))
		})

		// ── SIP ────────────────────────────────────────
		r.Get("/api/sip/credentials",  sipHandler.GetCredentials)
		// ── Агенты ─────────────────────────────────────
		r.Get("/api/agents/info", agentsInfoHandler.GetAgentsInfo)

		// ── Действия ───────────────────────────────────
		r.Post("/api/actions/pause",  actionsHandler.TogglePause)
		r.Post("/api/actions/hangup", actionsHandler.Hangup)
		r.Get("/api/actions/my-call",   actionsHandler.GetMyActiveCall)

		// ── Отчёты ───────────────────────────────────────────
		r.Get("/api/reports/calls", cdrHandler.GetCDR)

		// ── Записи звонков (защищённые) ───────────────────────
		r.Get("/api/recordings/{uniqueid}",       recordingHandler.Stream)
		r.Get("/api/recordings/{uniqueid}/link",  recordingHandler.GetSignedLink)

		// ── Сотрудники ────────────────────────────────────
		r.Get("/api/staff",                  staffHandler.GetStaff)
		r.Put("/api/staff/{id}/profile",     staffHandler.UpdateProfile)
		r.Post("/api/staff/{id}/avatar",     staffHandler.UploadAvatar)
		r.Delete("/api/staff/{id}/avatar",   staffHandler.DeleteAvatar)
		r.Delete("/api/staff/{id}",           staffHandler.DeleteStaff)

		// ── CRM: Тикеты ────────────────────────────────
		r.Get("/api/crm/tickets",                    crmHandler.GetTickets)
		r.Post("/api/crm/tickets",                   crmHandler.CreateTicket)
		r.Get("/api/crm/tickets/{id}",               crmHandler.GetTicket)
		r.Post("/api/crm/tickets/{id}/updates",      crmHandler.AddTicketUpdate)
		r.Patch("/api/crm/tickets/{id}/status",      crmHandler.ChangeTicketStatus)
		r.Patch("/api/crm/tickets/{id}/assign",      crmHandler.AssignTicket)
		r.Get("/api/crm/agents",                     crmHandler.GetAgentsList)
		r.Get("/api/crm/my-catalog",                 crmHandler.GetMyCatalog)

		// ── CRM: Каталоги ───────────────────────────────
		r.Get("/api/crm/catalogs",         crmCatalogHandler.GetCatalogs)
		r.Post("/api/crm/catalogs",        crmCatalogHandler.CreateCatalog)
		r.Get("/api/crm/catalogs/{id}",    crmCatalogHandler.GetCatalog)
		r.Put("/api/crm/catalogs/{id}",    crmCatalogHandler.UpdateCatalog)
		r.Delete("/api/crm/catalogs/{id}", crmCatalogHandler.DeleteCatalog)

		// ── CRM: Категории ──────────────────────────────
		r.Get("/api/crm/categories",          crmCatalogHandler.GetCategories)
		r.Post("/api/crm/categories",         crmCatalogHandler.CreateCategory)
		r.Put("/api/crm/categories/{id}",     crmCatalogHandler.UpdateCategory)
		r.Delete("/api/crm/categories/{id}",  crmCatalogHandler.DeleteCategory)

		// ── CRM: Назначения каталогов ───────────────────
		r.Get("/api/crm/catalog-assignments",             crmCatalogHandler.GetUserCatalogAssignments)
		r.Post("/api/crm/catalog-assignments",            crmCatalogHandler.AssignCatalogToUser)
		r.Delete("/api/crm/catalog-assignments/{userId}", crmCatalogHandler.UnassignCatalogFromUser)

		// ── CRM: Статусы ────────────────────────────────
		r.Get("/api/crm/statuses",         crmCatalogHandler.GetStatusList)
		r.Post("/api/crm/statuses",        crmCatalogHandler.CreateStatus)
		r.Put("/api/crm/statuses/{id}",    crmCatalogHandler.UpdateStatus)
		r.Delete("/api/crm/statuses/{id}", crmCatalogHandler.DeleteStatus)
	})

	// =========================
	// STATIC: аватары и записи звонков
	// =========================
	// Подписанные временные ссылки на записи (без JWT, но с HMAC подписью)
	r.Get("/api/recordings/{uniqueid}/play", recordingHandler.PlaySigned)

	r.Handle("/uploads/*", http.StripPrefix("/uploads/", http.FileServer(http.Dir("./uploads"))))

	// =========================
	// SWAGGER
	// =========================
	r.Get("/swagger/*", httpSwagger.WrapHandler)

	log.Printf("🚀 HTTP server started on %s\n", cfg.HTTP.Addr)
	log.Fatal(http.ListenAndServe(cfg.HTTP.Addr, r))
}