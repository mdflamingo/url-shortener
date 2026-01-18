package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

func setupRouter(t *testing.T, baseURL string, storage *repository.FileStorage) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	cookieSecret := "test-secret-key"
	cookieMiddleware := NewSignedCookieMiddleware(cookieSecret)
	r.Use(gzipMiddleware)
	r.Use(cookieMiddleware.CookieMiddleware)

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

func createTestStorage(t *testing.T) *repository.FileStorage {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "test-storage-*.json")
	require.NoError(t, err)
	tmpFile.Close()

	storage, err := repository.NewFileStorage(tmpFile.Name())
	require.NoError(t, err)

	t.Cleanup(func() {
		os.Remove(tmpFile.Name())
	})

	return storage
}

func TestPostHandler(t *testing.T) {
	storage := createTestStorage(t)
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
	storage := createTestStorage(t)
	testShortURL := "abc123"
	testOriginalURL := "https://example.com"
	testUserID := "abc-abc"
	_, err := storage.Save(testShortURL, testOriginalURL, testUserID)
	require.NoError(t, err)

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
	storage := createTestStorage(t)
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
	storage := createTestStorage(t)
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
			expectedCode: http.StatusCreated,
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

			if tt.checkResult && tt.expectedCode == http.StatusCreated {
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

func TestGzipCompression(t *testing.T) {
	storage := createTestStorage(t)
	baseURL := "http://localhost:8080"

	router := setupRouter(t, baseURL, storage)

	srv := httptest.NewServer(router)
	defer srv.Close()

	requestBody := `{"url": "https://example.com"}`
	t.Run("sends_gzip_request", func(t *testing.T) {
		buf := bytes.NewBuffer(nil)
		zb := gzip.NewWriter(buf)
		_, err := zb.Write([]byte(requestBody))
		require.NoError(t, err)
		err = zb.Close()
		require.NoError(t, err)

		r := httptest.NewRequest("POST", srv.URL+"/api/shorten", buf)
		r.RequestURI = ""
		r.Header.Set("Content-Encoding", "gzip")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept-Encoding", "")

		resp, err := http.DefaultClient.Do(r)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, resp.StatusCode)

		defer resp.Body.Close()

		b, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var response models.Response
		err = json.Unmarshal(b, &response)
		require.NoError(t, err)

		assert.Contains(t, response.Result, baseURL)
		assert.NotEmpty(t, response.Result)

		parts := strings.Split(response.Result, "/")
		shortID := parts[len(parts)-1]
		originalURL, exists := storage.Get(shortID)
		assert.True(t, exists)
		assert.Equal(t, "https://example.com", originalURL)
	})

	t.Run("accepts_gzip_response", func(t *testing.T) {
		buf := bytes.NewBufferString(requestBody)
		r := httptest.NewRequest("POST", srv.URL+"/api/shorten", buf)
		r.RequestURI = ""
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept-Encoding", "gzip")

		resp, err := http.DefaultClient.Do(r)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, resp.StatusCode)

		defer resp.Body.Close()

		assert.Equal(t, "gzip", resp.Header.Get("Content-Encoding"))

		zr, err := gzip.NewReader(resp.Body)
		require.NoError(t, err)
		defer zr.Close()

		b, err := io.ReadAll(zr)
		require.NoError(t, err)

		var response models.Response
		err = json.Unmarshal(b, &response)
		require.NoError(t, err)

		assert.Contains(t, response.Result, baseURL)
		assert.NotEmpty(t, response.Result)
	})

	t.Run("sends_and_accepts_gzip", func(t *testing.T) {
		buf := bytes.NewBuffer(nil)
		zb := gzip.NewWriter(buf)
		_, err := zb.Write([]byte(requestBody))
		require.NoError(t, err)
		err = zb.Close()
		require.NoError(t, err)

		r := httptest.NewRequest("POST", srv.URL+"/api/shorten", buf)
		r.RequestURI = ""
		r.Header.Set("Content-Encoding", "gzip")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept-Encoding", "gzip")

		resp, err := http.DefaultClient.Do(r)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, resp.StatusCode)

		defer resp.Body.Close()

		assert.Equal(t, "gzip", resp.Header.Get("Content-Encoding"))

		zr, err := gzip.NewReader(resp.Body)
		require.NoError(t, err)
		defer zr.Close()

		b, err := io.ReadAll(zr)
		require.NoError(t, err)

		var response models.Response
		err = json.Unmarshal(b, &response)
		require.NoError(t, err)

		assert.Contains(t, response.Result, baseURL)
		assert.NotEmpty(t, response.Result)
	})

	t.Run("plain_request_plain_response", func(t *testing.T) {
		buf := bytes.NewBufferString(requestBody)
		r := httptest.NewRequest("POST", srv.URL+"/api/shorten", buf)
		r.RequestURI = ""
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept-Encoding", "")

		resp, err := http.DefaultClient.Do(r)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, resp.StatusCode)

		defer resp.Body.Close()

		assert.Empty(t, resp.Header.Get("Content-Encoding"))

		b, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var response models.Response
		err = json.Unmarshal(b, &response)
		require.NoError(t, err)

		assert.Contains(t, response.Result, baseURL)
		assert.NotEmpty(t, response.Result)
	})
}
