package main

import (
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
var storage map[string]string

func generateShortUrl(length int) string {
	short := make([]byte, length)
	for i := range short {
		short[i] = letters[rand.Intn(len(letters))]
	}
	return string(short)
}

func postHandler(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Content-Type") != "text/plain" {
		http.Error(
			response,
			"Invalid Content-Type",
			http.StatusUnsupportedMediaType,
		)
		return
	}

	body, _ := io.ReadAll(request.Body)

	if strings.TrimSpace(string(body)) == "" {
		http.Error(
			response,
			"URL cannot be empty",
			http.StatusBadRequest,
		)
		return
	}

	short_url := generateShortUrl(6)
	storage[short_url] = string(body)
	response.Write([]byte(short_url))
}

func getHandler(response http.ResponseWriter, request *http.Request) {
	id := chi.URLParam(request, "id")
    orig_url, exists := storage[id]

    if exists {
        response.Write([]byte(orig_url))
    } else {
        http.Error(response, "URL not found", http.StatusNotFound)
    }
}

func main() {
	storage = make(map[string]string)
	r := chi.NewRouter()
	r.Get("/{id}", getHandler)
	r.Post("/", postHandler)

	log.Fatal(http.ListenAndServe(":8080", r))
}
