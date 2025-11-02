package config

import (
	"flag"
	"os"
	"strings"
)

type Config struct {
	FlagRunAddr  string
	BaseShortURL string
	LogLevel     string
}

func ParseFlags() *Config {
	cfg := &Config{}

	flagRunAddr := flag.String("a", ":8080", "address and port to run server")
	baseURL := flag.String("b", "http://localhost:8080", "base address before short url")
	logLevel := flag.String("l", "INFO", "log level")

	flag.Parse()

	cfg.FlagRunAddr = getEnvOrDefault("SERVER_ADDRESS", *flagRunAddr)
	cfg.BaseShortURL = getEnvOrDefault("BASE_URL", *baseURL)
	cfg.LogLevel = strings.ToUpper(getEnvOrDefault("LOG_LEVEL", *logLevel))

	return cfg
}

func getEnvOrDefault(envName, defaultValue string) string {
	if envValue := os.Getenv(envName); envValue != "" {
		return envValue
	}
	return defaultValue
}
