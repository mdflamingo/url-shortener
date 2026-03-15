// Package main - точка входа в приложение URL Shortener
//
// Приложение предоставляет сервис для сокращения URL-адресов с поддержкой:
// - Хранения в памяти, файле или PostgreSQL
// - Аудита действий через файл или HTTP
// - Cookie-аутентификации
// - Пакетного создания коротких ссылок
// - Удаления ссылок
package main

import (
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"

	"go.uber.org/zap"

	"github.com/mdflamingo/url-shortener/internal/config"
	"github.com/mdflamingo/url-shortener/internal/logger"
	"github.com/mdflamingo/url-shortener/internal/middleware"
	"github.com/mdflamingo/url-shortener/internal/repository"
	"github.com/mdflamingo/url-shortener/internal/router"
	"github.com/mdflamingo/url-shortener/internal/service"

	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	if os.Getenv("ENABLE_PPROF") == "true" {
		go func() {
			pprofServer := &http.Server{
				Addr:    "localhost:6060",
				Handler: http.DefaultServeMux,
			}
			log.Println("Starting pprof server on :6060")
			if err := pprofServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("Pprof server error: %v", err)
			}
		}()
	}
	conf := config.ParseFlags()
	if err := run(conf); err != nil {
		log.Fatal(err)
	}
}

// run инициализирует и запускает HTTP-сервер
//
// Параметры:
//   - conf: конфигурация приложения
//
// Возвращает:
//   - error: ошибка при запуске сервера
//
// Функция выполняет:
//   - Проверку обязательных параметров
//   - Инициализацию логгера
//   - Настройку сервиса аудита
//   - Инициализацию хранилища
//   - Настройку маршрутизатора
//   - Запуск HTTP-сервера
func run(conf *config.Config) error {
	if conf.CookieSecretKey == "" {
		logger.Log.Fatal("CookieSecretKey is required")
	}
	if err := logger.Initialize(conf.LogLevel); err != nil {
		return err
	}

	auditService := service.NewAuditService()
	fileObs, err := service.NewFileObserver(conf.AuditFile)
	if err != nil {
		logger.Log.Warn("Failed to init file audit observer", zap.Error(err))
	} else if fileObs != nil {
		auditService.Attach(fileObs)
		logger.Log.Info("Audit file enabled", zap.String("path", conf.AuditFile))
	}

	httpObs, _ := service.NewAPIObserver(conf.AuditURL)
	if httpObs != nil {
		auditService.Attach(httpObs)
		logger.Log.Info("Audit HTTP enabled", zap.String("url", conf.AuditURL))
	}

	logger.Log.Info("Running server", zap.String("address", conf.RunAddr))
	logger.Log.Info("Base short URL", zap.String("url", conf.BaseShortURL))

	storage, err := initStorage(conf)
	if err != nil {
		logger.Log.Fatal("Failed to create storage", zap.Error(err))
	}
	defer storage.Close()

	cookieMiddleware := middleware.NewSignedCookieMiddleware(conf.CookieSecretKey)
	r := router.NewRouter(conf, storage, cookieMiddleware, auditService)

	return http.ListenAndServe(conf.RunAddr, r)
}

// initStorage инициализирует хранилище URL в зависимости от конфигурации
//
// Параметры:
//   - conf: конфигурация приложения
//
// Возвращает:
//   - repository.URLStorage: инициализированное хранилище
//   - error: ошибка при инициализации
//
// Приоритет выбора хранилища:
//  1. PostgreSQL (если указан DataBaseDSN)
//  2. Файловое хранилище (если указан FileStoragePath)
//  3. In-memory хранилище (по умолчанию)
//
// initStorage инициализирует хранилище URL в зависимости от конфигурации
func initStorage(conf *config.Config) (repository.URLStorage, error) {
	logger.Log.Info("Initializing storage",
		zap.String("database_dsn", conf.DataBaseDSN),
		zap.String("file_storage_path", conf.FileStoragePath))

	if conf.DataBaseDSN != "" {
		logger.Log.Info("Attempting to use database storage", zap.String("dsn", conf.DataBaseDSN))
		if storage, err := repository.NewDBStorage(conf.DataBaseDSN); err == nil {
			logger.Log.Info("Successfully initialized database storage")
			return storage, nil
		} else {
			logger.Log.Warn("Failed to initialize database storage", zap.Error(err))
		}
	}

	if conf.FileStoragePath != "" {
		logger.Log.Info("Attempting to use file storage", zap.String("path", conf.FileStoragePath))

		file, err := os.OpenFile(conf.FileStoragePath, os.O_RDWR|os.O_CREATE, 0666)
		if err != nil {
			logger.Log.Warn("Cannot access file storage", zap.Error(err))
		} else {
			file.Close()
			if storage, err := repository.NewFileStorage(conf.FileStoragePath); err == nil {
				logger.Log.Info("Successfully initialized file storage")
				return storage, nil
			}
		}
		logger.Log.Warn("Failed to initialize file storage", zap.Error(err))
	}

	logger.Log.Info("Using in-memory storage")
	return repository.NewMemoryStorage(), nil
}
