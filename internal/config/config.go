package config

import (
	"flag"
)

type Config struct {
	FlagRunAddr  string
	BaseShortURL string
}

func ParseFlags() *Config {
	cfg := &Config{}
	flag.StringVar(&cfg.FlagRunAddr, "a", ":8080", "address and port to run server")
	flag.StringVar(&cfg.BaseShortURL, "b", "http://localhost:8080", "base address before short url")
	flag.Parse()

	return cfg
}
