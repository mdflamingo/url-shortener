package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mdflamingo/url-shortener/internal/logger"
	"github.com/mdflamingo/url-shortener/internal/models"
	"github.com/mdflamingo/url-shortener/internal/repository"
	"github.com/mdflamingo/url-shortener/internal/service"
	"go.uber.org/zap"

	"github.com/go-chi/chi/v5"
)

func PostHandler(response http.ResponseWriter, request *http.Request, baseURL string, storage *repository.URLStorage) {
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
	if strings.TrimSpace(string(body)) == "" {
		logger.Log.Warn("empty URL provided")
		http.Error(
			response,
			"URL cannot be empty",
			http.StatusBadRequest,
		)
		return
	}

	_, err = url.Parse(string(body))

	if err != nil {
		logger.Log.Warn("invalid URL provided",
			zap.Error(err))
		http.Error(response, "Invalid URL provided", http.StatusBadRequest)
		return
	}

	shortURL, err := GenerateShortURL(string(body), response, storage)

	if err != nil {
		logger.Log.Error("Failed to generate short URL",
			zap.String("original_url", string(body)),
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
	response.WriteHeader(http.StatusCreated)
	response.Write([]byte(fullURL))
}

func GetHandler(response http.ResponseWriter, request *http.Request, storage *repository.URLStorage) {
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

func JSONPostHandler(response http.ResponseWriter, request *http.Request, baseURL string, storage *repository.URLStorage) {
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
	shortURL, err := GenerateShortURL(origURL.URL, response, storage)

	if err != nil {
		logger.Log.Error("Failed to generate short URL",
			zap.String("original_url", origURL.URL),
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

	resp := models.Response{
		Result: fullURL,
	}
	respJSON, err := json.Marshal(resp)

	if err != nil {
		logger.Log.Error("Failed to marshal response to JSON", zap.Error(err), zap.Any("response_object", resp))
		http.Error(response, err.Error(), http.StatusInternalServerError)
		return
	}

	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusOK)
	response.Write(respJSON)
}

func GenerateShortURL(origURL string, response http.ResponseWriter, storage *repository.URLStorage) (string, error) {
	var maxAttempts = 10
	var shortURL string

	for attempts := range maxAttempts {
		shortURL = service.GenerateShortURL(6)
		err := storage.Save(shortURL, string(origURL))
		if err == nil {
			return shortURL, nil
		}

		logger.Log.Warn("ID collision detected",
			zap.String("short_url", shortURL),
			zap.Int("attempt", attempts+1),
			zap.Error(err))

		if attempts == maxAttempts-1 {
			return "", fmt.Errorf("failed to generate unique short URL after %d attempts: %w", maxAttempts, err)
		}
	}
	return "", fmt.Errorf("unknown error")

}
