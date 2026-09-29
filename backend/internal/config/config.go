package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port           string
	Env            string
	APIBaseURL     string
	PublicWebURL   string
	AllowedOrigins []string

	// Database
	DatabaseURL       string
	DBHost            string
	DBPort            string
	DBUser            string
	DBPassword        string
	DBName            string
	DBSSLMode         string
	DBMaxOpenConns    int
	DBMaxIdleConns    int
	DBConnMaxLifetime string

	// Redis
	RedisURL      string
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// JWT
	JWTPrivateKey       string
	JWTPublicKey        string
	JWTIssuer           string
	JWTAccessExpiryMin  int
	JWTRefreshExpiryDay int

	// Data Encryption (AES-256)
	DataEncryptionKey string

	// S3 Storage
	S3Endpoint        string
	S3Region          string
	S3PrivateBucket   string
	S3PublicBucket    string
	S3UseSSL          bool
	S3PublicCDNURL    string
	S3AccessKeyID     string
	S3SecretAccessKey string

	// Vertex AI / Gemini
	GCPProjectID          string
	GCPLocation           string
	GCPServiceAccountJSON string
	GeminiModelID         string

	// SMS Provider
	SMSProvider      string
	SMSAuthKey       string
	SMSSenderID      string
	SMSOTPTemplateID string

	// KYC Provider
	KYCProvider  string
	KYCAPIKey    string
	KYCAPISecret string
	KYCBaseURL   string

	// Payments
	GooglePlayServiceAccountJSON string
	GooglePlayPackageName        string
	AppleKeyID                   string
	AppleIssuerID                string
	AppleBundleID                string
	ApplePrivateKey              string

	// RevenueCat (server-side only — never ship these to a client)
	RevenueCatWebhookAuth   string // value RevenueCat sends in the webhook Authorization header
	RevenueCatSecretAPIKey  string // secret REST key (sk_...) used to re-read customers
	RevenueCatEntitlementID string // the single premium entitlement
	RevenueCatAPIBaseURL    string

	// FCM
	FCMServerKey string

	// Dev flags
	DevMockSMS    bool
	DevMockKYC    bool
	DevMockGemini bool
}

