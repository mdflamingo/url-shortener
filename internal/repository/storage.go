package repository

import "errors"

var ErrConflict = errors.New("data conflict")

type URLStorage interface {
	Save(shortURL, originalURL string) (string, error)
	SaveMany(urls []URLPair) error
	Get(shortURL string) (string, bool)
	Close() error
}
