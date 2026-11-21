package config

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"strings"
)

type Config struct {
	Port          string
	DatabaseURL   string
	AdminUsername string
	AdminPassword string
	JWTSecret     string
}

// Load reads configuration from environment variables, optionally falling back to a local .env file.
func Load() *Config {
	// Try loading .env if present (without external dependencies)
	loadDotEnv(".env")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Println("[WARN] DATABASE_URL is not set. Database connection may fail unless provided.")
	}

	adminUser := os.Getenv("ADMIN_USERNAME")
	if adminUser == "" {
		adminUser = "admin"
	}

	adminPass := os.Getenv("ADMIN_PASSWORD")
	if adminPass == "" {
		adminPass = "admin123"
		log.Println("[SECURITY WARNING] ADMIN_PASSWORD is using the default 'admin123'. Please set a secure password in production via environment variable ADMIN_PASSWORD!")
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = generateRandomSecret(32)
		log.Println("[WARN] JWT_SECRET is not set. A random secret was generated for this session. Logins will be invalidated on server restart.")
	}

	return &Config{
		Port:          port,
		DatabaseURL:   dbURL,
		AdminUsername: adminUser,
		AdminPassword: adminPass,
		JWTSecret:     jwtSecret,
	}
}

// loadDotEnv parses a simple KEY=VALUE .env file if it exists.
func loadDotEnv(filepath string) {
	file, err := os.Open(filepath)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			// Strip quotes if present
			val = strings.Trim(val, `"'`)
			// Only set if not already set in environment
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
	}
}

// generateRandomSecret generates a secure hex-encoded random string.
func generateRandomSecret(n int) string {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback-secret-key-please-change-in-production-123456"
	}
	return hex.EncodeToString(bytes)
}
