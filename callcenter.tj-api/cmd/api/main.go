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
	"callcentrix/internal/monitor"
	"callcentrix/internal/sip"
	"callcentrix/internal/ws"

	_ "callcentrix/docs"
)

func main() {
	// =========================
	// CONFIG
	// =========================
	cfg := config.Load()
	log.Println("✅ Config loaded")

	// =========================
	// DB
	// =========================
	pool, err := db.New(cfg.DB.DSN)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	// =========================
	// STORES
	// =========================
	agentStore := monitor.NewStore()
	callStore := monitor.NewCallStore()

	// =========================
	// AMI HANDLER
	// =========================
	amiHandler := &ami.Handler{
		Agents: agentStore,
		Calls:  callStore,
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

	go func() {
		log.Println("📡 Starting AMI service")
		amiService.Start()
	}()

	// =========================
	// ROUTER
	// =========================
	r := chi.NewRouter()

	// =========================
	// CORS
	// =========================
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{
			"http://localhost:3000",
			"http://localhost:5173",
		},
		AllowedMethods: []string{
			"GET", "POST", "PUT", "DELETE", "OPTIONS",
		},
		AllowedHeaders: []string{
			"Accept",
			"Authorization",
			"Content-Type",
		},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// =========================
	// HEALTH
	// =========================
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	// =========================
	// HANDLERS
	// =========================
	authHandler := &auth.Handler{
		DB:     pool,
		Secret: cfg.JWT.Secret,
		TTL:    time.Minute * time.Duration(cfg.JWT.TTLMinutes),
	}

	sipHandler := &sip.Handler{
		DB: pool,
	}

	// =========================
	// PUBLIC
	// =========================
	r.Post("/api/auth/login", authHandler.Login)

	// =========================
	// WEBSOCKET MONITOR
	// =========================
	r.Get("/ws/monitor", ws.Monitor(agentStore, callStore, cfg))

	// =========================
	// PROTECTED API
	// =========================
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(cfg.JWT.Secret))

		r.Get("/api/me", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(auth.FromContext(r.Context()))
		})

		r.Get("/api/sip/credentials", sipHandler.GetCredentials)
	})

	// =========================
	// SWAGGER
	// =========================
	r.Get("/swagger/*", httpSwagger.WrapHandler)

	// =========================
	// START
	// =========================
	log.Printf("🚀 HTTP server started on %s\n", cfg.HTTP.Addr)
	if err := http.ListenAndServe(cfg.HTTP.Addr, r); err != nil {
		log.Fatal(err)
	}
}
