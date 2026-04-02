// Package router отвечает за настройку маршрутизации HTTP-запросов
//
// Пакет использует маршрутизатор chi и связывает HTTP-эндпоинты
// с соответствующими обработчиками из пакета handler.
// Здесь также применяются глобальные middleware для всех запросов.
package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mdflamingo/url-shortener/internal/config"
	"github.com/mdflamingo/url-shortener/internal/handler"
	"github.com/mdflamingo/url-shortener/internal/logger"
	"github.com/mdflamingo/url-shortener/internal/repository"
	"github.com/mdflamingo/url-shortener/internal/service"

	"github.com/mdflamingo/url-shortener/internal/middleware"
)

// NewRouter создает и настраивает новый маршрутизатор HTTP-запросов
//
// Функция инициализирует маршрутизатор chi, подключает все необходимые middleware
// и регистрирует обработчики для всех эндпоинтов приложения.
//
// Параметры:
//   - conf: конфигурация приложения (содержит BaseShortURL и другие настройки)
//   - storage: хранилище URL (может быть in-memory, файловым или PostgreSQL)
//   - cookieMiddleware: middleware для работы с подписанными cookie (аутентификация)
//   - auditService: сервис аудита для логирования действий пользователей
//
// Возвращает:
//   - *chi.Mux: настроенный маршрутизатор, готовый к использованию с http.Server
//
// Подключаемые middleware (в порядке применения):
//  1. RequestLogger - логирование всех входящих запросов
//  2. GzipMiddleware - поддержка сжатия gzip для запросов и ответов
//  3. CookieMiddleware - аутентификация пользователей через подписанные cookie
func NewRouter(conf *config.Config, storage repository.URLStorage, cookieMiddleware *middleware.SignedCookieMiddleware, auditService *service.AuditService) *chi.Mux {
	r := chi.NewRouter()

	// Подключение глобальных middleware
	r.Use(logger.RequestLogger)
	r.Use(cookieMiddleware.CookieMiddleware)
	r.Use(middleware.GzipMiddleware)

	// Эндпоинт для проверки работоспособности (health check)
	r.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
		handler.DBHealthCheck(w, r, storage)
	})

	// Эндпоинт для перехода по короткой ссылке
	r.Get("/{id}", func(w http.ResponseWriter, req *http.Request) {
		handler.GetHandler(w, req, storage, auditService)
	})

	// Эндпоинт для создания короткой ссылки из текстового URL
	r.Post("/", func(w http.ResponseWriter, req *http.Request) {
		handler.PostHandler(w, req, conf.BaseShortURL, storage, auditService)
	})

	// Эндпоинт для создания короткой ссылки из JSON
	r.Post("/api/shorten", func(w http.ResponseWriter, req *http.Request) {
		handler.JSONPostHandler(w, req, conf.BaseShortURL, storage, auditService)
	})

	// Эндпоинт для пакетного создания коротких ссылок
	r.Post("/api/shorten/batch", func(w http.ResponseWriter, req *http.Request) {
		handler.BatchHandler(w, req, conf.BaseShortURL, storage)
	})

	// Эндпоинт для получения всех URL текущего пользователя
	r.Get("/api/user/urls", func(w http.ResponseWriter, req *http.Request) {
		handler.GetUserURLSHandler(w, req, conf.BaseShortURL, storage)
	})

	// Эндпоинт для удаления нескольких URL текущего пользователя
	r.Delete("/api/user/urls", func(w http.ResponseWriter, req *http.Request) {
		handler.DeleteUserURLSHandler(w, req, conf.BaseShortURL, storage)
	})

	return r
}
