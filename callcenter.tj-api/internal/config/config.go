package config

import (
	"log"
	"os"
	"strconv"
)

type Config struct {
	HTTP     HTTPConfig
	DB       DBConfig
	JWT      JWTConfig
	AMI      AMIConfig
	Asterisk AsteriskConfig
}

type HTTPConfig struct {
	Addr       string
	PublicBase string
}

type DBConfig struct {
	DSN string
}

type JWTConfig struct {
	Secret     string
	TTLMinutes int
}

type AMIConfig struct {
	Addr     string
	Username string
	Password string
}

type AsteriskConfig struct {
	RecordingURL string // URL nginx на Asterisk сервере
}

func Load() *Config {
	cfg := &Config{}

	// HTTP
	cfg.HTTP.Addr       = getEnv("HTTP_ADDR", ":8080")
	cfg.HTTP.PublicBase = getEnv("HTTP_PUBLIC_BASE", "http://localhost:8080")

	// DATABASE
	cfg.DB.DSN = getEnv("DB_DSN", "postgres://postgres:postgres@172.20.40.2:5432/postgres?sslmode=disable")

	// JWT
	cfg.JWT.Secret     = getEnv("JWT_SECRET", "CHANGE_ME_SECRET")
	cfg.JWT.TTLMinutes = getEnvInt("JWT_TTL_MINUTES", 60)

	// ASTERISK AMI
	cfg.AMI.Addr     = getEnv("AMI_ADDR", "172.20.40.3:5038")
	cfg.AMI.Username = getEnv("AMI_USER", "asterisk")
	cfg.AMI.Password = getEnv("AMI_PASS", "asterisk")

	// ASTERISK RECORDINGS (nginx)
	cfg.Asterisk.RecordingURL = getEnv("ASTERISK_RECORDING_URL", "http://172.20.40.3:8090/recordings")

	log.Println("✅ Config loaded")
	return cfg
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}