package handler_test

import (
	"net/http/httptest"
	"strings"

	"github.com/mdflamingo/url-shortener/internal/handler"
	"github.com/mdflamingo/url-shortener/internal/repository"
)

func ExamplePostHandler() {
	// Создаем тестовый запрос
	req := httptest.NewRequest("POST", "/", strings.NewReader("https://example.com"))
	req.Header.Set("Content-Type", "text/plain")

	w := httptest.NewRecorder()
	storage := repository.NewMemoryStorage()

	// Вызываем обработчик
	handler.PostHandler(w, req, "http://localhost:8080", storage, nil)

	// Результат: статус 201 и короткий URL в теле ответа
}
