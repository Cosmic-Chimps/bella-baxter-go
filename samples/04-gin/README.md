# Sample 04: Gin

**Pattern:** SDK called in `main()` before starting the server — secrets pre-loaded and injected into every request via `BellaMiddleware` → `gin.Context`. Zero per-request overhead.

---

## Setup

```bash
go mod tidy

export BELLA_BAXTER_URL=https://baxter.yourcompany.com
export BELLA_API_KEY=bax-xxxxxxxxxxxxxxxxxxxx

go run .
```

---

## How it works

```
main()
  → loadSecrets() → BaxterClient.GetAllSecrets() → map[string]string
  → r := gin.Default()
  → r.Use(BellaMiddleware(secrets))   ← stores map in every gin.Context
  → r.Run(":8080")
  → per request: GetSecret(c, "KEY") → c.MustGet("bella_secrets").(map[string]string)
```

Secrets fetched **once** at startup, stored in a simple `map[string]string`, accessed per-request from the context — O(1) lookup, zero I/O.

---

## Accessing secrets in handlers

```go
// Option 1: GetSecret helper (with os.Getenv fallback)
r.GET("/api/data", func(c *gin.Context) {
    token := GetSecret(c, "THIRD_PARTY_TOKEN")
    db    := GetSecret(c, "DATABASE_URL")
    // ...
})

// Option 2: Full map for custom logic
r.GET("/api/data", func(c *gin.Context) {
    secrets := c.MustGet("bella_secrets").(map[string]string)
    token := secrets["THIRD_PARTY_TOKEN"]
})
```

---

## Route groups with per-group secrets

```go
api := r.Group("/api")
api.Use(BellaMiddleware(secrets))   // already applied globally above

// Or inject different secret subsets per group:
adminSecrets := filterSecrets(secrets, "ADMIN_")
admin := r.Group("/admin")
admin.Use(BellaMiddleware(adminSecrets))
```

---

## GORM / database/sql integration

```go
func main() {
    secrets, _ := loadSecrets()

    // GORM setup — secrets ready before Open
    dsn := getEnvOrSecret(secrets, "DATABASE_URL", "")
    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatal(err)
    }

    r := gin.Default()
    r.Use(BellaMiddleware(secrets))
    r.Use(func(c *gin.Context) {
        c.Set("db", db)
        c.Next()
    })
    // ...
}
```

---

## Echo / Fiber

Same pattern — load in `main()`, register as middleware:

```go
// Echo
e := echo.New()
e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
    return func(c echo.Context) error {
        c.Set("bella_secrets", secrets)
        return next(c)
    }
})

// Fiber
app := fiber.New()
app.Use(func(c *fiber.Ctx) error {
    c.Locals("bella_secrets", secrets)
    return c.Next()
})
```

---

## File layout

```
main.go     ← loadSecrets() + BellaMiddleware + GetSecret helper + handlers
go.mod
README.md
```

## Secret rotation

❌ **Not supported automatically.** Secrets are fetched once in `main()` before `router.Run()`. The `BellaMiddleware` closure captures the initial map.

**To pick up rotated secrets:** restart the process.

**For automatic rotation without restarts:** use `sync.RWMutex` and a background goroutine (same pattern as `03-stdlib`):

```go
var (
    secretsMu sync.RWMutex
    secrets   map[string]string
)

go func() {
    for range time.NewTicker(60 * time.Second).C {
        keyCtx, err := client.GetKeyContext(ctx)
        if err != nil { continue }
        fresh, err := client.GetAllSecrets(ctx, keyCtx.ProjectSlug, keyCtx.EnvironmentSlug)
        if err == nil {
            secretsMu.Lock()
            secrets = fresh.Secrets
            secretsMu.Unlock()
        }
    }
}()

// In BellaMiddleware: read under RLock
func BellaMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        secretsMu.RLock()
        snap := secrets  // copy reference under lock
        secretsMu.RUnlock()
        c.Set("secrets", snap)
        c.Next()
    }
}
```
