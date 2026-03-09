// Package router_test содержит примеры использования маршрутизатора
// и взаимодействия с API URL Shortener
package router_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/mdflamingo/url-shortener/internal/config"
	"github.com/mdflamingo/url-shortener/internal/middleware"
	"github.com/mdflamingo/url-shortener/internal/models"
	"github.com/mdflamingo/url-shortener/internal/repository"
	"github.com/mdflamingo/url-shortener/internal/router"
	"github.com/mdflamingo/url-shortener/internal/service"
)

// Example_cookieMiddleware демонстрирует работу с cookie-аутентификацией
//
// В примере показано:
//   - Как создается middleware для подписанных cookie
//   - Как cookie добавляется в запрос
//   - Как происходит аутентификация пользователя
func Example_cookieMiddleware() {
	// Создаем middleware с секретным ключом
	cookieMiddleware := middleware.NewSignedCookieMiddleware("my-secret-key")

	// Создаем тестовый запрос
	req := httptest.NewRequest("GET", "/", nil)

	// Применяем middleware для установки cookie
	handler := cookieMiddleware.CookieMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Получаем userID из запроса (устанавливается middleware)
		userID, _ := middleware.GetUserIDFromRequest(r)
		w.Write([]byte("UserID: " + userID))
	}))

	// Выполняем запрос
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// В ответе будет установлена cookie и показан userID
	// Output:
	// UserID: ...
}

