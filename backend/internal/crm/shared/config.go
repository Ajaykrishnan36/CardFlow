package shared

import (
	"crypto/sha256"
	"encoding/base64"
	"os"
	"strconv"
	"strings"
)

// Config is the CRM module's own configuration (DECISIONS D-10, D-16..D-18).
// It deliberately reads CRM_* variables so nothing collides with CardFlow's settings.
type Config struct {
	AppEnv        string // local | dev | staging | production
	AppName       string
	BaseURL       string // web origin used in emailed links
	SeedDemo      bool
	EncryptionKey []byte
	CookieSecure  bool

	SMTPHost string
	SMTPPort int
	SMTPUser string
	SMTPPass string
	SMTPFrom string

	OwnerEmail             string
	OwnerBootstrapPassword string
}

func (c Config) IsProduction() bool { return c.AppEnv == "production" }

// IsLocalOrDev reports whether demo seeding is permitted (PRD §13.5).
func (c Config) IsLocalOrDev() bool { return c.AppEnv == "local" || c.AppEnv == "dev" }

// MFAEnforced reports whether privileged identities must use MFA (D-11).
func (c Config) MFAEnforced() bool { return c.AppEnv != "local" }

// LoadConfig builds the CRM config. cardflowEnv is CardFlow's ENV value, used only to
// derive a default CRM_APP_ENV. Problems lists settings that make the CRM unsafe to run.
func LoadConfig(cardflowEnv string) (cfg Config, problems []string) {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("CRM_APP_ENV")))
	if env == "" {
		// Render always sets RENDER=true: a hosted server is never "local", even if ENV is mis-set,
		// so the demo credential can't be seeded there.
		if strings.EqualFold(cardflowEnv, "production") || os.Getenv("RENDER") != "" {
			env = "production"
		} else {
			env = "local"
		}
	}
	switch env {
	case "local", "dev", "staging", "production":
	default:
		problems = append(problems, "CRM_APP_ENV must be local, dev, staging or production")
		env = "production"
	}
	cfg.AppEnv = env

	cfg.AppName = firstNonEmpty(os.Getenv("CRM_APP_NAME"), "Ajay's CRM")

	defaultBase := "http://localhost:3001"
	if env != "local" {
		defaultBase = "https://card-flow-kappa.vercel.app"
	}
	cfg.BaseURL = strings.TrimRight(firstNonEmpty(os.Getenv("CRM_BASE_URL"), defaultBase), "/")

	seedDefault := cfg.IsLocalOrDev()
	cfg.SeedDemo = parseBool(os.Getenv("CRM_SEED_DEMO"), seedDefault)
	if cfg.IsProduction() && cfg.SeedDemo {
		problems = append(problems, "CRM_SEED_DEMO must be false in production")
	}

	cfg.CookieSecure = parseBool(os.Getenv("CRM_COOKIE_SECURE"), env != "local")

	if raw := strings.TrimSpace(os.Getenv("CRM_ENCRYPTION_KEY_BASE64")); raw != "" {
		key, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(key) != 32 {
			problems = append(problems, "CRM_ENCRYPTION_KEY_BASE64 must be 32 bytes, base64-encoded")
		} else {
			cfg.EncryptionKey = key
		}
	}
	if cfg.EncryptionKey == nil {
		if env == "local" {
			// Documented, local-only fallback (D-18). Never used outside CRM_APP_ENV=local.
			sum := sha256.Sum256([]byte("ajays-crm-local-dev-encryption-key"))
			cfg.EncryptionKey = sum[:]
		} else {
			problems = append(problems, "CRM_ENCRYPTION_KEY_BASE64 is required outside local")
		}
	}

	cfg.SMTPHost = strings.TrimSpace(os.Getenv("CRM_SMTP_HOST"))
	cfg.SMTPPort, _ = strconv.Atoi(firstNonEmpty(os.Getenv("CRM_SMTP_PORT"), "587"))
	cfg.SMTPUser = os.Getenv("CRM_SMTP_USER")
	cfg.SMTPPass = os.Getenv("CRM_SMTP_PASS")
	cfg.SMTPFrom = firstNonEmpty(os.Getenv("CRM_SMTP_FROM"), cfg.AppName+" <no-reply@localhost>")

	cfg.OwnerEmail = strings.ToLower(strings.TrimSpace(os.Getenv("CRM_OWNER_EMAIL")))
	cfg.OwnerBootstrapPassword = os.Getenv("CRM_OWNER_BOOTSTRAP_PASSWORD")

	return cfg, problems
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func parseBool(raw string, def bool) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return def
	}
	return b
}
