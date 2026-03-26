// Sample app — loads Bella Baxter secrets at startup using the Go SDK,
// then starts a stdlib net/http server. Secrets are passed to handlers
// via a Config struct (the idiomatic Go approach — no global state).
//
// Start:
//
//	BELLA_API_KEY=bella_ak_xxx BELLA_SECRET_KEY=sk_xxx go run .
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/cosmic-chimps/bella-baxter-go/bellabaxter"
)

// Config holds the application config populated from Bella secrets.
// Passed to every handler — no global variables needed.
type Config struct {
	Port        string
	DatabaseURL string
	Secrets     map[string]string // all secrets for direct lookup
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", cfg.handleIndex)
	mux.HandleFunc("GET /health", cfg.handleHealth)
	mux.HandleFunc("GET /config/{key}", cfg.handleConfig)

	addr := ":" + cfg.Port
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

// loadConfig fetches secrets from Bella Baxter and builds the Config.
// Called once in main() — zero per-request overhead.
func loadConfig() (*Config, error) {
	apiKey    := os.Getenv("BELLA_API_KEY")
	baxterURL := getEnv("BELLA_BAXTER_URL", "http://localhost:5000")

	secrets := map[string]string{}

	if apiKey != "" {
		client, err := bellabaxter.New(bellabaxter.Options{
			BaxterURL:  baxterURL,
			ApiKey:     apiKey,
			EnableE2EE: true,
			Timeout:    10 * time.Second,
		})
		if err != nil {
			return nil, fmt.Errorf("bellabaxter.New: %w", err)
		}
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		keyCtx, err := client.GetKeyContext(ctx)
		if err != nil {
			log.Printf("warning: could not resolve key context: %v", err)
			return &Config{Port: "8080", Secrets: secrets}, nil
		}

		resp, err := client.GetAllSecrets(ctx, keyCtx.ProjectSlug, keyCtx.EnvironmentSlug)
		if err != nil {
			log.Printf("warning: could not load Bella secrets: %v", err)
			// Continue without secrets — app may have fallback values
		} else {
			secrets = resp.Secrets
			log.Printf("loaded %d secret(s) from Bella env '%s'", len(secrets), keyCtx.EnvironmentSlug)
		}
	} else {
		log.Println("BELLA_API_KEY not set — skipping secret load")
	}

	// LISTEN_PORT overrides PORT for the binding address (useful in tests
	// when the default PORT value conflicts with an existing process).
	port := getEnvOrSecret(secrets, "PORT", "8080")
	if override := os.Getenv("LISTEN_PORT"); override != "" {
		port = override
	}

	return &Config{
		Port:        port,
		DatabaseURL: getEnvOrSecret(secrets, "DATABASE_URL", ""),
		Secrets:     secrets,
	}, nil
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (c *Config) handleIndex(w http.ResponseWriter, r *http.Request) {
	db := c.DatabaseURL
	if len(db) > 20 {
		db = db[:20] + "***"
	}
	writeJSON(w, map[string]any{
		"message":      "Hello from Bella Baxter + stdlib net/http",
		"db":           db,
		"secretsCount": len(c.Secrets),
	})
}

func (c *Config) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]bool{"ok": true})
}

func (c *Config) handleConfig(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	val, ok := c.Secrets[key]
	if !ok {
		val = os.Getenv(key) // fallback to process env
	}
	if val == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	masked := val
	if len(masked) > 6 {
		masked = masked[:4] + "***"
	}
	writeJSON(w, map[string]string{"key": key, "value": masked})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnvOrSecret returns the value from secrets map first, then os.Getenv, then fallback.
func getEnvOrSecret(secrets map[string]string, key, fallback string) string {
	if v := secrets[key]; v != "" {
		return v
	}
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
