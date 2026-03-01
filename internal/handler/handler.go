package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mdflamingo/url-shortener/internal/logger"
	"github.com/mdflamingo/url-shortener/internal/middleware"
	"github.com/mdflamingo/url-shortener/internal/models"
	"github.com/mdflamingo/url-shortener/internal/repository"
	"github.com/mdflamingo/url-shortener/internal/service"
	"go.uber.org/zap"

	"github.com/go-chi/chi/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func PostHandler(response http.ResponseWriter, request *http.Request, baseURL string, storage repository.URLStorage, audit *service.AuditService) {
	if request.Header.Get("Content-Type") != "text/plain" {
		logger.Log.Warn("invalid content type", zap.String("content_type", request.Header.Get("Content-Type")))
		http.Error(
			response,
			"Invalid Content-Type",
			http.StatusUnsupportedMediaType,
		)
		return
	}

	userID, err := middleware.GetUserIDFromRequest(request)
	if err != nil {
		logger.Log.Warn("failed to get userID", zap.Error(err))
		http.Error(response, "Unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(request.Body)

	if err != nil {
		logger.Log.Error("failed to read request body", zap.Error(err))
		http.Error(response, "Failed to read request data", http.StatusBadRequest)
		return
	}

	originalURL := strings.TrimSpace(string(body))
	if originalURL == "" {
		logger.Log.Warn("empty URL provided")
		http.Error(
			response,
			"URL cannot be empty",
			http.StatusBadRequest,
		)
		return
	}

	_, err = url.Parse(originalURL)
	if err != nil {
		logger.Log.Warn("invalid URL provided",
			zap.Error(err))
		http.Error(response, "Invalid URL provided", http.StatusBadRequest)
		return
	}

	shortURL, err := GenerateAndSaveShortURL(originalURL, storage, userID)

	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			fullURL, joinErr := url.JoinPath(baseURL, shortURL)
			if joinErr != nil {
				logger.Log.Error("failed to join URL path",
					zap.String("base_url", baseURL),
					zap.String("short_url", shortURL),
					zap.Error(joinErr))
				http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}

			response.Header().Set("Content-Type", "text/plain")
			response.WriteHeader(http.StatusConflict)
			response.Write([]byte(fullURL))
			return
		}

		logger.Log.Error("Failed to generate short URL",
			zap.String("original_url", originalURL),
			zap.Error(err))
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	fullURL, err := url.JoinPath(baseURL, shortURL)
	if err != nil {
		logger.Log.Error("failed to join URL path",
			zap.String("base_url", baseURL),
			zap.String("short_url", shortURL),
			zap.Error(err))
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if audit != nil {
		audit.Notify(service.AuditEvent{Action: "shorten", UserID: userID, URL: fullURL, TS: time.Now().Unix()})
	}
	response.Header().Set("Content-Type", "text/plain")
	response.WriteHeader(http.StatusCreated)
	response.Write([]byte(fullURL))
}

func GetHandler(response http.ResponseWriter, request *http.Request, storage repository.URLStorage, audit *service.AuditService) {
	userID, err := middleware.GetUserIDFromRequest(request)
	if err != nil {
		logger.Log.Warn("failed to get userID", zap.Error(err))
		http.Error(response, "Unauthorized", http.StatusUnauthorized)
		return
	}

	id := chi.URLParam(request, "id")
	origURL, found, deleted := storage.Get(id)

	if !found {
		logger.Log.Warn("short URL not found",
			zap.String("short_id", id))
		http.Error(response, "URL not found", http.StatusNotFound)
		return
	}

	if deleted {
		logger.Log.Warn("short URL is deleted",
			zap.String("short_id", id))
		http.Error(response, "Gone", http.StatusGone)
		return
	}
	if audit != nil {
		audit.Notify(service.AuditEvent{Action: "follow", UserID: userID, URL: origURL, TS: time.Now().Unix()})
	}

	http.Redirect(response, request, origURL, http.StatusTemporaryRedirect)
}

func JSONPostHandler(response http.ResponseWriter, request *http.Request, baseURL string, storage repository.URLStorage, audit *service.AuditService) {
	if request.Header.Get("Content-Type") != "application/json" {
		logger.Log.Warn("invalid content type", zap.String("content_type", request.Header.Get("Content-Type")))
		http.Error(
			response,
			"Invalid Content-Type",
			http.StatusUnsupportedMediaType,
		)
		return
	}

	var origURL models.Request
	var buf bytes.Buffer

	userID, err := middleware.GetUserIDFromRequest(request)
	if err != nil {
		logger.Log.Warn("failed to get userID", zap.Error(err))
		http.Error(response, "Unauthorized", http.StatusUnauthorized)
		return
	}
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

	if origURL.URL == "" {
		http.Error(response, "URL cannot be empty", http.StatusBadRequest)
		return
	}

	shortURL, err := GenerateAndSaveShortURL(origURL.URL, storage, userID)

	if errors.Is(err, repository.ErrConflict) {
		fullURL, joinErr := url.JoinPath(baseURL, shortURL)
		if joinErr != nil {
			logger.Log.Error("failed to join URL path",
				zap.String("base_url", baseURL),
				zap.String("short_url", shortURL),
				zap.Error(joinErr))
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		resp := models.Response{
			Result: fullURL,
		}
		respJSON, marshalErr := json.Marshal(resp)
		if marshalErr != nil {
			logger.Log.Error("Failed to marshal response to JSON",
				zap.Error(marshalErr),
				zap.Any("response_object", resp))
			http.Error(response, marshalErr.Error(), http.StatusInternalServerError)
			return
		}

		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusConflict)
		response.Write(respJSON)
		return
	}

	if err != nil {
		logger.Log.Error("Failed to generate and save short URL",
			zap.Error(err),
			zap.String("original_url", origURL.URL))
		http.Error(response, "Failed to create short URL: "+err.Error(), http.StatusInternalServerError)
		return
	}

	fullURL, err := url.JoinPath(baseURL, shortURL)
	if err != nil {
		logger.Log.Error("failed to join URL path",
			zap.String("base_url", baseURL),
			zap.String("short_url", shortURL),
			zap.Error(err))
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp := models.Response{
		Result: fullURL,
	}
	respJSON, err := json.Marshal(resp)
	if err != nil {
		logger.Log.Error("Failed to marshal response to JSON",
			zap.Error(err),
			zap.Any("response_object", resp))
		http.Error(response, err.Error(), http.StatusInternalServerError)
		return
	}

	if audit != nil {
		audit.Notify(service.AuditEvent{Action: "shorten", UserID: userID, URL: fullURL, TS: time.Now().Unix()})
	}

	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusCreated)
	response.Write(respJSON)
}

func BatchHandler(response http.ResponseWriter, request *http.Request, baseURL string, storage repository.URLStorage) {
	var batches []models.BatchRequest
	var buf bytes.Buffer

	userID, err := middleware.GetUserIDFromRequest(request)
	if err != nil {
		logger.Log.Warn("failed to get userID", zap.Error(err))
		http.Error(response, "Unauthorized", http.StatusUnauthorized)
		return
	}
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

	urlPairs := make([]repository.URLPair, 0, len(batches))

	for _, row := range batches {
		if row.OriginalURL == "" {
			http.Error(response, "URL cannot be empty", http.StatusBadRequest)
			return
		}

		if _, err := url.ParseRequestURI(row.OriginalURL); err != nil {
			logger.Log.Warn("invalid URL",
				zap.String("url", row.OriginalURL),
				zap.Error(err))
			http.Error(response, "Invalid URL format", http.StatusBadRequest)
			return
		}

		shortURL := service.GenerateShortURLForBatch(row.OriginalURL)
		urlPairs = append(urlPairs, repository.URLPair{
			ShortURL:    shortURL,
			OriginalURL: row.OriginalURL,
			UserID:      userID,
		})
	}

	updatedPairs, err := storage.SaveMany(urlPairs)
	if err != nil {
		logger.Log.Error("Failed to save URLs in batch", zap.Error(err))
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	responses := make([]models.BatchResponse, 0, len(updatedPairs))
	for i, pair := range updatedPairs {
		fullURL, err := url.JoinPath(baseURL, pair.ShortURL)
		if err != nil {
			logger.Log.Error("failed to join URL path",
				zap.String("base_url", baseURL),
				zap.String("short_url", pair.ShortURL),
				zap.Error(err))
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		responses = append(responses, models.BatchResponse{
			CorrelationID: batches[i].CorrelationID,
			ShortURL:      fullURL,
		})
	}

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

func GenerateAndSaveShortURL(originalURL string, storage repository.URLStorage, userID string) (string, error) {
	var maxAttempts = 10

	for attempt := 0; attempt < maxAttempts; attempt++ {
		shortURL := service.GenerateShortURL(6)
		savedShortURL, err := storage.Save(shortURL, originalURL, userID)

		if err == nil {
			return savedShortURL, nil
		}

		if errors.Is(err, repository.ErrConflict) {
			if savedShortURL != "" {
				return savedShortURL, err
			}
			continue
		}

	}

	return "", fmt.Errorf("failed to generate unique short URL after %d attempts", maxAttempts)
}

func DBHealthCheck(response http.ResponseWriter, request *http.Request, storage repository.URLStorage) {
	logger.Log.Info("HealthCheck called", zap.String("method", request.Method))

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := storage.Ping(ctx); err != nil {
		logger.Log.Error("storage not available", zap.Error(err))
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger.Log.Info("HealthCheck completed successfully")
	response.WriteHeader(http.StatusOK)
	response.Write([]byte("OK"))
}

func GetUserURLSHandler(response http.ResponseWriter, request *http.Request, baseURL string, storage repository.URLStorage) {
	userID, err := middleware.GetUserIDFromRequest(request)
	if err != nil {
		logger.Log.Warn("failed to get userID", zap.Error(err))
		http.Error(response, "Unauthorized", http.StatusUnauthorized)
		return
	}

	urls, err := storage.GetByUserID(userID)
	if err != nil {
		logger.Log.Error("Failed to get URLs from storage", zap.Error(err))
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if len(urls) == 0 {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusNoContent)
		return
	}

	responses := make([]models.ResponseByUser, 0, len(urls))
	for _, pair := range urls {
		fullURL, err := url.JoinPath(baseURL, pair.ShortURL)
		if err != nil {
			logger.Log.Error("Failed to join URL path",
				zap.String("base_url", baseURL),
				zap.String("short_url", pair.ShortURL),
				zap.Error(err))
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		responses = append(responses, models.ResponseByUser{
			OriginalURL: pair.OriginalURL,
			ShortURL:    fullURL,
		})
	}

	respJSON, err := json.Marshal(responses)
	if err != nil {
		logger.Log.Error("Failed to marshal response to JSON", zap.Error(err))
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusOK)
	response.Write(respJSON)
}

func DeleteUserURLSHandler(response http.ResponseWriter, request *http.Request, baseURL string, storage repository.URLStorage) {
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
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusBadRequest)
		return
	}

	inputCh := make(chan string, len(urls))
	doneCh := make(chan struct{})

	go func() {
		defer close(inputCh)
		for _, url := range urls {
			select {
			case <-doneCh:
				logger.Log.Info("Delete cancelled")
				return
			case inputCh <- url:
				logger.Log.Info("Sending URL for delete", zap.String("url", url))
			}
		}
	}()

	resultCh := storage.Delete(doneCh, inputCh, userID)

	go func() {
		defer close(doneCh)
		for err := range resultCh {
			if err != nil {
				logger.Log.Error("Failed to delete URL batch", zap.Error(err))
			} else {
				logger.Log.Info("URL batch deleted successfully")
			}
		}
		logger.Log.Info("All delete operations completed")
	}()

	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusAccepted)
}
