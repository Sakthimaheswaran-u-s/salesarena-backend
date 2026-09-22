// Package config loads runtime settings from the environment (and an optional .env file).
package config

import (
	"bufio"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port          string
	MongoURI      string
	MongoDB       string
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	CORSOrigins   []string
	SessionTTL    time.Duration
	Timezone      *time.Location
	DemoMode      bool // enables POST /api/demo/reset
	SeedOnStart   bool // seed demo data when the users collection is empty
	GinMode       string
}

// Load reads .env (if present) and then the process environment.
func Load() Config {
	loadDotEnv(".env")

	tzName := get("TZ", "Local")
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		slog.Warn("invalid TZ, falling back to Local", "tz", tzName)
		loc = time.Local
	}

	return Config{
		Port:          get("PORT", "8080"),
		MongoURI:      get("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:       get("MONGO_DB", "salesarena"),
		RedisAddr:     get("REDIS_ADDR", "localhost:6379"),
		RedisPassword: get("REDIS_PASSWORD", ""),
		RedisDB:       getInt("REDIS_DB", 0),
		CORSOrigins:   strings.Split(get("CORS_ORIGINS", "http://localhost:5173,http://127.0.0.1:5173"), ","),
		SessionTTL:    getDuration("SESSION_TTL", 24*time.Hour),
		Timezone:      loc,
		DemoMode:      getBool("DEMO_MODE", true),
		SeedOnStart:   getBool("SEED_ON_START", true),
		GinMode:       get("GIN_MODE", "debug"),
	}
}

func get(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	if v, err := strconv.Atoi(get(key, "")); err == nil {
		return v
	}
	return def
}

func getBool(key string, def bool) bool {
	if v, err := strconv.ParseBool(get(key, "")); err == nil {
		return v
	}
	return def
}

func getDuration(key string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(get(key, "")); err == nil {
		return v
	}
	return def
}

// loadDotEnv sets variables from a KEY=VALUE file without overriding existing env.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}
}
