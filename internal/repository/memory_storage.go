package repository

import "errors"

var ErrURLExists = errors.New("short URL already exists")

type MemoryStorage struct {
	data map[string]string
}

func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		data: make(map[string]string),
	}
}

func (s *MemoryStorage) Save(shortURL, origURL string) error {
	if _, ok := s.data[shortURL]; ok {
		return ErrURLExists
	}
	s.data[shortURL] = origURL
	return nil
}

func (s *MemoryStorage) Get(shortURL string) (string, bool) {
	origURL, exists := s.data[shortURL]
	return origURL, exists
}

func (s *MemoryStorage) Close() error {
	return nil
}
