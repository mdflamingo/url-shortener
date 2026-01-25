package main

import (
	"log"
	"net/http"

	"github.com/mdflamingo/url-shortener/internal/config"
	"github.com/mdflamingo/url-shortener/internal/logger"
	"github.com/mdflamingo/url-shortener/internal/middleware"
	"github.com/mdflamingo/url-shortener/internal/repository"
	"github.com/mdflamingo/url-shortener/internal/router"
	"go.uber.org/zap"

	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	conf := config.ParseFlags()
	if err := run(conf); err != nil {
		log.Fatal(err)
	}
}

func run(conf *config.Config) error {
	if conf.CookieSecretKey == "" {
		logger.Log.Fatal("CookieSecretKey is required")
	}
	if err := logger.Initialize(conf.LogLevel); err != nil {
		return err
	}

	logger.Log.Info("Running server", zap.String("address", conf.RunAddr))
	logger.Log.Info("Base short URL", zap.String("url", conf.BaseShortURL))

	storage, err := initStorage(conf)
	if err != nil {
		logger.Log.Fatal("Failed to create storage", zap.Error(err))
	}
	defer storage.Close()

	cookieMiddleware := middleware.NewSignedCookieMiddleware(conf.CookieSecretKey)
	r := router.NewRouter(conf, storage, cookieMiddleware)

	return http.ListenAndServe(conf.RunAddr, r)
}

func initStorage(conf *config.Config) (repository.URLStorage, error) {
	if conf.DataBaseDSN != "" {
		logger.Log.Info("Attempting to use database storage", zap.String("dsn", conf.DataBaseDSN))
		if storage, err := repository.NewDBStorage(conf.DataBaseDSN); err == nil {
			logger.Log.Info("Successfully initialized database storage")
			return storage, nil
		}
		logger.Log.Warn("Failed to initialize database storage, trying file storage")
	}

	if conf.FileStoragePath != "" {
		logger.Log.Info("Attempting to use file storage", zap.String("path", conf.FileStoragePath))
		if storage, err := repository.NewFileStorage(conf.FileStoragePath); err == nil {
			logger.Log.Info("Successfully initialized file storage")
			return storage, nil
		}
		logger.Log.Warn("Failed to initialize file storage, using in-memory storage")
	}

	logger.Log.Info("Using in-memory storage")
	return repository.NewMemoryStorage(), nil
}
