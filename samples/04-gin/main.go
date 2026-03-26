// Sample app — loads Bella Baxter secrets at startup and makes them available
// in Gin handlers via a middleware that sets them on gin.Context.
//
// Start:
//
//	BELLA_API_KEY=bella_ak_xxx BELLA_SECRET_KEY=sk_xxx go run .
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/cosmic-chimps/bella-baxter-go/bellabaxter"
	"github.com/gin-gonic/gin"
)

// contextKey is used to store secrets in gin.Context.
const secretsKey = "bella_secrets"

// BellaMiddleware returns a Gin middleware that stores the pre-loaded secrets
// in every request's gin.Context. Secrets are fetched ONCE at app startup —
// not per request.
func BellaMiddleware(secrets map[string]string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(secretsKey, secrets)
		c.Next()
	}
}

// GetSecret retrieves a Bella secret from gin.Context (set by BellaMiddleware).
// Falls back to os.Getenv if the key is not in the secrets map.
func GetSecret(c *gin.Context, key string) string {
	if secrets, ok := c.MustGet(secretsKey).(map[string]string); ok {
		if v := secrets[key]; v != "" {
			return v
		}
	}
	return os.Getenv(key)
}

func main() {
	secrets, err := loadSecrets()
	if err != nil {
		log.Fatalf("failed to load secrets: %v", err)
	}

	port := getEnvOrSecret(secrets, "PORT", "8080")
	// LISTEN_PORT overrides PORT for binding (useful in tests when default port conflicts).
	if override := os.Getenv("LISTEN_PORT"); override != "" {
		port = override
	}
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()

	// Register middleware — injects secrets into every request's context
	r.Use(BellaMiddleware(secrets))

	// ── Routes ──────────────────────────────────────────────────────────────

	r.GET("/", func(c *gin.Context) {
		db := GetSecret(c, "DATABASE_URL")
		if len(db) > 20 {
			db = db[:20] + "***"
		}
		c.JSON(http.StatusOK, gin.H{
			"message":      "Hello from Bella Baxter + Gin",
			"db":           db,
			"secretsCount": len(secrets),
		})
	})

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	r.GET("/config/:key", func(c *gin.Context) {
		key := c.Param("key")
		val := GetSecret(c, key)
		if val == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		masked := val
		if len(masked) > 6 {
			masked = masked[:4] + "***"
		}
		c.JSON(http.StatusOK, gin.H{"key": key, "value": masked})
	})

	log.Printf("listening on :%s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}

// loadSecrets fetches all secrets from Bella Baxter.
// Called once in main() — zero per-request overhead.
func loadSecrets() (map[string]string, error) {
	apiKey    := os.Getenv("BELLA_API_KEY")
	baxterURL := getEnv("BELLA_BAXTER_URL", "http://localhost:5000")

	if apiKey == "" {
		log.Println("BELLA_API_KEY not set — skipping secret load")
		return map[string]string{}, nil
	}

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
		return map[string]string{}, nil
	}

	resp, err := client.GetAllSecrets(ctx, keyCtx.ProjectSlug, keyCtx.EnvironmentSlug)
	if err != nil {
		log.Printf("warning: could not load Bella secrets: %v", err)
		return map[string]string{}, nil
	}

	log.Printf("loaded %d secret(s) from Bella env '%s'", len(resp.Secrets), keyCtx.EnvironmentSlug)
	return resp.Secrets, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvOrSecret(secrets map[string]string, key, fallback string) string {
	if v := secrets[key]; v != "" {
		return v
	}
	return getEnv(key, fallback)
}
