# Sample 05: Typed Secrets (`bella secrets generate go`)

**Pattern:** `bella secrets generate go` → typed accessor struct → no more `os.Getenv("TYPO")`

---

## How it works

```
bella secrets generate go --project my-project --environment production
↓
secrets/secrets.go  (generated, safe to commit — contains NO secret values)
↓
import "bella-typed-secrets-go/secrets"
↓
secrets.AppSecrets{}.DatabaseUrl()  (typed, IDE-autocomplete, runtime panic on missing)
```

## Setup

```bash
# Install dependencies
go mod tidy

# Run with secrets injected by bella
bella run --api-key bax-... -- go run .
```

## Why use typed secrets?

- **Type safety** — `PORT` is an `int`, not a string you forget to parse
- **IDE autocomplete** — `s.` shows all available secrets
- **Fail-fast validation** — missing secrets panic at startup, not in production
- **Safe to commit** — generated file contains NO secret values, just key names and types

## What's generated

`bella secrets generate go` reads your project's secret manifest and emits `secrets/secrets.go`.
The actual secrets for this sample (`AppSecrets`) map to:

| Secret                       | Type         | Accessor                      |
|------------------------------|--------------|-------------------------------|
| `PORT`                       | `int`        | `s.Port()`                    |
| `DATABASE_URL`               | `*url.URL`   | `s.DatabaseUrl()`             |
| `EXTERNAL_API_KEY`           | `string`     | `s.ExternalApiKey()`          |
| `ENABLE_FEATURES`            | `bool`       | `s.EnableFeatures()`          |
| `APP_CONFIG`                 | `AppConfigShape` (JSON) | `s.AppConfig()`  |
| `APP_ID`                     | `uuid.UUID`  | `s.AppId()`                   |

## Regenerate after adding secrets

```bash
bella secrets generate go -p my-project -e production -o secrets/secrets.go
git add secrets/secrets.go  # safe — no values
```

## APP_CONFIG — JSON struct mapping

`APP_CONFIG` is a JSON secret. The generated `AppConfigShape` struct maps its fields:

```go
type AppConfigShape struct {
    Setting1 string `json:"setting1"`
    Setting2 int    `json:"setting2"`
}
```

Add fields to match your own JSON secret shape, then re-generate.
