package repository

import "errors"

var ErrURLExists = errors.New("ShortURL already exists")

type URLStorage struct {
	data map[string]string
}

func NewStorage() *URLStorage {
	return &URLStorage{
		data: make(map[string]string),
	}
}

func (s *URLStorage) Save(shortURL, origURL string) error {
	if _, ok := s.data[shortURL]; ok {
		return ErrURLExists
	}
	s.data[shortURL] = origURL
	return nil
}

func (s *URLStorage) Get(shortURL string) (string, bool) {
	origURL, exists := s.data[shortURL]
	return origURL, exists
}
