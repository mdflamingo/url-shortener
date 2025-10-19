package handler

import (
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/mdflamingo/url-shortener/internal/repository"
	"github.com/mdflamingo/url-shortener/internal/service"

	"github.com/go-chi/chi/v5"
)

func PostHandler(response http.ResponseWriter, request *http.Request, baseURL string, storage *repository.URLStorage) {
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

	for attempts := range maxAttempts {
		shortURL = service.GenerateShortURL(6)
		if _, ok := storage.Exists(shortURL); !ok {
			break
		}
		if attempts == maxAttempts-1 {
			log.Printf("Error: failed to generate unique short URL after %d attempts", maxAttempts)
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}

	storage.Save(shortURL, string(body))

	fullURL, err := url.JoinPath(baseURL, shortURL)
	if err != nil {
		log.Printf("Error: failed to join path: %v", err)
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
		http.Error(response, "URL not found", http.StatusNotFound)
	}
}
