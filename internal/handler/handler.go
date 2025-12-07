package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mdflamingo/url-shortener/internal/logger"
	"github.com/mdflamingo/url-shortener/internal/models"
	"github.com/mdflamingo/url-shortener/internal/repository"
	"github.com/mdflamingo/url-shortener/internal/service"

	"go.uber.org/zap"

	"github.com/go-chi/chi/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func PostHandler(response http.ResponseWriter, request *http.Request, baseURL string, storage repository.URLStorage) {
	if request.Header.Get("Content-Type") != "text/plain" {
		logger.Log.Warn("invalid content type", zap.String("content_type", request.Header.Get("Content-Type")))
		http.Error(
			response,
			"Invalid Content-Type",
			http.StatusUnsupportedMediaType,
		)
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

	shortURL, err := GenerateAndSaveShortURL(originalURL, storage)

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

	response.Header().Set("Content-Type", "text/plain")
	response.WriteHeader(http.StatusCreated)
	response.Write([]byte(fullURL))
}

func GetHandler(response http.ResponseWriter, request *http.Request, storage repository.URLStorage) {
	id := chi.URLParam(request, "id")
	origURL, ok := storage.Get(id)

	if ok {
		http.Redirect(response, request, origURL, http.StatusTemporaryRedirect)
	} else {
		logger.Log.Warn("short URL not found",
			zap.String("short_id", id))
		http.Error(response, "URL not found", http.StatusNotFound)
	}
}

func JSONPostHandler(response http.ResponseWriter, request *http.Request, baseURL string, storage repository.URLStorage) {
	if request.Method != http.MethodPost {
		http.Error(response, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

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

	_, err := buf.ReadFrom(request.Body)
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

	shortURL, err := GenerateAndSaveShortURL(origURL.URL, storage)

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

	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusCreated)
	response.Write(respJSON)
}

func BatchHandler(response http.ResponseWriter, request *http.Request, baseURL string, storage repository.URLStorage) {
	if request.Method != http.MethodPost {
		http.Error(response, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if request.Header.Get("Content-Type") != "application/json" {
		logger.Log.Warn("invalid content type", zap.String("content_type", request.Header.Get("Content-Type")))
		http.Error(
			response,
			"Invalid Content-Type",
			http.StatusUnsupportedMediaType,
		)
		return
	}

	var batches []models.BatchRequest
	var buf bytes.Buffer

	_, err := buf.ReadFrom(request.Body)
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
	responses := make([]models.BatchResponse, 0, len(batches))

	for _, row := range batches {
		if row.Original_url == "" {
			http.Error(response, "URL cannot be empty", http.StatusBadRequest)
			return
		}

		if _, err := url.ParseRequestURI(row.Original_url); err != nil {
			logger.Log.Warn("invalid URL",
				zap.String("url", row.Original_url),
				zap.Error(err))
			http.Error(response, "Invalid URL format", http.StatusBadRequest)
			return
		}

		shortURL := service.GenerateShortURLForBatch(row.Original_url)
		fullURL, err := url.JoinPath(baseURL, shortURL)
		if err != nil {
			logger.Log.Error("failed to join URL path",
				zap.String("base_url", baseURL),
				zap.String("short_url", shortURL),
				zap.Error(err))
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		urlPairs = append(urlPairs, repository.URLPair{
			ShortURL:    shortURL,
			OriginalURL: row.Original_url,
		})

		responses = append(responses, models.BatchResponse{
			Correlation_id: row.Correlation_id,
			Short_url:      fullURL,
		})
	}

	if err := storage.SaveMany(urlPairs); err != nil {
		logger.Log.Error("Failed to save URLs in batch", zap.Error(err))
		http.Error(response, "Failed to save URLs: "+err.Error(), http.StatusInternalServerError)
		return
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

func GenerateAndSaveShortURL(originalURL string, storage repository.URLStorage) (string, error) {
	var maxAttempts = 10

	for attempt := 0; attempt < maxAttempts; attempt++ {
		shortURL := service.GenerateShortURL(6)
		savedShortURL, err := storage.Save(shortURL, originalURL)

		if err == nil {
			return savedShortURL, nil
		}

		if errors.Is(err, repository.ErrConflict) {
			return savedShortURL, err
		}

	}

	return "", fmt.Errorf("failed to generate unique short URL after %d attempts", maxAttempts)
}

func DBHealthCheck(response http.ResponseWriter, request *http.Request, pg_dsn string) {
	logger.Log.Info("HealthCheck called", zap.String("method", request.Method))

	if request.Method != http.MethodGet {
		http.Error(response, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if pg_dsn == "" {
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	db, err := sql.Open("pgx", pg_dsn)
	if err != nil {
		logger.Log.Error("failed connect to postgres", zap.Error(err))
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err = db.PingContext(ctx); err != nil {
		logger.Log.Error("postgres not available", zap.Error(err))
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	logger.Log.Info("HealthCheck completed successfully")
	response.WriteHeader(http.StatusOK)
	response.Write([]byte("OK"))
}
