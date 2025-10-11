package main

import (
	"io"
	"math/rand"
	"net/http"
	"strings"
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
	id := request.URL.Path[1:]
	orig_url := storage[id]

	response.Write([]byte(orig_url))
}

func main() {
	storage = make(map[string]string)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path != "/" && request.Method == http.MethodGet:
			getHandler(response, request)
		case request.URL.Path == "/" && request.Method == http.MethodPost:
			postHandler(response, request)
		default:
			http.NotFound(response, request)
		}
	})

	err := http.ListenAndServe(`:8080`, mux)
	if err != nil {
		panic(err)
	}
}
