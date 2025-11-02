package handler

import (
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mdflamingo/url-shortener/internal/logger"
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

	var maxAttempts = 10
	var shortURL string

	for attempts := range maxAttempts {
		shortURL = service.GenerateShortURL(6)
		err := storage.Save(shortURL, string(body))
		if err == nil {
			break
		}

		logger.Log.Warn("ID collision detected",
			zap.String("short_url", shortURL),
			zap.Int("attempt", attempts+1),
			zap.Error(err))

		if attempts == maxAttempts-1 {
			logger.Log.Error("failed to generate unique short URL after max attempts", zap.Int("max_attempts", maxAttempts))
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
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
