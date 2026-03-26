// Sample app — reads secrets written to a .env file by the Bella CLI.
//
// Start with:
//
//	bella secrets get -p my-project -e production -o .env && go run .
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
)

func main() {
	// Load .env file (silently OK if not present — os.Getenv fallback)
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("warning: could not load .env file: %v", err)
	}

	port   := getEnv("PORT", "3000")
	dbURL  := getEnv("DATABASE_URL", "(not set)")
	apiKey := getEnv("EXTERNAL_API_KEY", "(not set)")

	fmt.Println("=== Bella Baxter: .env file sample (Go) ===")
	fmt.Printf("PORT         : %s\n", port)
	fmt.Printf("DATABASE_URL : %s\n", dbURL)
	fmt.Printf("API_KEY      : %s\n", mask(apiKey))
	fmt.Println()
	fmt.Println("All secrets loaded from .env file written by: bella secrets get -o .env")
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
