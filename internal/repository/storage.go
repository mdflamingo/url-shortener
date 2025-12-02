package repository

type URLStorage interface {
	Save(shortURL, originalURL string) error
	SaveMany(urls []URLPair) error
	Get(shortURL string) (string, bool)
	Close() error
}
