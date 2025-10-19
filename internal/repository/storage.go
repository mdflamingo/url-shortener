package repository

type URLStorage struct {
	data map[string]string
}

func NewStorage() *URLStorage {
	return &URLStorage{
		data: make(map[string]string),
	}
}

func (s *URLStorage) Save(shortURL, origURL string) {
	s.data[shortURL] = origURL
}

func (s *URLStorage) Get(shortURL string) (string, bool) {
	origURL, exists := s.data[shortURL]
	return origURL, exists
}

func (s *URLStorage) Exists(shortURL string) (string, bool) {
	origURL, exists := s.data[shortURL]
	return origURL, exists
}
