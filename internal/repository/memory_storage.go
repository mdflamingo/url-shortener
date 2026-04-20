// Package repository содержит in-memory реализацию хранилища URL
package repository

import (
	"context"
)

// MemoryStorage - in-memory хранилище URL
type MemoryStorage struct {
	data  map[string]string // shortURL -> originalURL
	index map[string]string // originalURL -> shortURL (для быстрого поиска дубликатов)
}

// NewMemoryStorage создает новое in-memory хранилище
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		data:  make(map[string]string),
		index: make(map[string]string),
	}
}

// Save сохраняет короткую ссылку в память
//
// Проверяет:
// 1. Существует ли уже такой оригинальный URL (возвращает ErrConflict + существующий shortURL)
// 2. Свободен ли shortURL (возвращает ErrURLExists если занят)
func (s *MemoryStorage) Save(shortURL, origURL, userID string) (string, error) {
	// Сначала проверяем, не существует ли уже такой оригинальный URL
	if existingShort, exists := s.index[origURL]; exists {
		// Возвращаем существующий короткий URL и ошибку конфликта
		return existingShort, ErrConflict
	}

	// Проверяем, не занят ли короткий URL
	if _, ok := s.data[shortURL]; ok {
		return "", ErrConflict
	}

	// Сохраняем новую пару
	s.data[shortURL] = origURL
	s.index[origURL] = shortURL

	return shortURL, nil
}

// SaveMany сохраняет несколько URL (shortURL должен быть уже сгенерирован)
func (s *MemoryStorage) SaveMany(urls []URLPair) ([]URLPair, error) {
	savedPairs := make([]URLPair, 0, len(urls))

	for _, url := range urls {
		// Проверяем, не существует ли уже оригинальный URL
		if existingShort, exists := s.index[url.OriginalURL]; exists {
			// Если существует, добавляем в результат существующую пару
			savedPairs = append(savedPairs, URLPair{
				ShortURL:    existingShort,
				OriginalURL: url.OriginalURL,
				UserID:      url.UserID,
			})
			continue
		}

		// Проверяем, не занят ли короткий URL
		if _, exists := s.data[url.ShortURL]; exists {
			// Если занят, возвращаем ошибку (сервис должен сгенерировать новый)
			return nil, ErrConflict
		}

		// Сохраняем
		s.data[url.ShortURL] = url.OriginalURL
		s.index[url.OriginalURL] = url.ShortURL

		savedPairs = append(savedPairs, URLPair{
			ShortURL:    url.ShortURL,
			OriginalURL: url.OriginalURL,
			UserID:      url.UserID,
		})
	}

	return savedPairs, nil
}

// GetByUserID возвращает все URL пользователя
func (s *MemoryStorage) GetByUserID(userID string) ([]URLPair, error) {
	// Для in-memory хранилища возвращаем все URL (в реальном приложении нужно фильтровать)
	pairs := make([]URLPair, 0, len(s.data))
	for shortURL, origURL := range s.data {
		pairs = append(pairs, URLPair{
			ShortURL:    shortURL,
			OriginalURL: origURL,
			UserID:      "", // userID не хранится в этой реализации
		})
	}
	return pairs, nil
}

// Delete возвращает nil канал (заглушка)
func (s *MemoryStorage) Delete(doneCh chan struct{}, inputCh chan string, userID string) chan error {
	resultCh := make(chan error)

	go func() {
		defer close(resultCh)

		for {
			select {
			case <-doneCh:
				return
			case shortURL, ok := <-inputCh:
				if !ok {
					return
				}
				// In-memory хранилище не поддерживает удаление,
				// но для совместимости просто пропускаем
				_ = shortURL
				resultCh <- nil
			}
		}
	}()

	return resultCh
}

// Get получает оригинальный URL по shortURL
func (s *MemoryStorage) Get(shortURL string) (string, bool, bool) {
	origURL, exists := s.data[shortURL]
	if !exists {
		return "", false, false
	}
	return origURL, true, false // deleted всегда false для in-memory
}

// GetStats возвращает количество сокращенных url и количество пользователей в сервисе
func (s *MemoryStorage) GetStats() (URLStats, error) {
	stats := URLStats{
		Urls:  len(s.data),
		Users: len(s.data),
	}

	return stats, nil
}

// Close не делает ничего
func (s *MemoryStorage) Close() error {
	return nil
}

// Ping всегда успешно
func (s *MemoryStorage) Ping(ctx context.Context) error {
	return nil
}
