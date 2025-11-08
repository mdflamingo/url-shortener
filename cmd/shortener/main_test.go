package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mdflamingo/url-shortener/internal/handler"
	"github.com/mdflamingo/url-shortener/internal/models"
	"github.com/mdflamingo/url-shortener/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func setupRouter(t *testing.T, baseURL string, storage *repository.URLStorage) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	r.Get("/{id}", func(w http.ResponseWriter, req *http.Request) {
		handler.GetHandler(w, req, storage)
	})
	r.Post("/", func(w http.ResponseWriter, req *http.Request) {
		handler.PostHandler(w, req, baseURL, storage)
	})
	r.Post("/api/shorten", func(w http.ResponseWriter, req *http.Request) {
		handler.JSONPostHandler(w, req, baseURL, storage)
	})
	return r
}
func TestPostHandler(t *testing.T) {
	storage := repository.NewStorage()
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

			router := setupRouter(t, "http://localhost:8080", storage)
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
				origURL, exists := storage.Get(shortID)
				assert.True(t, exists)
				assert.Equal(t, tt.body, origURL)
			} else {
				assert.Equal(t, tt.wantBody, string(resBody))
			}
		})
	}
}

func TestGetHandler(t *testing.T) {
	storage := repository.NewStorage()
	testShortURL := "abc123"
	testOriginalURL := "https://example.com"
	storage.Save(testShortURL, testOriginalURL)

	tests := []struct {
		name           string
		path           string
		wantStatusCode int
		wantLocation   string
		wantBody       string
	}{
		{
			name:           "positive test - existing URL",
			path:           "/" + testShortURL,
			wantStatusCode: http.StatusTemporaryRedirect,
			wantLocation:   testOriginalURL,
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

			router := setupRouter(t, "http://localhost:8080", storage)
			router.ServeHTTP(w, request)

			res := w.Result()
			defer res.Body.Close()

			resBody, err := io.ReadAll(res.Body)
			require.NoError(t, err)

			assert.Equal(t, tt.wantStatusCode, res.StatusCode)

			if tt.wantStatusCode == http.StatusTemporaryRedirect {
				assert.Equal(t, tt.wantLocation, res.Header.Get("Location"))
			}

			if tt.wantStatusCode != http.StatusTemporaryRedirect {
				assert.Equal(t, tt.wantBody, string(resBody))
			}
		})
	}
}

func TestGetHandler_SpecialCharacters(t *testing.T) {
	storage := repository.NewStorage()
	request := httptest.NewRequest(http.MethodGet, "/test/url", nil)
	w := httptest.NewRecorder()

	router := setupRouter(t, "http://localhost:8080", storage)
	router.ServeHTTP(w, request)

	res := w.Result()
	defer res.Body.Close()

	assert.Equal(t, http.StatusNotFound, res.StatusCode)

	resBody, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.Equal(t, "404 page not found\n", string(resBody))
}

func TestJSONPostHandler(t *testing.T) {
	storage := repository.NewStorage()
	baseURL := "http://localhost:8080"

	tests := []struct {
		name         string
		method       string
		body         string
		contentType  string
		expectedCode int
		expectedBody string
		checkResult  bool
	}{
		{
			name:         "method_get_not_allowed",
			method:       http.MethodGet,
			contentType:  "application/json",
			expectedCode: http.StatusMethodNotAllowed,
			expectedBody: "",
		},
		{
			name:         "method_put_not_allowed",
			method:       http.MethodPut,
			contentType:  "application/json",
			expectedCode: http.StatusMethodNotAllowed,
			expectedBody: "",
		},
		{
			name:         "method_delete_not_allowed",
			method:       http.MethodDelete,
			contentType:  "application/json",
			expectedCode: http.StatusMethodNotAllowed,
			expectedBody: "",
		},
		{
			name:         "invalid_json",
			method:       http.MethodPost,
			body:         `{"url": "https://example.com"`,
			contentType:  "application/json",
			expectedCode: http.StatusBadRequest,
			expectedBody: "",
		},
		{
			name:         "empty_url_field",
			method:       http.MethodPost,
			body:         `{"url": ""}`,
			contentType:  "application/json",
			expectedCode: http.StatusBadRequest,
			expectedBody: "URL cannot be empty\n",
		},
		{
			name:         "missing_url_field",
			method:       http.MethodPost,
			body:         `{"other_field": "value"}`,
			contentType:  "application/json",
			expectedCode: http.StatusBadRequest,
			expectedBody: "",
		},
		{
			name:         "valid_url_success",
			method:       http.MethodPost,
			body:         `{"url": "https://example.com"}`,
			contentType:  "application/json",
			expectedCode: http.StatusOK,
			expectedBody: "",
			checkResult:  true,
		},
		{
			name:         "wrong_content_type",
			method:       http.MethodPost,
			body:         `{"url": "https://example.com"}`,
			contentType:  "text/plain",
			expectedCode: http.StatusUnsupportedMediaType,
			expectedBody: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, "/api/shorten", bytes.NewBufferString(tt.body))
			request.Header.Set("Content-Type", tt.contentType)

			w := httptest.NewRecorder()

			router := setupRouter(t, baseURL, storage)
			router.ServeHTTP(w, request)

			res := w.Result()
			defer res.Body.Close()

			resBody, err := io.ReadAll(res.Body)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedCode, res.StatusCode)

			if tt.expectedBody != "" {
				assert.Equal(t, tt.expectedBody, string(resBody))
			}

			if tt.checkResult && tt.expectedCode == http.StatusOK {
				var response models.Response
				err = json.Unmarshal(resBody, &response)
				require.NoError(t, err)

				assert.Contains(t, response.Result, baseURL)
				assert.NotEmpty(t, response.Result)

				parts := strings.Split(response.Result, "/")
				shortID := parts[len(parts)-1]
				originalURL, exists := storage.Get(shortID)
				assert.True(t, exists)
				assert.Equal(t, "https://example.com", originalURL)
			}
		})
	}
}
