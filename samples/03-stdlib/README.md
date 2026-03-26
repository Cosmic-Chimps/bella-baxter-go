# Sample 03: stdlib `net/http`

**Pattern:** SDK called in `main()` before `http.ListenAndServe` — secrets stored in a `Config` struct, injected into handlers as methods. No global state, idiomatic Go.

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

Go's stdlib is production-ready — no framework needed:

```
main()
  → loadConfig() → BaxterClient.GetAllSecrets()
  → Config{ Port, DatabaseURL, Secrets }
  → http.NewServeMux()
  → mux.HandleFunc("GET /", cfg.handleIndex)  ← handler is a method on Config
  → http.ListenAndServe(":8080", mux)
```

Secrets are fetched **once** at startup. No per-request overhead. The `Config` struct is passed to handlers as a receiver — no global variables.

---

## Using secrets in handlers

```go
// Any secret by key
func (c *Config) handleSomething(w http.ResponseWriter, r *http.Request) {
    token := c.Secrets["THIRD_PARTY_TOKEN"]
    dbURL := c.DatabaseURL  // or c.Secrets["DATABASE_URL"]
    // ...
}
```

---

## database/sql integration

```go
func loadConfig() (*Config, error) {
    // ... load secrets ...

    db, err := sql.Open("pgx", secrets["DATABASE_URL"])
    if err != nil {
        return nil, fmt.Errorf("open db: %w", err)
    }

    return &Config{
        DB:      db,
        Secrets: secrets,
    }, nil
}
```

---

## Go 1.22 pattern routing

```go
// Go 1.22+ pattern routing (used in this sample)
mux.HandleFunc("GET /users/{id}", cfg.handleUser)
mux.HandleFunc("GET /health", cfg.handleHealth)
```

For older Go versions, use a router like `chi`:
```go
r := chi.NewRouter()
r.Get("/", cfg.handleIndex)
```

---

## File layout

```
main.go     ← loadConfig() + handlers as Config methods
go.mod
README.md
```

This is intentionally one file — in a real app split into `config/`, `handlers/`, etc.

## Secret rotation

❌ **Not supported automatically.** Secrets are fetched once in `main()` before the server starts. The `map[string]string` is immutable after startup.

**To pick up rotated secrets:** restart the process.

```bash
# Kubernetes rolling restart
kubectl rollout restart deployment/myapp

# Docker
docker restart myapp

# systemd
systemctl restart myapp
```

**For automatic rotation without restarts:** add a background goroutine that periodically re-fetches and atomically replaces the secrets map:

```go
var secretsMu sync.RWMutex
var secrets map[string]string

go func() {
    ticker := time.NewTicker(60 * time.Second)
    for range ticker.C {
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
```

Handlers read via `secretsMu.RLock()` / `secretsMu.RUnlock()`.
