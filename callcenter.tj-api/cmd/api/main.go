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

	agentStore := monitor.NewStore()
	callStore := monitor.NewCallStore()
	queueStore := monitor.NewQueueStore()

	tenantResolver := monitor.NewTenantResolver(pool)

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

	r := chi.NewRouter()

	// =========================
	// 🔥 CORS ДОЛЖЕН БЫТЬ ПЕРВЫМ
	// =========================
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{
			"http://localhost:3000",
			"http://localhost:5173",
			"http://localhost:8080",
		},
		AllowedMethods: []string{
			"GET", "POST", "PUT", "DELETE", "OPTIONS",
		},
		AllowedHeaders: []string{
			"Accept",
			"Authorization",
			"Content-Type",
		},
		ExposedHeaders: []string{
			"Authorization",
		},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	authHandler := &auth.Handler{
		DB:     pool,
		Secret: cfg.JWT.Secret,
		TTL:    time.Minute * time.Duration(cfg.JWT.TTLMinutes),
	}

	sipHandler := &sip.Handler{DB: pool}

	// ================= PUBLIC =================
	r.Post("/api/auth/login", authHandler.Login)

	r.Get("/ws/monitor", ws.Monitor(
		agentStore,
		callStore,
		queueStore,
		cfg,
	))

	// ================= PROTECTED =================
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(cfg.JWT.Secret))

		r.Get("/api/me", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(auth.FromContext(r.Context()))
		})

		r.Get("/api/sip/credentials", sipHandler.GetCredentials)
		r.Get("/api/agents/info", agentsInfoHandler.GetAgentsInfo)

		r.Post("/api/actions/pause", actionsHandler.TogglePause)
		r.Post("/api/actions/hangup", actionsHandler.Hangup)
	})

	r.Get("/swagger/*", httpSwagger.WrapHandler)

	log.Printf("🚀 HTTP server started on %s\n", cfg.HTTP.Addr)
	log.Fatal(http.ListenAndServe(cfg.HTTP.Addr, r))
}