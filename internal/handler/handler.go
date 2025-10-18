package handler

import (
	"github.com/mdflamingo/url-shortener/internal/service"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
)

var Storage map[string]string

func PostHandler(response http.ResponseWriter, request *http.Request, baseURL string) {
	if request.Header.Get("Content-Type") != "text/plain" {
		http.Error(
			response,
			"Invalid Content-Type",
			http.StatusUnsupportedMediaType,
		)
		return
	}

	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(response, "Failed to read request data", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(string(body)) == "" {
		http.Error(
			response,
			"URL cannot be empty",
			http.StatusBadRequest,
		)
		return
	}
	_, err = url.Parse(string(body))
	if err != nil {
		http.Error(response, "Invalid URL provided", http.StatusBadRequest)
		return
	}

	var maxAttempts = 10
	var shortURL string

	for attempts := 0; attempts < maxAttempts; attempts++ {
		shortURL = service.GenerateShortURL(6)
		if _, exists := Storage[shortURL]; !exists {
			break
		}
		if attempts == maxAttempts-1 {
			http.Error(response, "Failed to generate unique short URL", http.StatusBadRequest)
			return
		}
	}

	Storage[shortURL] = string(body)
	fullURL, err := url.JoinPath(baseURL, shortURL)
	if err != nil {
		http.Error(response, "Failed to create full url", http.StatusBadRequest)

	}
	response.WriteHeader(http.StatusCreated)
	response.Write([]byte(fullURL))
}

func GetHandler(response http.ResponseWriter, request *http.Request) {
	id := chi.URLParam(request, "id")
	origURL, exists := Storage[id]

	if exists {
		http.Redirect(response, request, origURL, http.StatusTemporaryRedirect)
	} else {
		http.Error(response, "URL not found", http.StatusNotFound)
	}
}
