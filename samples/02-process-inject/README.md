# Sample 02: Process Inject (bella run) — Go

**Pattern:** `bella run -- go run .` — secrets injected as env vars, no file written.

## Setup

```bash
# Authenticate
bella login

# Run with secrets injected
bella run -p my-project -e production -- go run .

# Or with a pre-built binary
go build -o myapp . && bella run -- ./myapp
```

## Works with any Go command

```bash
# Run compiled binary
bella run -- ./myapp

# Dev mode
bella run -- go run .

# Tests
bella run -e test -- go test ./...

# Gin / Echo / Fiber
bella run -- go run ./cmd/server

# Air (live reload)
bella run -- air
```

## vs. `.env` file

| | `bella secrets get -o .env` | `bella run --` |
|---|---|---|
| File on disk | ✅ Yes | ❌ No |
| Extra dep (godotenv) | ✅ Yes | ❌ No |
| Secret security | File system | Memory only |
| Any command | ✅ Yes | ✅ Yes |
