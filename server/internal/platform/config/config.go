package config

import (
	"os"
	"strings"
)

type Config struct {
	Host               string
	Port               string
	LogLevel           string
	DatabaseURL        string
	TrustedProxyCIDRs  []string
	TrustedProxyHeader string
}

func Load() Config {
	return Config{
		Host:               valueOrDefault("SERVER_HOST", "0.0.0.0"),
		Port:               valueOrDefault("SERVER_PORT", "8080"),
		LogLevel:           valueOrDefault("LOG_LEVEL", "info"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		TrustedProxyCIDRs:  splitCommaSeparated(os.Getenv("TRUSTED_PROXY_CIDRS")),
		TrustedProxyHeader: os.Getenv("TRUSTED_PROXY_HEADER"),
	}
}

func splitCommaSeparated(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func (c Config) Address() string { return c.Host + ":" + c.Port }

func valueOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
