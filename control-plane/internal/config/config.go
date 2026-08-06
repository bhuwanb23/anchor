package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	Port             string
	Env              string
	JWTSecret        string
	JWTExpiryHrs     int
	RefreshTokenDays int // Layer 5A Step 2C — refresh token lifetime (default 30)
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

	// Layer 5C Step 5 — backup of the database itself.
	// Local backups (VACUUM INTO + gzip) run every DBBackupIntervalHours and
	// are kept in DBBackupDir; the S3 upload runs daily when configured.
	DBBackupDir           string
	DBBackupIntervalHours int
	S3Endpoint            string
	S3AccessKey           string
	S3SecretKey           string
	S3Bucket              string
	S3Region              string
	// DBBackupEncryptionKey is the passphrase used to derive the AES-256 key
	// that encrypts S3 uploads. S3 upload is skipped when it is empty.
	DBBackupEncryptionKey string
}

func Load() *Config {
	return &Config{
		Port:             getEnv("PORT", "8080"),
		Env:              getEnv("ENV", "development"),
		JWTSecret:        getEnv("JWT_SECRET", "dev-secret-change-in-production"),
		JWTExpiryHrs:     parseInt(getEnv("JWT_EXPIRY_HOURS", "24")),
		RefreshTokenDays: parseInt(getEnv("REFRESH_TOKEN_DAYS", "30")),
		DatabasePath:     getEnv("DATABASE_PATH", "./data/yourplatform.db"),
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

		DBBackupDir:           dbBackupDir(getEnv("DB_BACKUP_DIR", "")),
		DBBackupIntervalHours: parseInt(getEnv("DB_BACKUP_INTERVAL_HOURS", "6")),
		S3Endpoint:            os.Getenv("S3_ENDPOINT"),
		S3AccessKey:           os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:           os.Getenv("S3_SECRET_KEY"),
		S3Bucket:              os.Getenv("S3_BUCKET"),
		S3Region:              os.Getenv("S3_REGION"),
		DBBackupEncryptionKey: os.Getenv("DB_BACKUP_ENCRYPTION_KEY"),
	}
}

// dbBackupDir resolves the local backup directory: an explicit value wins;
// otherwise backups live in a "backups" subdirectory next to the database
// file (default DATABASE_PATH is ./data/yourplatform.db → ./data/backups).
func dbBackupDir(explicit string) string {
	if explicit != "" {
		return explicit
	}
	return filepath.Join(filepath.Dir(getEnv("DATABASE_PATH", "./data/yourplatform.db")), "backups")
}

// EmailConfigured reports whether SMTP settings are present.
func (c *Config) EmailConfigured() bool {
	return c.EmailEnabled && c.SMTPHost != ""
}

// DNSConfigured returns true if Cloudflare credentials are set.
func (c *Config) DNSConfigured() bool {
	return c.CloudflareToken != "" && c.CloudflareZoneID != ""
}

// S3Configured reports whether S3 database uploads can run: endpoint + full
// credentials + bucket + an encryption passphrase (uploads are never sent in
// the clear).
func (c *Config) S3Configured() bool {
	return c.S3Endpoint != "" && c.S3AccessKey != "" && c.S3SecretKey != "" &&
		c.S3Bucket != "" && c.DBBackupEncryptionKey != ""
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
