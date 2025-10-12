package main

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"github.com/go-chi/chi/v5"
)

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
var storage map[string]string

func generateShortURL(length int) string {
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

	shortURL := generateShortURL(6)
	storage[shortURL] = string(body)
	fullShortURL := baseShortURL + "/" + shortURL

	response.WriteHeader(http.StatusCreated)
    response.Write([]byte(fullShortURL))
}

func getHandler(response http.ResponseWriter, request *http.Request) {
	id := chi.URLParam(request, "id")
    origURL, exists := storage[id]

    if exists {
		http.Redirect(response, request, origURL, http.StatusTemporaryRedirect)
    } else {
        http.Error(response, "URL not found", http.StatusNotFound)
    }
}

func main() {
	parseFlags()
	if err := run(); err != nil {
        panic(err)
    }
}

func run() error {
    fmt.Println("Running server on", flagRunAddr)
	fmt.Printf("Base short URL: %s\n", baseShortURL)

	storage = make(map[string]string)
	r := chi.NewRouter()
	r.Get("/{id}", getHandler)
	r.Post("/", postHandler)
    return http.ListenAndServe(flagRunAddr, r)
}