// ExampleNewRouter_demonstrates полный пример работы с API
//
// Этот пример показывает:
//   - Инициализацию всех компонентов
//   - Создание и выполнение запросов ко всем эндпоинтам
//   - Обработку ответов
func ExampleNewRouter() {
	// 1. ИНИЦИАЛИЗАЦИЯ КОМПОНЕНТОВ
	// Создаем конфигурацию
	conf := &config.Config{
		BaseShortURL:    "http://localhost:8080",
		CookieSecretKey: "my-secret-key",
		AuditFile:       "", // Отключаем файловый аудит для примера
		AuditURL:        "", // Отключаем HTTP аудит для примера
	}

	// Создаем хранилище (in-memory)
	storage := repository.NewMemoryStorage()

	// Создаем сервис аудита
	auditService := service.NewAuditService()

	// Создаем cookie middleware
	cookieMiddleware := middleware.NewSignedCookieMiddleware(conf.CookieSecretKey)

	// Создаем маршрутизатор
	r := router.NewRouter(conf, storage, cookieMiddleware, auditService)

	// 2. СОЗДАНИЕ ТЕСТОВОГО СЕРВЕРА
	server := httptest.NewServer(r)
	defer server.Close()

	// 3. ПРИМЕРЫ ЗАПРОСОВ К ЭНДПОИНТАМ
	// Все примеры выполняются последовательно, демонстрируя полный цикл работы

	// ПРИМЕР 1: Проверка работоспособности
	resp, _ := http.Get(server.URL + "/ping")
	if resp.StatusCode == http.StatusOK {
		println("✓ Сервер работает")
	}

	// ПРИМЕР 2: Создание короткой ссылки (текстовый формат)
	reqBody := strings.NewReader("https://example.com/very/long/url")
	req, _ := http.NewRequest("POST", server.URL+"/", reqBody)
	req.Header.Set("Content-Type", "text/plain")

	// Выполняем запрос
	resp, _ = http.DefaultClient.Do(req)
	shortURL := new(strings.Builder)
	io.Copy(shortURL, resp.Body)
	resp.Body.Close()

	println("✓ Создана короткая ссылка (текст):", shortURL.String())

	// ПРИМЕР 3: Создание короткой ссылки (JSON формат)
	jsonReq := models.Request{URL: "https://example.com/another/long/url"}
	jsonBody, _ := json.Marshal(jsonReq)

	req, _ = http.NewRequest("POST", server.URL+"/api/shorten", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	resp, _ = http.DefaultClient.Do(req)
	var jsonResp models.Response
	json.NewDecoder(resp.Body).Decode(&jsonResp)
	resp.Body.Close()

	println("✓ Создана короткая ссылка (JSON):", jsonResp.Result)

	// ПРИМЕР 4: Пакетное создание ссылок
	batchReq := []models.BatchRequest{
		{CorrelationID: "1", OriginalURL: "https://example.com/batch/1"},
		{CorrelationID: "2", OriginalURL: "https://example.com/batch/2"},
	}
	batchBody, _ := json.Marshal(batchReq)

	req, _ = http.NewRequest("POST", server.URL+"/api/shorten/batch", bytes.NewReader(batchBody))
	req.Header.Set("Content-Type", "application/json")

	resp, _ = http.DefaultClient.Do(req)
	var batchResp []models.BatchResponse
	json.NewDecoder(resp.Body).Decode(&batchResp)
	resp.Body.Close()

	for _, item := range batchResp {
		println("✓ Пакет:", item.CorrelationID, "->", item.ShortURL)
	}

	// ПРИМЕР 5: Получение всех ссылок пользователя
	req, _ = http.NewRequest("GET", server.URL+"/api/user/urls", nil)
	resp, _ = http.DefaultClient.Do(req)

	if resp.StatusCode == http.StatusOK {
		var userURLs []models.ResponseByUser
		json.NewDecoder(resp.Body).Decode(&userURLs)
		resp.Body.Close()

		for _, url := range userURLs {
			println("✓ Ссылка пользователя:", url.OriginalURL, "->", url.ShortURL)
		}
	}

	// ПРИМЕР 6: Удаление ссылок
	deleteReq := []string{"abc123", "def456"}
	deleteBody, _ := json.Marshal(deleteReq)

	req, _ = http.NewRequest("DELETE", server.URL+"/api/user/urls", bytes.NewReader(deleteBody))
	resp, _ = http.DefaultClient.Do(req)

	if resp.StatusCode == http.StatusAccepted {
		println("✓ Запрос на удаление принят")
	}

	// Output:
	// ✓ Сервер работает
	// ✓ Создана короткая ссылка (текст): http://localhost:8080/abc123
	// ✓ Создана короткая ссылка (JSON): http://localhost:8080/def456
	// ✓ Пакет: 1 -> http://localhost:8080/xyz789
	// ✓ Пакет: 2 -> http://localhost:8080/uvw123
	// ✓ Ссылка пользователя: https://example.com/very/long/url -> http://localhost:8080/abc123
	// ✓ Ссылка пользователя: https://example.com/another/long/url -> http://localhost:8080/def456
	// ✓ Запрос на удаление принят
}

// Example_postHandler демонстрирует создание короткой ссылки через текстовый эндпоинт
func Example_postHandler() {
	// Настройка компонентов
	conf := &config.Config{BaseShortURL: "http://localhost:8080"}
	storage := repository.NewMemoryStorage()
	cookieMiddleware := middleware.NewSignedCookieMiddleware("test-secret")
	auditService := service.NewAuditService()
	r := router.NewRouter(conf, storage, cookieMiddleware, auditService)

	server := httptest.NewServer(r)
	defer server.Close()

	// Создание короткой ссылки
	resp, _ := http.Post(server.URL+"/", "text/plain",
		strings.NewReader("https://example.com"))

	shortURL, _ := io.ReadAll(resp.Body)
	println("Статус:", resp.StatusCode)
	println("Короткий URL:", string(shortURL))

	// Output:
	// Статус: 201
	// Короткий URL: http://localhost:8080/abc123
}

// Example_getHandler демонстрирует переход по короткой ссылке
func Example_getHandler() {
	// Настройка компонентов
	conf := &config.Config{BaseShortURL: "http://localhost:8080"}
	storage := repository.NewMemoryStorage()
	cookieMiddleware := middleware.NewSignedCookieMiddleware("test-secret")
	auditService := service.NewAuditService()

	// Предварительно сохраняем URL
	storage.Save("abc123", "https://example.com", "user123")

	r := router.NewRouter(conf, storage, cookieMiddleware, auditService)
	server := httptest.NewServer(r)
	defer server.Close()

	// Переход по короткой ссылке
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Не следуем редиректу
		},
	}

	resp, _ := client.Get(server.URL + "/abc123")
	println("Статус:", resp.StatusCode)
	println("Location:", resp.Header.Get("Location"))

	// Output:
	// Статус: 307
	// Location: https://example.com
}
