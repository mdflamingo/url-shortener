package main

import (
	"github.com/mdflamingo/url-shortener/internal/config"
	"github.com/mdflamingo/url-shortener/internal/handler"
	"github.com/mdflamingo/url-shortener/internal/logger"
	"github.com/mdflamingo/url-shortener/internal/repository"
	"go.uber.org/zap"

	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func main() {
	conf := config.ParseFlags()
	if err := run(conf); err != nil {
		log.Fatal(err)
	}
}

func run(conf *config.Config) error {
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

	r := chi.NewRouter()

	r.Use(logger.RequestLogger)
	r.Use(gzipMiddleware)

	r.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
		handler.DBHealthCheck(w, r, conf.DataBaseDSN)
	})
	r.Get("/{id}", func(w http.ResponseWriter, req *http.Request) {
		handler.GetHandler(w, req, storage)
	})
	r.Post("/", func(w http.ResponseWriter, req *http.Request) {
		handler.PostHandler(w, req, conf.BaseShortURL, storage)
	})
	r.Post("/api/shorten", func(w http.ResponseWriter, req *http.Request) {
		handler.JSONPostHandler(w, req, conf.BaseShortURL, storage)
	})
	r.Post("/api/shorten/batch", func(w http.ResponseWriter, req *http.Request) {
		handler.BatchHandler(w, req, conf.BaseShortURL, storage)
	})

	return http.ListenAndServe(conf.RunAddr, r)
}

func initStorage(conf *config.Config) (repository.URLStorage, error) {
	if conf.DataBaseDSN != "" {
		logger.Log.Info("Attempting to use database storage", zap.String("dsn", conf.DataBaseDSN))
		dbStorage, err := repository.NewDBStorage(conf.DataBaseDSN)
		if err != nil {
			logger.Log.Error("Failed to initialize database storage, falling back to next option", zap.Error(err))
		} else {
			logger.Log.Info("Successfully initialized database storage")
			return dbStorage, nil
		}
	}

	if conf.FileStoragePath != "" {
		logger.Log.Info("Attempting to use file storage", zap.String("path", conf.FileStoragePath))
		fileStorage, err := repository.NewFileStorage(conf.FileStoragePath)
		if err != nil {
			logger.Log.Error("Failed to initialize file storage, falling back to memory", zap.Error(err))
		} else {
			logger.Log.Info("Successfully initialized file storage")
			return fileStorage, nil
		}
	}

	logger.Log.Info("Using in-memory storage")
	return repository.NewMemoryStorage(), nil
}