func Load() *Config {
	// Try loading .env from common working directories (repo root, backend/, or cwd)
	for _, path := range []string{".env", "../.env", "backend/.env", "../backend/.env"} {
		_ = godotenv.Load(path)
	}

	cfg := &Config{
		Port:           getEnv("PORT", "8080"),
		Env:            getEnv("ENV", "development"),
		APIBaseURL:     getEnv("API_BASE_URL", "http://localhost:8080/api/v1"),
		PublicWebURL:   getEnv("PUBLIC_WEB_URL", "https://cardflow.app"),
		AllowedOrigins: strings.Split(getEnv("ALLOWED_ORIGINS", "http://localhost:3000,http://127.0.0.1:3000,https://cardflow.app,cardflow://"), ","),

		DatabaseURL:       getEnv("DATABASE_URL", ""),
		DBHost:            getEnv("DB_HOST", "localhost"),
		DBPort:            getEnv("DB_PORT", "5432"),
		DBUser:            getEnv("DB_USER", "cardflow_app"),
		DBPassword:        getEnv("DB_PASSWORD", "cardflow_password"),
		DBName:            getEnv("DB_NAME", "cardflow_db"),
		DBSSLMode:         getEnv("DB_SSL_MODE", "disable"),
		DBMaxOpenConns:    getEnvInt("DB_MAX_OPEN_CONNS", 25),
		DBMaxIdleConns:    getEnvInt("DB_MAX_IDLE_CONNS", 10),
		DBConnMaxLifetime: getEnv("DB_CONN_MAX_LIFETIME", "5m"),

		RedisURL:      getEnv("REDIS_URL", ""),
		RedisHost:     getEnv("REDIS_HOST", "localhost"),
		RedisPort:     getEnv("REDIS_PORT", "6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("REDIS_DB", 0),

		JWTPrivateKey:       getEnv("JWT_PRIVATE_KEY", "cardflow-dev-secret-key-ed25519-placeholder-for-dev"),
		JWTPublicKey:        getEnv("JWT_PUBLIC_KEY", ""),
		JWTIssuer:           getEnv("JWT_ISSUER", "cardflow.app"),
		JWTAccessExpiryMin:  getEnvInt("JWT_ACCESS_EXPIRY_MINUTES", 15),
		JWTRefreshExpiryDay: getEnvInt("JWT_REFRESH_EXPIRY_DAYS", 30),

		DataEncryptionKey: getEnv("DATA_ENCRYPTION_KEY", "01234567890123456789012345678901"),

		S3Endpoint:        getEnv("S3_ENDPOINT", "http://localhost:9000"),
		S3Region:          getEnv("S3_REGION", "asia-south1"),
		S3PrivateBucket:   getEnv("S3_PRIVATE_BUCKET", "cardflow-private-media"),
		S3PublicBucket:    getEnv("S3_PUBLIC_BUCKET", "cardflow-public-media"),
		S3UseSSL:          getEnvBool("S3_USE_SSL", false),
		S3PublicCDNURL:    getEnv("S3_PUBLIC_CDN_URL", "https://cdn.cardflow.app"),
		S3AccessKeyID:     getEnv("S3_ACCESS_KEY_ID", "minioadmin"),
		S3SecretAccessKey: getEnv("S3_SECRET_ACCESS_KEY", "minioadmin"),

		GCPProjectID:          getEnv("GCP_PROJECT_ID", "cardflow-dev"),
		GCPLocation:           getEnv("GCP_LOCATION", "asia-south1"),
		GCPServiceAccountJSON: getEnv("GCP_SERVICE_ACCOUNT_JSON", ""),
		GeminiModelID:         getEnv("GEMINI_MODEL_ID", "gemini-3.0-flash-lite"),

		SMSProvider:      getEnv("SMS_PROVIDER", "mock"),
		SMSAuthKey:       getEnv("SMS_AUTH_KEY", ""),
		SMSSenderID:      getEnv("SMS_SENDER_ID", "CRDFLW"),
		SMSOTPTemplateID: getEnv("SMS_OTP_TEMPLATE_ID", ""),

		KYCProvider:  getEnv("KYC_PROVIDER", "mock"),
		KYCAPIKey:    getEnv("KYC_API_KEY", ""),
		KYCAPISecret: getEnv("KYC_API_SECRET", ""),
		KYCBaseURL:   getEnv("KYC_BASE_URL", "https://api.sandbox.co.in"),

		GooglePlayServiceAccountJSON: getEnv("GOOGLE_PLAY_SERVICE_ACCOUNT_JSON", ""),
		GooglePlayPackageName:        getEnv("GOOGLE_PLAY_PACKAGE_NAME", "com.cardflow.app"),
		AppleKeyID:                   getEnv("APPLE_KEY_ID", ""),
		AppleIssuerID:                getEnv("APPLE_ISSUER_ID", ""),
		AppleBundleID:                getEnv("APPLE_BUNDLE_ID", "com.cardflow.app"),
		ApplePrivateKey:              getEnv("APPLE_PRIVATE_KEY", ""),

		RevenueCatWebhookAuth:   getEnv("REVENUECAT_WEBHOOK_AUTH", ""),
		RevenueCatSecretAPIKey:  getEnv("REVENUECAT_SECRET_API_KEY", ""),
		RevenueCatEntitlementID: getEnv("REVENUECAT_ENTITLEMENT_ID", "premium"),
		RevenueCatAPIBaseURL:    getEnv("REVENUECAT_API_BASE_URL", "https://api.revenuecat.com"),

		FCMServerKey: getEnv("FCM_SERVER_KEY", ""),

		DevMockSMS:    getEnvBool("DEV_MOCK_SMS", true),
		DevMockKYC:    getEnvBool("DEV_MOCK_KYC", true),
		DevMockGemini: getEnvBool("DEV_MOCK_GEMINI", true),
	}

	return cfg
}

func (c *Config) IsProduction() bool {
	return strings.ToLower(c.Env) == "production"
}

// S3Enabled is false when MinIO/S3 is unset or still pointing at local dev.
func (c *Config) S3Enabled() bool {
	ep := strings.ToLower(strings.TrimSpace(c.S3Endpoint))
	if ep == "" {
		return false
	}
	if strings.Contains(ep, "localhost") || strings.Contains(ep, "127.0.0.1") || strings.Contains(ep, "[::1]") {
		return false
	}
	return true
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		if boolVal, err := strconv.ParseBool(val); err == nil {
			return boolVal
		}
	}
	return fallback
}

func SetupLogger(env string) {
	var handler slog.Handler
	if strings.ToLower(env) == "production" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	}
	slog.SetDefault(slog.New(handler))
}
