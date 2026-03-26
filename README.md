# Bella Baxter Go SDK

Go client for [Bella Baxter](https://github.com/cosmic-chimps/bella-baxter) — load secrets into your Go application at startup with optional end-to-end encryption.

[![pkg.go.dev](https://pkg.go.dev/badge/github.com/cosmic-chimps/bella-baxter-go.svg)](https://pkg.go.dev/github.com/cosmic-chimps/bella-baxter-go)

## Features

- **Simple API** — one `New()` call, then `GetAllSecrets()` or `InjectEnv()`
- **End-to-end encryption** — optional E2EE using ECDH-P256-HKDF-SHA256-AES256GCM; the server never sees plaintext secrets in transit
- **ENV injection** — `InjectEnv()` respects existing values (local dev overrides work)
- **Webhook signature verification** — `VerifyWebhookSignature()` validates Bella webhook payloads
- **Generated low-level client** — full OpenAPI-generated client via Kiota for advanced use cases

## Installation

```bash
go get github.com/cosmic-chimps/bella-baxter-go
```

## Quick start

```go
import "github.com/cosmic-chimps/bella-baxter-go/bellabaxter"

client, err := bellabaxter.New(bellabaxter.Options{
    BaxterURL: "https://baxter.example.com",
    ApiKey:    "bax-...",
})
if err != nil {
    log.Fatal(err)
}
defer client.Close()

// Fetch all secrets for the environment scoped to this API key
resp, err := client.GetAllSecrets(ctx, "my-project", "production")
if err != nil {
    log.Fatal(err)
}
fmt.Println(resp.Secrets["DATABASE_URL"])
```

## ENV injection

Load secrets directly into `os.Getenv` before starting your server:

```go
func main() {
    client, _ := bellabaxter.New(bellabaxter.Options{
        BaxterURL: os.Getenv("BELLA_BAXTER_URL"),
        ApiKey:    os.Getenv("BELLA_API_KEY"),
    })
    defer client.Close()

    // Inject into ENV — existing values are NOT overwritten (local dev wins)
    if err := client.InjectEnv(context.Background(), "my-project", "production"); err != nil {
        log.Fatal(err)
    }

    // From here, os.Getenv("DATABASE_URL") works as expected
    http.ListenAndServe(":"+os.Getenv("PORT"), router)
}
```

## Options

| Option | Default | Description |
|--------|---------|-------------|
| `BaxterURL` | `https://api.bella-baxter.io` | Base URL of the Bella Baxter API |
| `ApiKey` | — | API key (starts with `bax-`). Obtain from WebApp → Project → API Keys |
| `Timeout` | `10s` | Per-request HTTP timeout |
| `EnableE2EE` | `false` | Enable end-to-end encryption for secrets responses |

## Samples

| Sample | Approach | Best for |
|--------|----------|---------|
| [01-dotenv-file](./samples/01-dotenv-file/) | CLI → `.env` file | Scripts, CI/CD |
| [02-process-inject](./samples/02-process-inject/) | `bella run --` | Zero Go deps |
| [03-stdlib](./samples/03-stdlib/) | SDK in `main()` → config struct | `net/http`, chi |
| [04-gin](./samples/04-gin/) | SDK → Gin middleware | Gin, Echo, Fiber |
| [05-typed-secrets](./samples/05-typed-secrets/) | Generated `AppSecrets` struct | Type-safe access |

## Terraform Provider

A Terraform provider for managing Bella Baxter secrets as infrastructure:

```hcl
terraform {
  required_providers {
    bella = {
      source  = "cosmic-chimps/bella-baxter"
      version = "~> 0.1"
    }
  }
}

provider "bella" {
  baxter_url = "https://baxter.example.com"
  api_key    = var.bella_api_key
}

resource "bella_secret" "db_password" {
  key   = "RDS_PASSWORD"
  value = random_password.rds.result
}
```

→ [terraform-provider-bella-baxter](https://github.com/cosmic-chimps/terraform-provider-bella-baxter)

## License

Apache 2.0 — see [LICENSE](https://github.com/cosmic-chimps/bella-baxter-go/blob/main/LICENSE) for details.
