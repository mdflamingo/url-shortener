package repository

import (
	"context"
	"errors"

	"github.com/mdflamingo/url-shortener/internal/service"
)

var ErrURLExists = errors.New("short URL already exists")

type MemoryStorage struct {
	data map[string]string
}

func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		data: make(map[string]string),
	}
}

func (s *MemoryStorage) Save(shortURL, origURL string) (string, error) {
	if _, ok := s.data[shortURL]; ok {
		return "", ErrURLExists
	}
	s.data[shortURL] = origURL
	return shortURL, nil
}

func (s *MemoryStorage) SaveMany(urls []URLPair) ([]URLPair, error) {
	for i, url := range urls {
		for {
			_, err := s.Save(url.ShortURL, url.OriginalURL)
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

func (s *MemoryStorage) GetByUserID(userID string) ([]URLPair, error) {
	return []URLPair{}, nil
}

func (s *MemoryStorage) Delete(doneCh chan struct{}, inputCh chan string, userID string) chan error {
	return nil
}

func (s *MemoryStorage) Get(shortURL string) (string, bool) {
	origURL, exists := s.data[shortURL]
	return origURL, exists
}

func (s *MemoryStorage) Close() error {
	return nil
}
func (s *MemoryStorage) Ping(ctx context.Context) error {
	return nil
}
