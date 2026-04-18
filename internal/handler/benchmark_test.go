package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mdflamingo/url-shortener/internal/repository"
	"github.com/mdflamingo/url-shortener/internal/service"
	"go.uber.org/zap"
)

func setupTestService() *service.URLService {
	storage := repository.NewMemoryStorage()
	log, _ := zap.NewDevelopment()
	urlService := service.NewURLService(storage, "http://localhost:8080", nil, log)
	return urlService
}

// addUserIDToContext добавляет userID в контекст запроса (имитирует работу CookieMiddleware)
func addUserIDToContext(req *http.Request, userID string) *http.Request {
	ctx := context.WithValue(req.Context(), "userID", userID)
	return req.WithContext(ctx)
}

func BenchmarkPostHandler(b *testing.B) {
	urlService := setupTestService()
	url := "https://example.com"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		body := bytes.NewBufferString(url)
		req := httptest.NewRequest(http.MethodPost, "/", body)
		req.Header.Set("Content-Type", "text/plain")

		req = addUserIDToContext(req, "test-user-123")

		w := httptest.NewRecorder()
		b.StartTimer()

		PostHandler(w, req, urlService)
	}
}

func BenchmarkJSONPostHandler(b *testing.B) {
	urlService := setupTestService()
	jsonBody := `{"url":"https://example.com"}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		body := bytes.NewBufferString(jsonBody)
		req := httptest.NewRequest(http.MethodPost, "/api/shorten", body)
		req.Header.Set("Content-Type", "application/json")

		req = addUserIDToContext(req, "test-user-123")

		w := httptest.NewRecorder()
		b.StartTimer()

		JSONPostHandler(w, req, urlService)
	}
}

func BenchmarkGetHandler(b *testing.B) {
	urlService := setupTestService()

	shortURL, _, err := urlService.CreateShortURL(context.Background(), "https://example.com", "test-user-123")
	if err != nil {
		b.Fatalf("Failed to create test URL: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		req := httptest.NewRequest(http.MethodGet, "/"+shortURL, nil)
		req = addUserIDToContext(req, "test-user-123")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", shortURL)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		b.StartTimer()

		GetHandler(w, req, urlService)
	}
}

func BenchmarkBatchHandler(b *testing.B) {
	urlService := setupTestService()
	jsonBody := `[
		{"correlation_id": "1", "original_url": "https://example1.com"},
		{"correlation_id": "2", "original_url": "https://example2.com"}
	]`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		body := bytes.NewBufferString(jsonBody)
		req := httptest.NewRequest(http.MethodPost, "/api/shorten/batch", body)
		req.Header.Set("Content-Type", "application/json")

		req = addUserIDToContext(req, "test-user-123")

		w := httptest.NewRecorder()
		b.StartTimer()

		BatchHandler(w, req, urlService)
	}
}

func BenchmarkGetUserURLsHandler(b *testing.B) {
	urlService := setupTestService()

	for i := 0; i < 10; i++ {
		urlService.CreateShortURL(context.Background(), "https://example.com", "test-user-123")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		req := httptest.NewRequest(http.MethodGet, "/api/user/urls", nil)

		req = addUserIDToContext(req, "test-user-123")

		w := httptest.NewRecorder()
		b.StartTimer()

		GetUserURLSHandler(w, req, urlService)
	}
}

func BenchmarkDeleteUserURLsHandler(b *testing.B) {
	urlService := setupTestService()

	shortURLs := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		shortURL, _, err := urlService.CreateShortURL(context.Background(), "https://example.com", "test-user-123")
		if err != nil {
			b.Fatalf("Failed to create test URL: %v", err)
		}
		shortURLs = append(shortURLs, shortURL)
	}

	jsonBody, _ := json.Marshal(shortURLs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		body := bytes.NewBuffer(jsonBody)
		req := httptest.NewRequest(http.MethodDelete, "/api/user/urls", body)
		req.Header.Set("Content-Type", "application/json")

		req = addUserIDToContext(req, "test-user-123")

		w := httptest.NewRecorder()
		b.StartTimer()

		DeleteUserURLSHandler(w, req, urlService)
	}
}

func BenchmarkGetStatsHandler(b *testing.B) {
	urlService := setupTestService()
	for i := 0; i < 100; i++ {
		urlService.CreateShortURL(context.Background(), "https://example.com", "test-user-123")
	}

	trustedSubnet := "192.168.1.0/24"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		req := httptest.NewRequest(http.MethodGet, "/api/internal/stats", nil)
		req.Header.Set("X-Real-IP", "192.168.1.100")

		w := httptest.NewRecorder()
		b.StartTimer()

		GetStatsHandler(w, req, urlService, trustedSubnet)
	}
}
