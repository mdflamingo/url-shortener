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
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/mdflamingo/url-shortener/api/url_shortener"
	"github.com/mdflamingo/url-shortener/internal/config"
	shortenerGRPC "github.com/mdflamingo/url-shortener/internal/grpc"
	"github.com/mdflamingo/url-shortener/internal/interceptor"
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

// run инициализирует и запускает HTTP/HTTPS-сервер и gRPC-сервер с graceful shutdown
func run(conf *config.Config) error {
	if conf.CookieSecretKey == "" {
		logger.Log.Fatal("CookieSecretKey is required")
	}

	if err := logger.InitLogger(conf.LogLevel); err != nil {
		return err
	}

	// Инициализация зависимостей
	auditService, err := initAuditService(conf)
	if err != nil {
		return fmt.Errorf("failed to initialize audit service: %w", err)
	}

	var errStorage error
	storage, errStorage := initStorage(conf)
	if errStorage != nil {
		logger.Log.Fatal("Failed to create storage", zap.Error(errStorage))
	}

	urlService := service.NewURLService(storage, conf.BaseShortURL, auditService, logger.Log)
	cookieMiddleware := middleware.NewSignedCookieMiddleware(conf.CookieSecretKey)
	r := router.NewRouter(conf, urlService, cookieMiddleware)

	// Создается ServerGroup
	serverGroup := NewServerGroup(logger.Log, storage)

	// Контекст с таймаутом для shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Запуск серверов
	if err := serverGroup.StartHTTP(conf, r); err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	if err := serverGroup.StartGRPC(shutdownCtx, conf, urlService); err != nil {
		return fmt.Errorf("failed to start gRPC server: %w", err)
	}

	// Ожидание сигнала завершения
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)

	select {
	case sig := <-sigCh:
		logger.Log.Info("Received shutdown signal", zap.String("signal", fmt.Sprintf("%+v", sig)))
	case <-shutdownCtx.Done():
		logger.Log.Warn("Shutdown timeout reached")
	}

	// Graceful shutdown
	return serverGroup.Shutdown(shutdownCtx)
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

// ServerGroup управляет группой серверов с graceful shutdown
type ServerGroup struct {
	httpServer   *http.Server
	grpcServer   *grpc.Server
	grpcListener net.Listener
	logger       *zap.Logger
	storage      repository.URLStorage
}

// NewServerGroup создает группу серверов
func NewServerGroup(logger *zap.Logger, storage repository.URLStorage) *ServerGroup {
	return &ServerGroup{
		logger:  logger,
		storage: storage,
	}
}

// StartHTTP запускает HTTP/HTTPS сервер
func (sg *ServerGroup) StartHTTP(conf *config.Config, r http.Handler) error {
	sg.httpServer = &http.Server{
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	if conf.EnabledHTTPS {
		certificate, privateKey := readKeys()
		sg.httpServer.Addr = ":443"
		sg.httpServer.TLSConfig = &tls.Config{}
		sg.logger.Info("Starting HTTPS server", zap.String("addr", sg.httpServer.Addr))
		go sg.httpServer.ListenAndServeTLS(certificate, privateKey)
	} else {
		sg.httpServer.Addr = conf.RunAddr
		sg.httpServer.Handler = r
		sg.logger.Info("Starting HTTP server", zap.String("addr", sg.httpServer.Addr))
		go sg.httpServer.ListenAndServe()
	}

	return nil
}

// StartGRPC запускает gRPC сервер
func (sg *ServerGroup) StartGRPC(ctx context.Context, conf *config.Config, urlService *service.URLService) error {
	if conf.GRPCAddr == "" {
		return nil
	}

	lis, err := net.Listen("tcp", conf.GRPCAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", conf.GRPCAddr, err)
	}

	sg.grpcListener = lis
	authInterceptor := interceptor.NewAuthInterceptor(conf.CookieSecretKey)

	unaryInterceptor := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return authInterceptor.UnaryInterceptor(ctx, req, info, handler)
	}

	sg.grpcServer = grpc.NewServer(
		grpc.UnaryInterceptor(unaryInterceptor),
	)

	url_shortener.RegisterShortenerServiceServer(sg.grpcServer, shortenerGRPC.NewShortenerServer(urlService, sg.logger))

	sg.logger.Info("Starting gRPC server", zap.String("addr", conf.GRPCAddr))

	go func() {
		if err := sg.grpcServer.Serve(lis); err != nil {
			sg.logger.Error("gRPC server Serve error", zap.Error(err))
		}
	}()

	return nil
}

// Shutdown выполняет graceful shutdown всех серверов
func (sg *ServerGroup) Shutdown(ctx context.Context) error {
	sg.logger.Info("Starting graceful shutdown of all servers...")

	var errs []error

	if sg.grpcServer != nil {
		done := make(chan struct{})
		go func() {
			sg.grpcServer.GracefulStop()
			close(done)
		}()

		select {
		case <-done:
			sg.logger.Info("gRPC server stopped gracefully")
		case <-ctx.Done():
			sg.grpcServer.Stop()
			sg.logger.Warn("gRPC server force stopped")
		}
	}

	if sg.grpcListener != nil {
		sg.grpcListener.Close()
	}

	if sg.httpServer != nil {
		if err := sg.httpServer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("HTTP server shutdown: %w", err))
			sg.logger.Error("HTTP server shutdown error", zap.Error(err))
		} else {
			sg.logger.Info("HTTP server shutdown gracefully")
		}
	}

	if sg.storage != nil {
		if err := sg.storage.Close(); err != nil {
			errs = append(errs, fmt.Errorf("storage close: %w", err))
			sg.logger.Error("Storage close error", zap.Error(err))
		} else {
			sg.logger.Info("Storage closed successfully")
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}

	sg.logger.Info("All servers stopped gracefully")
	return nil
}
