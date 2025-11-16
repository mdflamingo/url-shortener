package config

import (
	"flag"
	"os"
	"strings"
)

type Config struct {
	RunAddr     string
	BaseShortURL    string
	LogLevel        string
	FileStoragePath string
	DataBaseDSN string
}

func ParseFlags() *Config {
	cfg := &Config{}

	RunAddr := flag.String("a", ":8080", "address and port to run server")
	baseURL := flag.String("b", "http://localhost:8080", "base address before short url")
	logLevel := flag.String("l", "INFO", "log level")
	fileStoragePath := flag.String("f", "urls.csv", "urls file path")
	dataBaseDSN := flag.String("d", "", "connect to postgres")

	flag.Parse()

	cfg.RunAddr = getEnvOrDefault("SERVER_ADDRESS", *RunAddr)
	cfg.BaseShortURL = getEnvOrDefault("BASE_URL", *baseURL)
	cfg.LogLevel = strings.ToUpper(getEnvOrDefault("LOG_LEVEL", *logLevel))
	cfg.FileStoragePath = getEnvOrDefault("FILE_STORAGE_PATH", *fileStoragePath)
	cfg.DataBaseDSN = getEnvOrDefault("DATABASE_DSN", *dataBaseDSN)

	return cfg
}

func getEnvOrDefault(envName, defaultValue string) string {
	if envValue := os.Getenv(envName); envValue != "" {
		return envValue
	}
	return defaultValue
}
