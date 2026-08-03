package config

import "os"

type Config struct {
	Port             string
	Env              string
	JWTSecret        string
	JWTExpiryHrs     int
	DatabasePath     string
	FrontendURL      string
	WSPath           string
	BaseDomain       string
	CloudflareToken  string
	CloudflareZoneID string
}

func Load() *Config {
	return &Config{
		Port:             getEnv("PORT", "8080"),
		Env:              getEnv("ENV", "development"),
		JWTSecret:        getEnv("JWT_SECRET", "dev-secret-change-in-production"),
		JWTExpiryHrs:     parseInt(getEnv("JWT_EXPIRY_HOURS", "24")),
		DatabasePath:     getEnv("DATABASE_PATH", "./yourplatform.db"),
		FrontendURL:      getEnv("FRONTEND_URL", "http://localhost:3000"),
		WSPath:           getEnv("WS_PATH", "/ws/agent"),
		BaseDomain:       getEnv("BASE_DOMAIN", "yourplatform.app"),
		CloudflareToken:  os.Getenv("CLOUDFLARE_API_TOKEN"),
		CloudflareZoneID: os.Getenv("CLOUDFLARE_ZONE_ID"),
	}
}

// DNSConfigured returns true if Cloudflare credentials are set.
func (c *Config) DNSConfigured() bool {
	return c.CloudflareToken != "" && c.CloudflareZoneID != ""
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func parseInt(s string) int {
	n := 0
	for _, ch := range s {
		if ch >= '0' && ch <= '9' {
			n = n*10 + int(ch-'0')
		}
	}
	return n
}
