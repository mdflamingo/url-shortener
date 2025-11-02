package config

import (
	"flag"
	"os"
)

type Config struct {
	FlagRunAddr  string
	BaseShortURL string
	LogLevel string
}

func ParseFlags() *Config {
	cfg := &Config{}
	flagRunAddr := flag.String("a", ":8080", "address and port to run server")
	baseURL := flag.String("b", "http://localhost:8080", "base address before short url")
	logLevel := flag.String("l", "INFO", "log level")
	flag.Parse()

	if envRunAddr := os.Getenv("SERVER_ADDRESS"); envRunAddr != "" {
		cfg.FlagRunAddr = envRunAddr
	} else {
		cfg.FlagRunAddr = *flagRunAddr
	}
	if envBaseURL := os.Getenv("BASE_URL"); envBaseURL != "" {
		cfg.BaseShortURL = envBaseURL
	} else {
		cfg.BaseShortURL = *baseURL
	}
	if envLogLevel := os.Getenv("LOG_LEVEL"); envLogLevel != "" {
		cfg.LogLevel = envLogLevel
	} else {
		cfg.LogLevel = *logLevel
	}

	return cfg
}
