// Typed Secrets sample — one secret per Bella type:
//   String → ExternalApiKey
//   Int    → Port
//   Bool   → EnableFeatures
//   Uri    → DatabaseUrl  ← *url.URL
//   JSON   → AppConfig    ← parsed into AppConfigShape struct
//   GUID   → AppId        ← parsed into uuid.UUID
//
// Workflow:
//   bella secrets generate go -p my-project -e production -o secrets/secrets.go
//   bella exec -- go run .
package main

import (
"fmt"

"github.com/joho/godotenv"

"bella-typed-secrets-go/secrets"
)

func main() {
_ = godotenv.Load()

s := secrets.AppSecrets{}
cfg := s.AppConfig()

fmt.Println("=== Bella Baxter: Typed Secrets (Go) ===")
fmt.Println()
fmt.Printf("String  EXTERNAL_API_KEY : %s***\n", s.ExternalApiKey()[:4])
fmt.Printf("Int     PORT             : %d  ← type: int\n", s.Port())
fmt.Printf("Bool    ENABLE_FEATURES  : %v  ← type: bool\n", s.EnableFeatures())
fmt.Printf("Uri     DATABASE_URL     : scheme=%s  ← type: *url.URL\n", s.DatabaseUrl().Scheme)
fmt.Printf("JSON    APP_CONFIG       : %+v\n", cfg)
fmt.Printf("           .Setting1     : %q  ← string\n", cfg.Setting1)
fmt.Printf("           .Setting2     : %d  ← int\n", cfg.Setting2)
fmt.Printf("GUID    APP_ID           : %s  ← type: uuid.UUID\n", s.AppId())
fmt.Println()
fmt.Println("No raw os.Getenv calls — secrets are typed, validated, and structured.")
}
