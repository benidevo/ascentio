package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Settings holds the configuration settings for the application.
type Settings struct {
	AppName            string
	ServerPort         string
	DBConnectionString string
	DBDriver           string
	LogLevel           string
	IsDevelopment      bool
	IsTest             bool
	MigrationsDir      string

	DBMaxOpenConns    int
	DBMaxIdleConns    int
	DBConnMaxLifetime time.Duration

	TokenSecret        string
	AccessTokenExpiry  time.Duration
	RefreshTokenExpiry time.Duration
	CookieDomain       string
	CookieSecure       bool
	CookieSameSite     string

	GoogleOAuthEnabled      bool
	GoogleClientID          string
	GoogleClientSecret      string
	GoogleClientRedirectURL string
	GoogleAuthUserInfoURL   string
	GoogleAuthUserInfoScope string

	CORSAllowedOrigins   []string
	CORSAllowCredentials bool

	CreateAdminUser    bool
	AdminUsername      string
	AdminPassword      string
	AdminEmail         string
	ResetAdminPassword bool

	SecurityPageEnabled bool

	AIProvider             string
	GeminiAPIKey           string
	GeminiModel            string // Default model (deprecated, use specific models)
	GeminiModelCVParsing   string // Fast model for CV parsing
	GeminiModelJobAnalysis string // Advanced model for job analysis
	GeminiModelCoverLetter string // Advanced model for cover letter generation
}

// NewSettings initializes and returns a Settings struct with default values
// populated from environment variables. If an environment variable is
// not set, a predefined default value is used.
func NewSettings() Settings {
	isDevelopment := getEnv("IS_DEVELOPMENT", "false") == "true"
	isTest := getEnv("GO_ENV", "") == "test"

	// Production-optimized defaults
	accessTokenExpiry := 60 * time.Minute // 1 hour
	refreshTokenExpiry := 168 * time.Hour // 7 days
	dbMaxOpenConns := 25
	dbMaxIdleConns := 5
	dbConnMaxLifetime := 5 * time.Minute

	if envVal := getEnv("ACCESS_TOKEN_EXPIRY", ""); envVal != "" {
		if mins, err := strconv.Atoi(envVal); err == nil {
			accessTokenExpiry = time.Duration(mins) * time.Minute
		}
	}

	if envVal := getEnv("REFRESH_TOKEN_EXPIRY", ""); envVal != "" {
		if hours, err := strconv.Atoi(envVal); err == nil {
			refreshTokenExpiry = time.Duration(hours) * time.Hour
		}
	}

	// Database connection string with sensible production default
	dbConnectionString := "/app/data/vega.db?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON&_cache_size=10000&_synchronous=NORMAL"
	if envDB := getEnv("DB_CONNECTION_STRING", ""); envDB != "" {
		dbConnectionString = envDB
	}
	if isTest && getEnv("DB_CONNECTION_STRING", "") == "" {
		dbConnectionString = ":memory:"
	}

	cookieSecure := !isDevelopment
	if envSecure := getEnv("COOKIE_SECURE", ""); envSecure != "" {
		cookieSecure = envSecure == "true"
	}

	var corsOrigins []string
	if envCORS := getEnv("CORS_ALLOWED_ORIGINS", ""); envCORS != "" {
		corsOrigins = strings.Split(envCORS, ",")
		for i, origin := range corsOrigins {
			corsOrigins[i] = strings.TrimSpace(origin)
		}
	} else {
		corsOrigins = []string{"*"}
	}

	return Settings{
		AppName:            "vega",
		ServerPort:         ":8765",
		DBConnectionString: dbConnectionString,
		DBDriver:           "sqlite",
		LogLevel:           getEnv("LOG_LEVEL", getDefaultLogLevel(isDevelopment)),
		IsDevelopment:      isDevelopment,
		TokenSecret:        getEnv("TOKEN_SECRET", "default-secret-key"),
		IsTest:             isTest,
		MigrationsDir:      "migrations/sqlite",

		DBMaxOpenConns:    dbMaxOpenConns,
		DBMaxIdleConns:    dbMaxIdleConns,
		DBConnMaxLifetime: dbConnMaxLifetime,

		AccessTokenExpiry:  accessTokenExpiry,
		RefreshTokenExpiry: refreshTokenExpiry,
		CookieDomain:       getEnv("COOKIE_DOMAIN", ""),
		CookieSecure:       cookieSecure,
		CookieSameSite:     "lax",

		GoogleOAuthEnabled:      getEnv("GOOGLE_OAUTH_ENABLED", "false") == "true",
		GoogleClientID:          getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret:      getEnv("GOOGLE_CLIENT_SECRET", ""),
		GoogleClientRedirectURL: getEnv("GOOGLE_CLIENT_REDIRECT_URL", "http://localhost:8765/auth/google/callback"),
		GoogleAuthUserInfoURL:   "https://www.googleapis.com/oauth2/v3/userinfo",
		GoogleAuthUserInfoScope: "https://www.googleapis.com/auth/userinfo.email",

		CORSAllowedOrigins:   corsOrigins,
		CORSAllowCredentials: false,

		CreateAdminUser:    getEnv("CREATE_ADMIN_USER", "false") == "true",
		AdminUsername:      getEnv("ADMIN_USERNAME", ""),
		AdminPassword:      getEnv("ADMIN_PASSWORD", ""),
		ResetAdminPassword: getEnv("RESET_ADMIN_PASSWORD", "false") == "true",
		AdminEmail:         getEnv("ADMIN_EMAIL", ""),

		SecurityPageEnabled: getEnv("SECURITY_PAGE_ENABLED", "false") == "true",

		AIProvider:             "gemini",
		GeminiAPIKey:           getEnv("GEMINI_API_KEY", ""),
		GeminiModel:            getEnv("GEMINI_MODEL", "gemini-2.5-flash"), // Default/fallback model
		GeminiModelCVParsing:   getEnv("GEMINI_MODEL_CV_PARSING", "gemini-1.5-flash"),
		GeminiModelJobAnalysis: getEnv("GEMINI_MODEL_JOB_ANALYSIS", "gemini-2.5-flash"),
		GeminiModelCoverLetter: getEnv("GEMINI_MODEL_COVER_LETTER", "gemini-2.5-flash"),
	}
}

// NewTestSettings creates settings optimized for testing with in-memory database
func NewTestSettings() Settings {
	settings := NewSettings()
	settings.IsTest = true
	settings.DBConnectionString = ":memory:"
	settings.ServerPort = ":0" // Use random available port
	return settings
}

// NewTestSettingsWithTempDB creates settings for testing with a temporary file database
// This is useful for tests that need persistence or migration testing
// The caller is responsible for cleaning up the returned temp file path
func NewTestSettingsWithTempDB() (Settings, string) {
	tempDir := os.TempDir()
	tempFile := filepath.Join(tempDir, fmt.Sprintf("vega_test_%d.db", time.Now().UnixNano()))

	settings := NewSettings()
	settings.IsTest = true
	settings.DBConnectionString = tempFile + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON"
	settings.ServerPort = ":0"

	return settings, tempFile
}

func getEnv(key string, defaultValue string) (value string) {
	value = os.Getenv(key)
	if value == "" {
		value = defaultValue
	}
	return
}

// getDefaultLogLevel returns appropriate log level based on environment
func getDefaultLogLevel(isDevelopment bool) string {
	if isDevelopment {
		return "debug"
	}
	return "info"
}
