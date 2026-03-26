# Sample 01: `.env` File Approach (Go)

**Pattern:** CLI writes secrets to a `.env` file → app reads it with `godotenv`.

Works with any Go application — stdlib, Gin, Echo, gRPC, etc.

---

## How it works

```
bella secrets get -o .env   →   .env file on disk   →   godotenv.Load()   →   os.Getenv()
```

## Setup

```bash
go mod tidy

# Authenticate
bella login

# Pull secrets then run
bella secrets get -p my-project -e production -o .env && go run .
```

## Works with any Go command

```bash
# Any binary
bella secrets get -o .env && go run .
bella secrets get -o .env && ./myapp

# Tests
bella secrets get -e test -o .env && go test ./...

# Docker build time injection
bella secrets get -o .env && docker build --env-file .env .
```

## Gin / Echo integration

```go
// Load .env before starting the server
func main() {
    godotenv.Load()

    r := gin.Default()
    r.GET("/", func(c *gin.Context) {
        // os.Getenv works normally
        c.JSON(200, gin.H{"db": os.Getenv("DATABASE_URL")})
    })
    r.Run(":" + os.Getenv("PORT"))
}
```

## Security notes

- Add `.env` to `.gitignore`
- Prefer the SDK approach (samples 03/04) for long-running services — no file on disk

## Secret rotation

❌ **Not supported automatically.** The `.env` file is written once by `bella secrets get` and read once at process start. Secrets loaded into `os.Getenv()` are static for the lifetime of the process.

**To pick up rotated secrets:** re-write the file and restart:

```bash
bella secrets get -o .env && go run main.go
# or with a compiled binary:
bella secrets get -o .env && ./myapp
```

For automatic rotation without restarts, use the process-inject approach with a background reload strategy, or consider running Baxter's SDK in a goroutine that periodically calls `GetAllSecrets` and updates a shared `sync.Map`.
