// Package handler содержит HTTP-обработчики для URL Shortener
//
// Обработчики реализуют REST API для:
// - Создания коротких ссылок (plain/text и JSON)
// - Перехода по коротким ссылкам
// - Пакетного создания ссылок
// - Получения всех ссылок пользователя
// - Удаления ссылок

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/go-chi/chi/v5"

	"github.com/mdflamingo/url-shortener/internal/logger"
	"github.com/mdflamingo/url-shortener/internal/middleware"
	"github.com/mdflamingo/url-shortener/internal/models"
	"github.com/mdflamingo/url-shortener/internal/service"
)

// PostHandler обрабатывает POST-запросы с текстовым URL
func PostHandler(response http.ResponseWriter, request *http.Request, urlService *service.URLService) {
	// Получаем userID
	userID, err := middleware.GetUserIDFromRequest(request)
	if err != nil {
		logger.Log.Warn("failed to get userID", zap.Error(err))
		http.Error(response, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Проверяем Content-Type
	contentType := request.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		logger.Log.Warn("invalid content type", zap.String("content_type", contentType))
		http.Error(response, "Invalid Content-Type", http.StatusUnsupportedMediaType)
		return
	}

	// Читаем тело запроса
	body, err := io.ReadAll(request.Body)
	if err != nil {
		logger.Log.Error("failed to read request body", zap.Error(err))
		http.Error(response, "Failed to read request data", http.StatusBadRequest)
		return
	}

	originalURL := strings.TrimSpace(string(body))

	// Создаем короткий URL через сервис
	shortURL, isConflict, err := urlService.CreateShortURL(request.Context(), originalURL, userID)
	if err != nil {
		logger.Log.Error("Failed to create short URL",
			zap.String("original_url", originalURL),
			zap.Error(err))

		switch {
		case errors.Is(err, service.ErrEmptyURL):
			http.Error(response, "URL cannot be empty", http.StatusBadRequest)
		case errors.Is(err, service.ErrInvalidURL):
			http.Error(response, "Invalid URL provided", http.StatusBadRequest)
		default:
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		return
	}

	// Строим полный URL
	fullURL := urlService.BuildFullURL(shortURL)
	if fullURL == "" {
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Отправляем ответ
	response.Header().Set("Content-Type", "text/plain")
	if isConflict {
		response.WriteHeader(http.StatusConflict)
	} else {
		response.WriteHeader(http.StatusCreated)
	}
	response.Write([]byte(fullURL))
}

// GetHandler обрабатывает GET-запросы для перехода по короткой ссылке
func GetHandler(response http.ResponseWriter, request *http.Request, urlService *service.URLService) {
	userID, err := middleware.GetUserIDFromRequest(request)
	if err != nil {
		logger.Log.Warn("failed to get userID", zap.Error(err))
		http.Error(response, "Unauthorized", http.StatusUnauthorized)
		return
	}

	id := chi.URLParam(request, "id")

	origURL, err := urlService.GetOriginalURL(request.Context(), id, userID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrURLNotFound):
			http.Error(response, "URL not found", http.StatusNotFound)
		case errors.Is(err, service.ErrURLDeleted):
			http.Error(response, "Gone", http.StatusGone)
		default:
			logger.Log.Error("Failed to get original URL", zap.Error(err))
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		return
	}

	http.Redirect(response, request, origURL, http.StatusTemporaryRedirect)
}

// JSONPostHandler обрабатывает POST-запросы с JSON-телом
func JSONPostHandler(response http.ResponseWriter, request *http.Request, urlService *service.URLService) {
	// Проверяем Content-Type
	contentType := request.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		logger.Log.Warn("invalid content type", zap.String("content_type", contentType))
		http.Error(response, "Invalid Content-Type", http.StatusUnsupportedMediaType)
		return
	}

	// Получаем userID
	userID, err := middleware.GetUserIDFromRequest(request)
	if err != nil {
		logger.Log.Warn("failed to get userID", zap.Error(err))
		http.Error(response, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Читаем и парсим JSON
	var origURL models.Request
	var buf bytes.Buffer

	_, err = buf.ReadFrom(request.Body)
	if err != nil {
		logger.Log.Error("failed to read request body", zap.Error(err))
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}

	if err = json.Unmarshal(buf.Bytes(), &origURL); err != nil {
		logger.Log.Error("Failed to unmarshal JSON",
			zap.Error(err),
			zap.String("request_body", buf.String()))
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}

	// Создаем короткий URL через сервис
	shortURL, isConflict, err := urlService.CreateShortURL(request.Context(), origURL.URL, userID)
	if err != nil {
		logger.Log.Error("Failed to create short URL",
			zap.String("original_url", origURL.URL),
			zap.Error(err))

		switch {
		case errors.Is(err, service.ErrEmptyURL):
			http.Error(response, "URL cannot be empty", http.StatusBadRequest)
		case errors.Is(err, service.ErrInvalidURL):
			http.Error(response, "Invalid URL provided", http.StatusBadRequest)
		default:
			http.Error(response, "Failed to create short URL: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Строим полный URL
	fullURL := urlService.BuildFullURL(shortURL)
	if fullURL == "" {
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Формируем ответ
	resp := models.Response{Result: fullURL}
	respJSON, err := json.Marshal(resp)
	if err != nil {
		logger.Log.Error("Failed to marshal response to JSON",
			zap.Error(err),
			zap.Any("response_object", resp))
		http.Error(response, err.Error(), http.StatusInternalServerError)
		return
	}

	response.Header().Set("Content-Type", "application/json")
	if isConflict {
		response.WriteHeader(http.StatusConflict)
	} else {
		response.WriteHeader(http.StatusCreated)
	}
	response.Write(respJSON)
}

// BatchHandler обрабатывает пакетное создание коротких ссылок
func BatchHandler(response http.ResponseWriter, request *http.Request, urlService *service.URLService) {
	// Получаем userID
	userID, err := middleware.GetUserIDFromRequest(request)
	if err != nil {
		logger.Log.Warn("failed to get userID", zap.Error(err))
		http.Error(response, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Читаем и парсим запрос
	var batches []models.BatchRequest
	var buf bytes.Buffer

	_, err = buf.ReadFrom(request.Body)
	if err != nil {
		logger.Log.Error("failed to read request body", zap.Error(err))
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}

	if err = json.Unmarshal(buf.Bytes(), &batches); err != nil {
		logger.Log.Error("Failed to unmarshal JSON",
			zap.Error(err),
			zap.String("request_body", buf.String()))
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}

	// Создаем пакет через сервис
	responses, err := urlService.CreateBatchShortURLs(request.Context(), batches, userID)
	if err != nil {
		logger.Log.Error("Failed to create batch short URLs", zap.Error(err))
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// Отправляем ответ
	respJSON, err := json.Marshal(responses)
	if err != nil {
		logger.Log.Error("Failed to marshal response to JSON", zap.Error(err))
		http.Error(response, err.Error(), http.StatusInternalServerError)
		return
	}

	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusCreated)
	response.Write(respJSON)
}

// GetUserURLSHandler возвращает все URL текущего пользователя
func GetUserURLSHandler(response http.ResponseWriter, request *http.Request, urlService *service.URLService) {
	userID, err := middleware.GetUserIDFromRequest(request)
	if err != nil {
		logger.Log.Warn("failed to get userID", zap.Error(err))
		http.Error(response, "Unauthorized", http.StatusUnauthorized)
		return
	}

	urls, err := urlService.GetUserURLs(request.Context(), userID)
	if err != nil {
		logger.Log.Error("Failed to get user URLs", zap.Error(err))
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if len(urls) == 0 {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusNoContent)
		return
	}

	respJSON, err := json.Marshal(urls)
	if err != nil {
		logger.Log.Error("Failed to marshal response to JSON", zap.Error(err))
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusOK)
	response.Write(respJSON)
}

// DeleteUserURLSHandler обрабатывает удаление нескольких URL
func DeleteUserURLSHandler(response http.ResponseWriter, request *http.Request, urlService *service.URLService) {
	userID, err := middleware.GetUserIDFromRequest(request)
	if err != nil {
		logger.Log.Warn("failed to get userID", zap.Error(err))
		http.Error(response, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var urls []string
	var buf bytes.Buffer

	_, err = buf.ReadFrom(request.Body)
	if err != nil {
		logger.Log.Error("failed to read request body", zap.Error(err))
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}

	if err = json.Unmarshal(buf.Bytes(), &urls); err != nil {
		logger.Log.Error("Failed to unmarshal JSON",
			zap.Error(err),
			zap.String("request_body", buf.String()))
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}

	if len(urls) == 0 {
		http.Error(response, "Empty URLs list", http.StatusBadRequest)
		return
	}

	// Асинхронное удаление через сервис
	err = urlService.DeleteUserURLs(request.Context(), urls, userID)
	if err != nil {
		logger.Log.Error("Failed to delete user URLs", zap.Error(err))
		http.Error(response, err.Error(), http.StatusInternalServerError)
		return
	}

	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusAccepted)
}

// DBHealthCheck проверяет доступность хранилища
func DBHealthCheck(response http.ResponseWriter, request *http.Request, urlService *service.URLService) {
	logger.Log.Info("HealthCheck called", zap.String("method", request.Method))

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := urlService.Ping(ctx); err != nil {
		logger.Log.Error("storage not available", zap.Error(err))
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger.Log.Info("HealthCheck completed successfully")
	response.WriteHeader(http.StatusOK)
	response.Write([]byte("OK"))
}

// GetStatsHandler возвращает статистику сервиса
func GetStatsHandler(response http.ResponseWriter, request *http.Request, urlService *service.URLService, trustedSubnet string) {
	clientIP := request.Header.Get("X-Real-IP")

	if !urlService.ValidateTrustedIP(clientIP, trustedSubnet) {
		logger.Log.Warn("IP not in trusted subnet",
			zap.String("client_ip", clientIP),
			zap.String("trusted_subnet", trustedSubnet))
		http.Error(response, "Forbidden: IP not in trusted subnet", http.StatusForbidden)
		return
	}

	stats, err := urlService.GetStats(request.Context())
	if err != nil {
		logger.Log.Error("Failed to get stats", zap.Error(err))
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	respJSON, err := json.Marshal(stats)
	if err != nil {
		logger.Log.Error("Failed to marshal response to JSON", zap.Error(err))
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusOK)
	response.Write(respJSON)
}
