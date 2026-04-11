// Package repository определяет интерфейс URLStorage и общие типы/ошибки
//
// URLStorage - единый интерфейс для всех хранилищ (file, memory, postgres).
// Обеспечивает абстракцию над разными реализациями (in-memory, file, DB).
package repository

import (
	"context"
	"errors"
)

// URLPair представляет пару короткий/оригинальный URL
//
// Используется во внешнем API и для передачи данных между слоями.
// UserID обязателен для реализации мультиарендности.
type URLPair struct {
	ShortURL    string // короткий идентификатор (генерируется service.GenerateShortURL)
	OriginalURL string // оригинальный длинный URL (валидируется url.Parse)
	UserID      string // уникальный ID пользователя (UUID из middleware)
}

// URLStats представляет количество сокращенных url и количество пользователей в сервисе
//
// Используется во внешнем API и для передачи данных между слоями.
type URLStats struct {
	Urls  int // количество сокращённых URL в сервисе
	Users int // количество пользователей в сервисе
}

// ErrConflict возвращается при конфликте данных (существующий full_url или short_url)
var ErrConflict = errors.New("data conflict")

// ErrNotFound возвращается когда shortURL не найден в хранилище
var ErrNotFound = errors.New("URL not found")

// ErrShortURLConflict возвращается при попытке сохранить уже существующий shortURL
var ErrShortURLConflict = errors.New("short URL conflict")

// URLStorage - основной интерфейс хранилища URL
//
// Все реализации (FileStorage, MemoryStorage, DBStorage) должны реализовывать этот интерфейс.
// Поддерживает конкурентный доступ, мультиарендность (userID), soft delete.
//
// Основные возможности:
//   - Save/SaveMany - сохранение с дедупликацией по full_url
//   - Get - получение с проверкой is_deleted
//   - GetByUserID - фильтрация по владельцу
//   - Delete - асинхронное батч удаление (soft delete)
//   - Ping/Close - healthcheck и cleanup
type URLStorage interface {
	// Save сохраняет короткую ссылку
	//
	// Возвращает сохраненный shortURL или ошибку:
	//   - ErrConflict / ErrShortURLConflict при дубликате
	//   - internal error при сбое хранилища
	Save(shortURL, originalURL, userID string) (string, error)

	// SaveMany сохраняет батч URL с автоматической регенерацией при конфликтах
	//
	// Гарантирует уникальность shortURL для каждого originalURL.
	SaveMany(urls []URLPair) ([]URLPair, error)

	// Get возвращает originalURL, found (true/false), deleted (true если soft deleted)
	//
	// deleted=true означает 410 Gone (ссылка удалена владельцем).
	Get(shortURL string) (originalURL string, found bool, deleted bool)

	// GetByUserID возвращает все активные URL пользователя (is_deleted=false)
	GetByUserID(userID string) ([]URLPair, error)

	// GetStats возвращает количество сокращенных url и количество пользователей в сервисе
	GetStats() (URLStats, error)

	// Delete асинхронно помечает батч URL как удаленные (soft delete)
	//
	// doneCh - сигнал остановки обработки
	// inputCh - канал shortURL для удаления
	// Возвращает канал ошибок (закрывается после обработки)
	Delete(doneCh chan struct{}, inputCh chan string, userID string) chan error

	// Close освобождает ресурсы хранилища (закрытие файлов/соединений)
	Close() error

	// Ping проверяет доступность хранилища (healthcheck)
	//
	// Используется в /ping эндпоинте.
	Ping(ctx context.Context) error
}
