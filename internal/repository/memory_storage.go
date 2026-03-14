// Package repository содержит in-memory реализацию хранилища URL
//
// Простое хранилище на основе map[string]string для разработки и тестирования.
// НЕ поддерживает персистентность, удаление и фильтрацию по userID.
// Подходит только для демонстрации и unit-тестов.
//
// Ограничения:
// - Данные теряются при перезапуске
// - Нет поддержки userID (заглушки возвращают пустые результаты)
// - Нет удаления (всегда IsDeleted=false)
package repository

import (
	"context"
	"errors"

	"github.com/mdflamingo/url-shortener/internal/service"
)

// ErrURLExists возвращается при попытке сохранить уже существующий shortURL
var ErrURLExists = errors.New("short URL already exists")

// MemoryStorage - in-memory хранилище URL (map[string]string)
type MemoryStorage struct {
	data map[string]string // shortURL -> originalURL (userID игнорируется)
}

// NewMemoryStorage создает новое in-memory хранилище
//
// Инициализирует пустую map. Данные не сохраняются на диск.
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		data: make(map[string]string),
	}
}

// Save сохраняет короткую ссылку в память
//
// Проверяет уникальность shortURL.
// Возвращает ErrURLExists при конфликте.
// userID игнорируется (нет фильтрации по пользователям).
func (s *MemoryStorage) Save(shortURL, origURL, userID string) (string, error) {
	if _, ok := s.data[shortURL]; ok {
		return "", ErrURLExists
	}
	s.data[shortURL] = origURL
	return shortURL, nil
}

// SaveMany сохраняет несколько URL с регенерацией при конфликтах
//
// При ErrURLExists генерирует новый shortURL через GenerateShortURLForBatch.
// userID игнорируется.
func (s *MemoryStorage) SaveMany(urls []URLPair) ([]URLPair, error) {
	for i, url := range urls {
		for {
			_, err := s.Save(url.ShortURL, url.OriginalURL, url.UserID)
			if err != nil {
				if errors.Is(err, ErrURLExists) {
					urls[i].ShortURL = service.GenerateShortURLForBatch(url.OriginalURL)
					continue
				}
				return nil, err
			}
			break
		}
	}
	return urls, nil
}

// GetByUserID возвращает пустой список (заглушка)
//
// In-memory хранилище не поддерживает фильтрацию по userID.
func (s *MemoryStorage) GetByUserID(userID string) ([]URLPair, error) {
	return []URLPair{}, nil
}

// Delete возвращает nil канал (заглушка)
//
// In-memory хранилище не поддерживает удаление.
func (s *MemoryStorage) Delete(doneCh chan struct{}, inputCh chan string, userID string) chan error {
	return nil
}

// Get получает оригинальный URL по shortURL
//
// Возвращает origURL, exists=true, deleted=false (удаление не поддерживается).
func (s *MemoryStorage) Get(shortURL string) (string, bool, bool) {
	origURL, exists := s.data[shortURL]
	if !exists {
		return "", false, false
	}
	return origURL, true, false
}

// Close не делает ничего (in-memory)
func (s *MemoryStorage) Close() error {
	return nil
}

// Ping всегда успешно (in-memory хранилище всегда доступно)
func (s *MemoryStorage) Ping(ctx context.Context) error {
	return nil
}
