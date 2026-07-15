package config

import "os"

type Config struct {
	Host     string
	Port     string
	LogLevel string
}

func Load() Config {
	return Config{
		Host:     valueOrDefault("SERVER_HOST", "0.0.0.0"),
		Port:     valueOrDefault("SERVER_PORT", "8080"),
		LogLevel: valueOrDefault("LOG_LEVEL", "info"),
	}
}

func (c Config) Address() string { return c.Host + ":" + c.Port }

func valueOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
