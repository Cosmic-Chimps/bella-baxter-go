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
| `PrivateKeyPEM` | `BELLA_BAXTER_PRIVATE_KEY` | Device key (PKCS#8 P-256, PEM or base64 DER). When supplied, it is presented and responses are decrypted with it — no other option needed |
| `EnableE2EE` | `false` | End-to-end encryption **without** a device key (an ephemeral key per client). Not needed when a device key is supplied |
| `DisableE2EE` | `false` | Explicit opt-out: never present a key, even a supplied one (logs one warning if a key is supplied) |

## Device key (ZKE)

When a device key is supplied — `PrivateKeyPEM`, or the `BELLA_BAXTER_PRIVATE_KEY` variable that
`bella sdk run` injects — the client presents it as `X-E2E-Public-Key` on every secrets request and
decrypts the response with it. You do not need `EnableE2EE` for that. Under ZKE enforcement this is
what lets the app read at all: a request that presents no registered key is refused with a 403.

```go
client, err := bellabaxter.New(bellabaxter.Options{
    BaxterURL: os.Getenv("BELLA_BAXTER_URL"),
    ApiKey:    os.Getenv("BELLA_API_KEY"),
    // PrivateKeyPEM defaults to BELLA_BAXTER_PRIVATE_KEY
})
```

- A key that is set but unreadable makes `New` return an error naming `BELLA_BAXTER_PRIVATE_KEY` (or
  `Options.PrivateKeyPEM`). It is never replaced by a throwaway key.
- `DisableE2EE: true` keeps the key off the wire. `New` then logs one warning through the standard `log`
  package, because under enforcement every read will be refused.
- Setting both `EnableE2EE` and `DisableE2EE` is an error.
- Once a key is presented, a secrets read that does not come back as an envelope decrypting with it is
  refused with an `*bellabaxter.E2EEResponseError` (match with `errors.As`): `Code` is
  `e2ee-plaintext-response` for a plaintext answer and `e2ee-decryption-failed` for a tampered envelope or
  one encrypted to another key. There is no plaintext fallback (#1050).

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
