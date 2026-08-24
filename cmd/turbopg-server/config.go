package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	insecureAPIKey    = "testapikey"
	defaultListenHost = "127.0.0.1"
	defaultListenPort = "8080"
	defaultMaxBody    = 32 << 20
	defaultShutdown   = 30 * time.Second
	defaultDBMaxOpen  = 25
	defaultDBMaxIdle  = 5
	defaultConnLife   = time.Hour
	defaultHeaderTO   = 10 * time.Second
	defaultReadTO     = 60 * time.Second
	defaultWriteTO    = 120 * time.Second
	defaultIdleTO     = 90 * time.Second
)

// ServerConfig holds the configuration for the server.
type ServerConfig struct {
	DatabaseURL         string
	APIKey              string
	AllowInsecureAPIKey bool
	Listen              string
	StorePrefix         string
	EmbeddingBaseURL    string
	EmbeddingAPIKey     string
	DBMaxOpen           int
	DBMaxIdle           int
	DBConnLifetime      time.Duration
	ReadHeaderTimeout   time.Duration
	ReadTimeout         time.Duration
	WriteTimeout        time.Duration
	IdleTimeout         time.Duration
	MaxBodyBytes        int64
	StatementTimeout    time.Duration
	IVFFlatLists        int
	IVFFlatProbes       int
	PatchByFilterMax    int
	DeleteByFilterMax   int
	LogLevel            string
	ShutdownTimeout     time.Duration
	TLSCertFile         string
	TLSKeyFile          string
	PprofListen         string
}

func loadConfig() (ServerConfig, error) {
	port := getEnv("TURBOPG_PORT", defaultListenPort)
	listen := strings.TrimSpace(os.Getenv("TURBOPG_LISTEN"))
	if listen == "" {
		listen = net.JoinHostPort(defaultListenHost, port)
	}

	cfg := ServerConfig{
		DatabaseURL:         getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"),
		APIKey:              os.Getenv("TURBOPG_API_KEY"),
		AllowInsecureAPIKey: envBool("TURBOPG_ALLOW_INSECURE_API_KEY"),
		Listen:              listen,
		StorePrefix:         getEnv("TURBOPG_STORE_PREFIX", "tpga_"),
		EmbeddingBaseURL:    firstEnv("EMBEDDING_BASE_URL", "OPENAI_BASE_URL", "OPENROUTER_BASE_URL"),
		EmbeddingAPIKey:     firstEnv("EMBEDDING_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY"),
		DBMaxOpen:           envInt("TURBOPG_DB_MAX_OPEN", defaultDBMaxOpen),
		DBMaxIdle:           envInt("TURBOPG_DB_MAX_IDLE", defaultDBMaxIdle),
		DBConnLifetime:      envDuration("TURBOPG_DB_CONN_LIFETIME", defaultConnLife),
		ReadHeaderTimeout:   envDuration("TURBOPG_READ_HEADER_TIMEOUT", defaultHeaderTO),
		ReadTimeout:         envDuration("TURBOPG_READ_TIMEOUT", defaultReadTO),
		WriteTimeout:        envDuration("TURBOPG_WRITE_TIMEOUT", defaultWriteTO),
		IdleTimeout:         envDuration("TURBOPG_IDLE_TIMEOUT", defaultIdleTO),
		MaxBodyBytes:        int64(envInt("TURBOPG_MAX_BODY_BYTES", defaultMaxBody)),
		StatementTimeout:    envDuration("TURBOPG_STATEMENT_TIMEOUT", 0),
		IVFFlatLists:        envInt("TURBOPG_IVFFLAT_LISTS", 0),
		IVFFlatProbes:       envInt("TURBOPG_IVFFLAT_PROBES", 0),
		PatchByFilterMax:    envInt("TURBOPG_PATCH_BY_FILTER_MAX", 0),
		DeleteByFilterMax:   envInt("TURBOPG_DELETE_BY_FILTER_MAX", 0),
		LogLevel:            strings.ToLower(getEnv("TURBOPG_LOG_LEVEL", "info")),
		ShutdownTimeout:     envDuration("TURBOPG_SHUTDOWN_TIMEOUT", defaultShutdown),
		TLSCertFile:         os.Getenv("TURBOPG_TLS_CERT"),
		TLSKeyFile:          os.Getenv("TURBOPG_TLS_KEY"),
		PprofListen:         os.Getenv("TURBOPG_PPROF_LISTEN"),
	}
	if cfg.IVFFlatLists <= 0 {
		cfg.IVFFlatLists = 100
	}
	if err := cfg.validateAPIKey(); err != nil {
		return ServerConfig{}, err
	}
	if (cfg.TLSCertFile == "") != (cfg.TLSKeyFile == "") {
		return ServerConfig{}, fmt.Errorf("TURBOPG_TLS_CERT and TURBOPG_TLS_KEY must be set together")
	}
	return cfg, nil
}

func (c ServerConfig) validateAPIKey() error {
	if c.APIKey != "" && c.APIKey != insecureAPIKey {
		return nil
	}
	if c.AllowInsecureAPIKey {
		return nil
	}
	return fmt.Errorf("TURBOPG_API_KEY must be set to a non-default value (or set TURBOPG_ALLOW_INSECURE_API_KEY=1 for local use)")
}

func (c *ServerConfig) effectiveAPIKey() string {
	if c.APIKey == "" {
		return insecureAPIKey
	}
	return c.APIKey
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value, exists := os.LookupEnv(key); exists && value != "" {
			return value
		}
	}
	return ""
}

func envBool(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes"
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

func envDuration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return d
	}
	if n, err := strconv.Atoi(raw); err == nil {
		return time.Duration(n) * time.Millisecond
	}
	return fallback
}

func withStatementTimeout(dsn string, timeout time.Duration) string {
	if timeout <= 0 {
		return dsn
	}
	ms := timeout.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	opt := fmt.Sprintf("-c statement_timeout=%d", ms)
	if strings.Contains(dsn, "?") {
		return dsn + "&options=" + strings.ReplaceAll(opt, " ", "+")
	}
	return dsn + "?options=" + strings.ReplaceAll(opt, " ", "+")
}
