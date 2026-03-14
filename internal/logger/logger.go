// Package logger предоставляет глобальный логгер на базе zap и middleware для логирования HTTP запросов
//
// Инициализирует структурированное логирование с поддержкой уровней (INFO, DEBUG, WARN, ERROR)
// и логирует все входящие HTTP запросы с метриками (статус, размер ответа, время выполнения).
package logger

import (
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Log - глобальный логгер приложения (thread-safe singleton)
var Log *zap.Logger = zap.NewNop()

var (
	once     sync.Once
	initErr  error
	levelStr string
)

// Initialize инициализирует глобальный логгер с заданным уровнем логирования
//
// Поддерживаемые уровни: debug, info, warn, error (нечувствительно к регистру)
// Применяет production конфигурацию с сэмплированием и ISO8601 временем.
//
// Пример:
//
//	logger.Initialize("debug")
//
// Возвращает ошибку при неудачной инициализации конфигурации zap.
func Initialize(level string) error {
	levelStr = level

	lvl, err := zap.ParseAtomicLevel(level)
	if err != nil {
		return err
	}

	var logger *zap.Logger
	once.Do(func() {
		cfg := zap.NewProductionConfig()
		cfg.Level = lvl

		cfg.Sampling = &zap.SamplingConfig{
			Initial:    100,
			Thereafter: 100,
		}
		cfg.DisableStacktrace = true
		cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

		logger, err = cfg.Build(zap.AddCallerSkip(1))
		if err != nil {
			initErr = err
			return
		}

		Log = logger
	})

	return initErr
}

// getLogger возвращает инициализированный глобальный логгер (lazy initialization)
//
// Вызывается внутренне всеми лог-гер методами. Гарантирует thread-safe инициализацию.
func getLogger() *zap.Logger {
	once.Do(func() {
		lvl := zap.NewAtomicLevelAt(zap.InfoLevel)
		if levelStr != "" {
			if parsed, err := zap.ParseAtomicLevel(levelStr); err == nil {
				lvl = parsed
			}
		}

		cfg := zap.NewProductionConfig()
		cfg.Level = lvl
		cfg.Sampling = &zap.SamplingConfig{
			Initial:    100,
			Thereafter: 100,
		}
		cfg.DisableStacktrace = true
		cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

		logger, err := cfg.Build(zap.AddCallerSkip(1))
		if err != nil {
			Log = zap.NewNop()
			return
		}
		Log = logger
	})

	return Log
}

// responseData содержит метрики HTTP ответа для логирования
type responseData struct {
	status int // HTTP статус код ответа
	size   int // размер ответа в байтах
}

// loggingResponseWriter - обертка над http.ResponseWriter для перехвата метрик ответа
type loggingResponseWriter struct {
	http.ResponseWriter               // встраиваем оригинальный ResponseWriter
	responseData        *responseData // метрики ответа
}

// Write перехватывает данные записи в ResponseWriter и подсчитывает размер
func (r *loggingResponseWriter) Write(b []byte) (int, error) {
	size, err := r.ResponseWriter.Write(b)
	r.responseData.size += size
	return size, err
}

// WriteHeader перехватывает установку HTTP статуса
func (r *loggingResponseWriter) WriteHeader(statusCode int) {
	r.ResponseWriter.WriteHeader(statusCode)
	r.responseData.status = statusCode
}

// RequestLogger - middleware для логирования всех HTTP запросов
//
// Логирует: URI, метод, статус ответа, размер ответа, время выполнения.
// Использует sync.Pool для минимизации аллокаций responseData.
//
// Регистрация в chi:
//
//	r.Use(logger.RequestLogger)
//
// Пример лога:
//
//	{"level":"info","ts":"2024-01-01T12:00:00Z","msg":"request completed","uri":"/api/shorten","method":"POST","status":201,"duration":2.345ms,"size":25}
func RequestLogger(h http.Handler) http.Handler {
	pool := sync.Pool{
		New: func() interface{} {
			return &responseData{
				status: 200,
				size:   0,
			}
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		responseData := pool.Get().(*responseData)
		responseData.status = 200
		responseData.size = 0

		lw := &loggingResponseWriter{
			ResponseWriter: w,
			responseData:   responseData,
		}

		h.ServeHTTP(lw, r)

		defer pool.Put(responseData)

		duration := time.Since(start)

		getLogger().Info("request completed",
			zap.String("uri", r.RequestURI),
			zap.String("method", r.Method),
			zap.Int("status", responseData.status),
			zap.Duration("duration", duration),
			zap.Int("size", responseData.size),
		)
	})
}

// Sync синхронизирует все буферы логгера с дисковыми файлами
func Sync() error {
	if Log != nil {
		return Log.Sync()
	}
	return nil
}
