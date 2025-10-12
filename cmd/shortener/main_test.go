package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupRouter() http.Handler {
	r := chi.NewRouter()
	r.Get("/{id}", getHandler)
	r.Post("/", postHandler)
	return r
}

func TestPostHandler(t *testing.T) {
	storage = make(map[string]string)

	tests := []struct {
		name           string
		contentType    string
		body           string
		wantStatusCode int
		wantBody       string
	}{
		{
			name:           "positive test with valid data",
			contentType:    "text/plain",
			body:           "https://example.com",
			wantStatusCode: http.StatusCreated,
			wantBody:       "",
		},
		{
			name:           "invalid content type",
			contentType:    "application/json",
			body:           "https://example.com",
			wantStatusCode: http.StatusUnsupportedMediaType,
			wantBody:       "Invalid Content-Type\n",
		},
		{
			name:           "empty body",
			contentType:    "text/plain",
			body:           "",
			wantStatusCode: http.StatusBadRequest,
			wantBody:       "URL cannot be empty\n",
		},
		{
			name:           "body with whitespace only",
			contentType:    "text/plain",
			body:           "   ",
			wantStatusCode: http.StatusBadRequest,
			wantBody:       "URL cannot be empty\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(tt.body))
			request.Header.Set("Content-Type", tt.contentType)

			w := httptest.NewRecorder()

			router := setupRouter()
			router.ServeHTTP(w, request)

			res := w.Result()
			defer res.Body.Close()

			resBody, err := io.ReadAll(res.Body)
			require.NoError(t, err)

			assert.Equal(t, tt.wantStatusCode, res.StatusCode)

			if tt.wantStatusCode == http.StatusCreated {
				shortURL := string(resBody)
				parts := strings.Split(shortURL, "/")
				shortID := parts[len(parts)-1]
				assert.Len(t, shortID, 6)
				for _, char := range shortID {
					assert.True(t, strings.Contains(letters, string(char)))
				}
				assert.Equal(t, tt.body, storage[shortID])
			} else {
				assert.Equal(t, tt.wantBody, string(resBody))
			}
		})
	}
}

func TestGetHandler(t *testing.T) {
	storage = make(map[string]string)
	testShortURL := "abc123"
	testOriginalURL := "https://example.com"
	storage[testShortURL] = testOriginalURL

	tests := []struct {
		name           string
		path           string
		wantStatusCode int
		wantBody       string
	}{
		{
			name:           "positive test - existing URL",
			path:           "/" + testShortURL,
			wantStatusCode: http.StatusTemporaryRedirect,
			wantBody:       "<a href=\"https://example.com\">Temporary Redirect</a>.\n\n",
		},
		{
			name:           "non-existing URL",
			path:           "/nonexistent",
			wantStatusCode: http.StatusNotFound,
			wantBody:       "URL not found\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			router := setupRouter()
			router.ServeHTTP(w, request)

			res := w.Result()
			defer res.Body.Close()

			resBody, err := io.ReadAll(res.Body)
			require.NoError(t, err)

			assert.Equal(t, tt.wantStatusCode, res.StatusCode)
			assert.Equal(t, tt.wantBody, string(resBody))
		})
	}
}

func TestGetHandler_SpecialCharacters(t *testing.T) {
	storage = make(map[string]string)
	request := httptest.NewRequest(http.MethodGet, "/test/url", nil)
	w := httptest.NewRecorder()

	router := setupRouter()
	router.ServeHTTP(w, request)

	res := w.Result()
	defer res.Body.Close()

	assert.Equal(t, http.StatusNotFound, res.StatusCode)

	resBody, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.Equal(t, "404 page not found\n", string(resBody))
}
