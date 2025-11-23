package repository

type URLStorage interface {
	Save(shortURL, originalURL string) error
	Get(shortURL string) (string, bool)
	Close() error
}
