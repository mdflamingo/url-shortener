package logger

import (
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Log *zap.Logger = zap.NewNop()

var (
	once     sync.Once
	initErr  error
	levelStr string
)

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

type (
	responseData struct {
		status int
		size   int
	}

	loggingResponseWriter struct {
		http.ResponseWriter
		responseData *responseData
	}
)

func (r *loggingResponseWriter) Write(b []byte) (int, error) {
	size, err := r.ResponseWriter.Write(b)
	r.responseData.size += size
	return size, err
}

func (r *loggingResponseWriter) WriteHeader(statusCode int) {
	r.ResponseWriter.WriteHeader(statusCode)
	r.responseData.status = statusCode
}

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

func Sync() error {
	if Log != nil {
		return Log.Sync()
	}
	return nil
}
