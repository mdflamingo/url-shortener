// Package service предоставляет бизнес-логику приложения URL Shortener
//
// Пакет содержит сервисы для:
//   - Генерации коротких URL
//   - Аудита действий пользователей с поддержкой множественных наблюдателей
//   - Наблюдателей для аудита (файл, HTTP)
package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
)

// AuditEvent представляет структуру события для системы аудита
//
// Событие содержит всю необходимую информацию о действии пользователя:
//   - TS: временная метка события (Unix timestamp)
//   - Action: тип выполненного действия ("shorten", "follow" и т.д.)
//   - UserID: идентификатор пользователя, совершившего действие
//   - URL: URL, с которым связано действие (исходный или сокращенный)
//
// Пример:
//
//	event := AuditEvent{
//	    TS:     time.Now().Unix(),
//	    Action: "shorten",
//	    UserID: "user123",
//	    URL:    "https://example.com",
//	}
type AuditEvent struct {
	TS     int64  `json:"ts"`      // Временная метка в формате Unix timestamp
	Action string `json:"action"`  // Тип действия (shorten, follow, delete и т.д.)
	UserID string `json:"user_id"` // Идентификатор пользователя
	URL    string `json:"url"`     // Задействованный URL
}

// Observer определяет интерфейс для всех наблюдателей системы аудита
//
// Все типы наблюдателей должны реализовывать этот интерфейс.
// Паттерн Observer позволяет легко добавлять новые способы обработки событий
// без изменения существующего кода.
type Observer interface {
	// OnAudit вызывается при каждом событии аудита
	OnAudit(event AuditEvent)
}

// FileObserver реализует запись событий аудита в файл
//
// Особенности:
//   - Потокобезопасная запись с использованием RWMutex
//   - Автоматическое создание файла при инициализации
//   - Запись в формате JSON (по одному событию на строку)
//   - Ошибки логируются через стандартный логгер

type FileObserver struct {
	file *os.File     // Файл для записи аудита
	mu   sync.RWMutex // Мьютекс для потокобезопасной записи
}

// NewFileObserver создает новый наблюдатель для записи в файл
//
// Параметры:
//   - path: путь к файлу для записи аудита
//
// Возвращает:
//   - *FileObserver: инициализированный наблюдатель
//   - error: ошибка при открытии или создании файла
//
// Особенности:
//   - Если path пустой, возвращает (nil, nil) - наблюдатель не создается
//   - Файл открывается в режиме добавления (append) с созданием при необходимости
//   - Права доступа к файлу: 0644 (rw-r--r--)
func NewFileObserver(path string) (*FileObserver, error) {
	if path == "" {
		return nil, nil
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open/create log file: %w", err)
	}

	return &FileObserver{file: file}, nil
}

// OnAudit записывает событие аудита в файл
// Особенности реализации:
//   - Потокобезопасная запись с блокировкой на время записи
//   - Событие сериализуется в JSON
//   - Каждое событие записывается с новой строки
//   - Ошибки сериализации и записи логируются через log.Printf
//   - Метод не возвращает ошибку, а только логирует её
func (f *FileObserver) OnAudit(event AuditEvent) {
	if f == nil || f.file == nil {
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("AUDIT FILE ERROR Failed to marshal: %v", err)
		return
	}

	data = append(data, '\n')
	if _, err := f.file.Write(data); err != nil {
		log.Printf("AUDIT FILE ERROR Failed to write: %v", err)
	}
}

// APIObserver реализует отправку событий аудита на внешний HTTP API
//
// Особенности:
//   - Отправка POST-запросов с JSON-телом
//   - Асинхронная отправка (fire-and-forget)
//   - Проверка HTTP статуса ответа (ожидается 200 OK)
//   - Ошибки логируются через стандартный логгер
type APIObserver struct {
	url string // URL внешнего API для отправки событий
}

// NewAPIObserver создает новый наблюдатель для отправки на внешний API
// Особенности:
//   - Если url пустой, возвращает (nil, nil) - наблюдатель не создается
//   - Валидация URL не выполняется, ошибки возникнут при отправке
func NewAPIObserver(url string) (*APIObserver, error) {
	if url == "" {
		return nil, nil
	}

	return &APIObserver{url: url}, nil
}

// OnAudit отправляет событие аудита на внешний API
// Особенности реализации:
//   - Отправляет POST-запрос с Content-Type: application/json
//   - Тело запроса содержит JSON-представление события
//   - Проверяет, что статус ответа равен 200 OK
//   - Все ошибки (сеть, статус ответа) логируются через log.Printf
//   - Response body закрывается автоматически через defer
//   - Метод не блокирует выполнение программы при ошибках (fire-and-forget)
func (h *APIObserver) OnAudit(event AuditEvent) {
	if h == nil || h.url == "" {
		return
	}

	jsonData, err := json.Marshal(event)
	if err != nil {
		log.Printf("AUDIT HTTP ERROR Marshal error: %v", err)
		return
	}

	resp, err := http.Post(h.url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("AUDIT HTTP ERROR Request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("AUDIT HTTP ERROR Remote server returned status: %d", resp.StatusCode)
	}
}

// AuditService реализует паттерн Observer для системы аудита
//
// Сервис позволяет динамически добавлять и удалять наблюдателей,
// которые получают уведомления о всех событиях аудита.
// Особенности:
//   - Потокобезопасное добавление наблюдателей и рассылку уведомлений
//   - Поддержка множественных наблюдателей разных типов
//   - "Fire-and-forget" семантика - ошибки в наблюдателях не влияют на других
//
// Пример использования:
//
//	auditService := NewAuditService()
//	auditService.Attach(fileObserver)
//	auditService.Attach(apiObserver)
//	auditService.Notify(AuditEvent{Action: "shorten", UserID: "user123", ...})
type AuditService struct {
	observers []Observer   // Список зарегистрированных наблюдателей
	mu        sync.RWMutex // Мьютекс для потокобезопасного доступа
}

// NewAuditService создает новый экземпляр сервиса аудита
func NewAuditService() *AuditService {
	return &AuditService{
		observers: make([]Observer, 0),
	}
}

// Attach добавляет нового наблюдателя в систему аудита
// Особенности:
//   - Потокобезопасная операция
//   - После добавления наблюдатель будет получать все последующие события
//   - Можно добавлять наблюдателей динамически во время работы приложения
func (a *AuditService) Attach(obs Observer) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.observers = append(a.observers, obs)
}

// Notify уведомляет всех наблюдателей о событии аудита
// Особенности:
//   - Потокобезопасная операция (блокировка на чтение)
//   - Все наблюдатели получают уведомление одновременно (в цикле)
//   - Ошибки в одном наблюдателе не влияют на другие (каждый OnAudit вызывается независимо)
//   - Метод не ожидает завершения обработки (fire-and-forget)
func (a *AuditService) Notify(event AuditEvent) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	for _, obs := range a.observers {
		obs.OnAudit(event)
	}
}
