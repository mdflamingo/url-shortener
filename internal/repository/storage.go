package repository

import (
	"context"
	"errors"
)

var ErrConflict = errors.New("data conflict")
var ErrNotFound = errors.New("URL not found")
var ErrShortURLConflict = errors.New("short URL conflict")

type URLStorage interface {
	Save(shortURL, originalURL string) (string, error)
	SaveMany(urls []URLPair) ([]URLPair, error)
	Get(shortURL string) (string, bool)
	GetByUserID(userID string) ([]URLPair, error)
	Delete(doneCh chan struct{}, inputCh chan string, userID string) (chan error)
	Close() error
	Ping(ctx context.Context) error
}
