package main

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	httpSwagger "github.com/swaggo/http-swagger"

	"callcentrix/internal/auth"
	"callcentrix/internal/config"
	"callcentrix/internal/db"
	"callcentrix/internal/sip"

	_ "callcentrix/docs"
)

// @title CallCentrix API
// @version 1.0
// @description CRM + VoIP backend
// @BasePath /
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	cfg := config.Load()

	pool, err := db.New(cfg.DB.DSN)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	r := chi.NewRouter()

	// CORS (для Vite / браузера)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{
			"http://localhost:5173",
			"http://localhost:3000",
		},
		AllowedMethods: []string{
			"GET", "POST", "PUT", "DELETE", "OPTIONS",
		},
		AllowedHeaders: []string{
			"Accept", "Authorization", "Content-Type",
		},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	// Handlers
	authHandler := &auth.Handler{
		DB:     pool,
		Secret: cfg.JWT.Secret,
		TTL:    time.Minute * time.Duration(cfg.JWT.TTLMinutes),
	}

	sipHandler := &sip.Handler{
		DB: pool,
	}

	// Public routes
	r.Post("/api/auth/login", authHandler.Login)

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(cfg.JWT.Secret))

		r.Get("/api/me", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(auth.FromContext(r.Context()))
		})

		r.Get("/api/sip/credentials", sipHandler.GetCredentials)
	})

	// Swagger
	r.Get("/swagger/*", httpSwagger.WrapHandler)

	// Start server
	http.ListenAndServe(cfg.HTTP.Addr, r)
}
