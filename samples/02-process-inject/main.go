// Sample app — reads secrets injected directly into the process by bella run.
//
// Start with:
//
//	bella run -p my-project -e production -- go run .
//
// No .env file is written. Secrets are already in os.Getenv() from the parent process.
package main

import (
	"fmt"
	"os"
)

func main() {
	port   := getEnv("PORT", "3000")
	dbURL  := getEnv("DATABASE_URL", "(not set)")
	apiKey := getEnv("EXTERNAL_API_KEY", "(not set)")

	fmt.Println("=== Bella Baxter: process inject sample (Go) ===")
	fmt.Printf("PORT         : %s\n", port)
	fmt.Printf("DATABASE_URL : %s\n", dbURL)
	fmt.Printf("API_KEY      : %s\n", mask(apiKey))
	fmt.Println()
	fmt.Println("Secrets injected directly into process by: bella run -- go run .")
	fmt.Println("No .env file was written.")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mask(s string) string {
	if len(s) > 6 {
		return s[:4] + "***"
	}
	return "(not set)"
}
