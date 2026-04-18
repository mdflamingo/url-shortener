package handler_test

import (
	"net/http/httptest"
	"strings"

	"github.com/mdflamingo/url-shortener/internal/handler"
	"github.com/mdflamingo/url-shortener/internal/repository"
	"github.com/mdflamingo/url-shortener/internal/service"
	"go.uber.org/zap"
)

func ExamplePostHandler() {
	// Создаем тестовый запрос
	req := httptest.NewRequest("POST", "/", strings.NewReader("https://example.com"))
	req.Header.Set("Content-Type", "text/plain")

	w := httptest.NewRecorder()
	storage := repository.NewMemoryStorage()
	auditService := service.NewAuditService()
	urlService := service.NewURLService(storage, "http://localhost:8080", auditService, zap.NewNop())

	// Вызываем обработчик
	handler.PostHandler(w, req, urlService)

	// Результат: статус 201 и короткий URL в теле ответа
}
