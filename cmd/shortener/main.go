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
	"context"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/mdflamingo/url-shortener/internal/config"
	"github.com/mdflamingo/url-shortener/internal/logger"
	"github.com/mdflamingo/url-shortener/internal/middleware"
	"github.com/mdflamingo/url-shortener/internal/repository"
	"github.com/mdflamingo/url-shortener/internal/router"
	"github.com/mdflamingo/url-shortener/internal/service"

	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// Глобальные переменные сборки (заполняются при компиляции через ldflags или используются дефолтные значения)
var (
	buildVersion string
	buildDate    string
	buildCommit  string
)

func main() {
	printBuildInfo()

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
	logger.Log.Info("Server shutdown gracefully")
}

// run инициализирует и запускает HTTP/HTTPS-сервер (HTTP ИЛИ HTTPS) с graceful shutdown
func run(conf *config.Config) error {
	if conf.CookieSecretKey == "" {
		logger.Log.Fatal("CookieSecretKey is required")
	}

	if err := logger.InitLogger(conf.LogLevel); err != nil {
		return err
	}

	auditService, err := initAuditService(conf)
	if err != nil {
		return fmt.Errorf("failed to initialize audit service: %w", err)
	}

	logger.Log.Info("Configuration loaded",
		zap.String("address", conf.RunAddr),
		zap.String("base_url", conf.BaseShortURL))

	var errStorage error
	storage, errStorage := initStorage(conf)
	if errStorage != nil {
		logger.Log.Fatal("Failed to create storage", zap.Error(errStorage))
	}

	cookieMiddleware := middleware.NewSignedCookieMiddleware(conf.CookieSecretKey)
	r := router.NewRouter(conf, storage, cookieMiddleware, auditService)

	// Cервер с базовыми настройками таймаутов
	server := &http.Server{
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	idleConnsClosed := GracefulShutdown(server, storage)

	if conf.EnabledHTTPS {
		certificate, privateKey := readKeys()
		server.Addr = ":443"
		server.Handler = r

		logger.Log.Info("Starting HTTPS server", zap.String("addr", server.Addr))
		if err := server.ListenAndServeTLS(certificate, privateKey); err != http.ErrServerClosed {
			return fmt.Errorf("HTTPS server ListenAndServeTLS: %w", err)
		}
	} else {
		server.Addr = conf.RunAddr
		server.Handler = r

		logger.Log.Info("Starting HTTP server", zap.String("addr", server.Addr))
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			return fmt.Errorf("HTTP server ListenAndServe: %w", err)
		}
	}

	<-idleConnsClosed
	return nil
}

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
			logger.Log.Fatal("Failed to initialize database storage", zap.Error(err))
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
			} else {
				logger.Log.Warn("Failed to initialize file storage", zap.Error(err))
			}
		}
	}

	logger.Log.Info("Using in-memory storage")
	return repository.NewMemoryStorage(), nil
}

// initAuditService инициализирует сервис аудита с наблюдателями
func initAuditService(conf *config.Config) (*service.AuditService, error) {
	auditService := service.NewAuditService()

	// Файловый аудит
	if conf.AuditFile != "" {
		fileObs, err := service.NewFileObserver(conf.AuditFile)
		if err != nil {
			logger.Log.Warn("Failed to init file audit observer",
				zap.String("path", conf.AuditFile),
				zap.Error(err))
		} else {
			auditService.Attach(fileObs)
			logger.Log.Info("Audit file observer attached", zap.String("path", conf.AuditFile))
		}
	} else {
		logger.Log.Info("Audit file disabled (no path provided)")
	}

	// HTTP аудит
	if conf.AuditURL != "" {
		httpObs, err := service.NewAPIObserver(conf.AuditURL)
		if err != nil {
			logger.Log.Warn("Failed to init HTTP audit observer",
				zap.String("url", conf.AuditURL),
				zap.Error(err))
		} else {
			auditService.Attach(httpObs)
			logger.Log.Info("Audit HTTP observer attached", zap.String("url", conf.AuditURL))
		}
	} else {
		logger.Log.Info("Audit HTTP disabled (no URL provided)")
	}
	return auditService, nil
}

func printBuildInfo() {
	fmt.Printf("Build version: %s\n", getOrDefault(buildVersion, "dev"))
	fmt.Printf("Build date: %s\n", getOrDefault(buildDate, "unknown"))
	fmt.Printf("Build commit: %s\n", getOrDefault(buildCommit, "none"))
	fmt.Println("---")
}

// getOrDefault возвращает значение или значение по умолчанию
func getOrDefault(value, defaultValue string) string {
	if value != "" {
		return value
	}
	return defaultValue
}

// readKeys Загружает сертификат и приватный ключ из файлов ~/cert.pem и ~/private.pem
func readKeys() (string, string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		logger.Log.Fatal("cannot get user home directory", zap.Error(err))
	}

	certPath := filepath.Join(homeDir, "cert.pem")
	keyPath := filepath.Join(homeDir, "private.pem")

	certificateBytes, err := os.ReadFile(certPath)
	if err != nil {
		logger.Log.Fatal("cannot read certificate file",
			zap.String("path", certPath),
			zap.Error(err))
	}

	privateKeyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		logger.Log.Fatal("cannot read private key file",
			zap.String("path", keyPath),
			zap.Error(err))
	}

	return string(certificateBytes), string(privateKeyBytes)
}

// GracefulShutdown настраивает обработку сигналов и возвращает канал для ожидания
func GracefulShutdown(server *http.Server, storage repository.URLStorage) <-chan struct{} {
	idleConnsClosed := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)

	go func() {
		<-sigCh
		logger.Log.Info("Received shutdown signal, starting graceful shutdown...")

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		// Graceful shutdown сервера
		if server != nil {
			if err := server.Shutdown(ctx); err != nil {
				logger.Log.Error("HTTP server shutdown error", zap.Error(err))
			} else {
				logger.Log.Info("HTTP server shutdown gracefully")
			}
		}

		// Сохранение данных
		if storage != nil {
			if err := storage.Close(); err != nil {
				logger.Log.Error("Storage close error", zap.Error(err))
			} else {
				logger.Log.Info("Storage closed successfully - all data saved")
			}
		}

		logger.Log.Info("All resources closed")
		close(idleConnsClosed)
	}()

	return idleConnsClosed
}
