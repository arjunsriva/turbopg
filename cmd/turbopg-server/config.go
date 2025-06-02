package main

import (
	"os"
)

// ServerConfig holds the configuration for the server.
type ServerConfig struct {
	DatabaseURL string
	APIKey      string
	Port        string
	StorePrefix string
}

// loadConfig loads the server configuration from environment variables or uses default values.
func loadConfig() ServerConfig {
	config := ServerConfig{
		DatabaseURL: getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"),
		APIKey:      getEnv("TURBOPG_API_KEY", "testapikey"),
		Port:        getEnv("TURBOPG_PORT", "8080"),
		StorePrefix: getEnv("TURBOPG_STORE_PREFIX", "tpga_"),
	}
	return config
}

// getEnv retrieves the value of an environment variable or returns a default value if not set.
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
