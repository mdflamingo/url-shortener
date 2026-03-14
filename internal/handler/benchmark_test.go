package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mdflamingo/url-shortener/internal/repository"
)

func BenchmarkPostHandler(b *testing.B) {
	storage := repository.NewMemoryStorage()
	url := "https://example.com"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		body := bytes.NewBufferString(url)
		req := httptest.NewRequest(http.MethodPost, "/", body)
		req.Header.Set("Content-Type", "text/plain")
		w := httptest.NewRecorder()
		b.StartTimer()

		PostHandler(w, req, "http://localhost:8080", storage, nil)
	}
}

func BenchmarkJSONPostHandler(b *testing.B) {
	storage := repository.NewMemoryStorage()
	jsonBody := `{"url":"https://example.com"}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		body := bytes.NewBufferString(jsonBody)
		req := httptest.NewRequest(http.MethodPost, "/api/shorten", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		b.StartTimer()

		JSONPostHandler(w, req, "http://localhost:8080", storage, nil)
	}
}

func BenchmarkGetHandler(b *testing.B) {
	storage := repository.NewMemoryStorage()
	// Сначала сохраняем URL
	shortURL, _ := GenerateAndSaveShortURL("https://example.com", storage, "test-user")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		req := httptest.NewRequest(http.MethodGet, "/"+shortURL, nil)
		w := httptest.NewRecorder()
		b.StartTimer()

		GetHandler(w, req, storage, nil)
	}
}

func BenchmarkBatchHandler(b *testing.B) {
	storage := repository.NewMemoryStorage()
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
		w := httptest.NewRecorder()
		b.StartTimer()

		BatchHandler(w, req, "http://localhost:8080", storage)
	}
}

func BenchmarkGenerateAndSaveShortURL(b *testing.B) {
	storage := repository.NewMemoryStorage()
	url := "https://example.com"
	userID := "test-user"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GenerateAndSaveShortURL(url, storage, userID)
	}
}
