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

	// Layer 4C Step 6 — alert email delivery (SMTP).
	// When disabled, alerts are logged instead of emailed.
	EmailEnabled   bool
	SMTPHost       string
	SMTPPort       int
	SMTPUser       string
	SMTPPass       string
	SMTPFrom       string
	AlertEmailTo   string // optional override; default is the server owner's email
	WorkHourStart  int    // resolved-alert emails only sent between these hours
	WorkHourEnd    int
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

		EmailEnabled:  getEnv("EMAIL_ENABLED", "false") == "true",
		SMTPHost:      os.Getenv("SMTP_HOST"),
		SMTPPort:      parseInt(getEnv("SMTP_PORT", "587")),
		SMTPUser:      os.Getenv("SMTP_USER"),
		SMTPPass:      os.Getenv("SMTP_PASS"),
		SMTPFrom:      getEnv("SMTP_FROM", "alerts@yourplatform.app"),
		AlertEmailTo:  os.Getenv("ALERT_EMAIL_TO"),
		WorkHourStart: parseInt(getEnv("ALERT_WORK_HOUR_START", "9")),
		WorkHourEnd:   parseInt(getEnv("ALERT_WORK_HOUR_END", "18")),
	}
}

// EmailConfigured reports whether SMTP settings are present.
func (c *Config) EmailConfigured() bool {
	return c.EmailEnabled && c.SMTPHost != ""
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
