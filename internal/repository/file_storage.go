// Package repository реализует файловое хранилище URL на основе JSONL формата
//
// Хранит URL в виде JSON объектов построчно (JSONL) в указанном файле.
// Поддерживает конкурентный доступ (sync.RWMutex), soft delete (IsDeleted флаг),
// батч операции и асинхронное удаление.
//
// Формат записи:
//
//	{"short_url":"abc123","original_url":"https://example.com","user_id":"uuid","is_deleted":false}\n
package repository

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/mdflamingo/url-shortener/internal/service"
)

// FileStorage - файловое хранилище URL с поддержкой конкурентного доступа
type FileStorage struct {
	producer *Producer    // producer для записи (append-only)
	filename string       // путь к файлу хранилища
	mu       sync.RWMutex // mutex для синхронизации доступа
}

// URL - структура хранения URL в файле (JSONL формат)
type URL struct {
	ShortURL    string `json:"short_url"`    // короткий идентификатор
	OriginalURL string `json:"original_url"` // оригинальный URL
	UserID      string `json:"user_id"`      // ID пользователя-владельца
	IsDeleted   bool   `json:"is_deleted"`   // флаг логического удаления
}

// Producer - компонент для append-only записи URL в файл
type Producer struct {
	file   *os.File      // файловый дескриптор
	writer *bufio.Writer // буферизованный writer
}

// Consumer - компонент для последовательного чтения URL из файла
type Consumer struct {
	file   *os.File      // файловый дескриптор
	reader *bufio.Reader // буферизованный reader
}

// NewFileStorage создает новое файловое хранилище
//
// filename - путь к файлу (создается автоматически при необходимости).
//
// Примечание: файл открывается в режиме O_APPEND для безопасной конкурентной записи.
func NewFileStorage(filename string) (*FileStorage, error) {
	producer, err := NewProducer(filename)
	if err != nil {
		return nil, err
	}

	storage := &FileStorage{
		producer: producer,
		filename: filename,
	}

	return storage, nil
}

// Save сохраняет новую короткую ссылку или возвращает ошибку конфликта
//
// Проверяет уникальность shortURL перед сохранением.
// Возвращает ErrConflict если shortURL уже существует.
func (fs *FileStorage) Save(shortURL, originalURL, userID string) (string, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	exists, err := fs.checkExists(shortURL)
	if err != nil {
		return "", fmt.Errorf("failed to check URL existence: %w", err)
	}
	if exists {
		return "", fmt.Errorf("short URL already exists: %s", shortURL)
	}

	url := &URL{
		ShortURL:    shortURL,
		OriginalURL: originalURL,
		UserID:      userID,
		IsDeleted:   false,
	}

	if err := fs.producer.WriteURL(url); err != nil {
		return "", fmt.Errorf("failed to write URL to file: %w", err)
	}

	return url.ShortURL, nil
}

// SaveMany сохраняет несколько URL с автоматической генерацией при конфликтах
func (fs *FileStorage) SaveMany(urls []URLPair) ([]URLPair, error) {
	for i, url := range urls {
		for {
			_, err := fs.Save(url.ShortURL, url.OriginalURL, url.UserID)
			if err != nil {
				if errors.Is(err, ErrConflict) {
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

// GetByUserID возвращает все активные URL пользователя
//
// Игнорирует удаленные (IsDeleted=true) ссылки.
func (fs *FileStorage) GetByUserID(userID string) ([]URLPair, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	urlMap := fs.loadURLMap()
	result := make([]URLPair, 0)
	for shortURL, url := range urlMap {
		if url.UserID == userID && !url.IsDeleted {
			result = append(result, URLPair{
				ShortURL:    shortURL,
				OriginalURL: url.OriginalURL,
				UserID:      url.UserID,
			})
		}
	}
	return result, nil
}

// Delete асинхронно помечает URL как удаленные (soft delete)
//
// Принимает каналы для graceful shutdown и обработки ошибок.
// Записывает запись с IsDeleted=true в конец файла.
func (fs *FileStorage) Delete(doneCh chan struct{}, inputCh chan string, userID string) chan error {
	errCh := make(chan error, 1)

	go func() {
		defer close(errCh)

		for {
			select {
			case <-doneCh:
				return
			case shortURL, ok := <-inputCh:
				if !ok {
					return
				}

				fs.mu.Lock()
				urlMap := fs.loadURLMap()
				if url, exists := urlMap[shortURL]; exists && url.UserID == userID {
					deleteURL := &URL{
						ShortURL:    shortURL,
						OriginalURL: url.OriginalURL,
						UserID:      url.UserID,
						IsDeleted:   true,
					}
					if err := fs.producer.WriteURL(deleteURL); err != nil {
						errCh <- fmt.Errorf("failed to mark URL as deleted: %w", err)
					}
				}
				fs.mu.Unlock()
			}
		}
	}()

	return errCh
}

// Get получает оригинальный URL по короткому идентификатору
//
// Возвращает originalURL, found (true/false), deleted (true если IsDeleted).
func (fs *FileStorage) Get(shortURL string) (string, bool, bool) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	urlMap := fs.loadURLMap()
	if url, exists := urlMap[shortURL]; exists {
		return url.OriginalURL, true, url.IsDeleted
	}
	return "", false, false
}

// Close закрывает файловый дескриптор хранилища
func (fs *FileStorage) Close() error {
	if fs.producer != nil {
		return fs.producer.Close()
	}
	return nil
}

// checkExists проверяет существование shortURL (внутренний метод)
func (fs *FileStorage) checkExists(shortURL string) (bool, error) {
	urlMap := fs.loadURLMap()
	_, exists := urlMap[shortURL]
	return exists, nil
}

// loadURLMap загружает весь файл в память как map[shortURL]URL (внутренний метод)
func (fs *FileStorage) loadURLMap() map[string]URL {
	urlMap := make(map[string]URL)

	file, err := os.OpenFile(fs.filename, os.O_RDONLY|os.O_CREATE, 0666)
	if err != nil {
		return urlMap
	}
	defer file.Close()

	reader := bufio.NewReader(file)

	for {
		data, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			continue
		}

		if len(data) == 0 || (len(data) == 1 && data[0] == '\n') {
			continue
		}

		url := URL{}
		if err := json.Unmarshal(data, &url); err != nil {
			continue
		}

		urlMap[url.ShortURL] = url
	}

	return urlMap
}

// NewProducer создает producer для append-only записи в файл
func NewProducer(filename string) (*Producer, error) {
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return nil, err
	}

	return &Producer{
		file:   file,
		writer: bufio.NewWriter(file),
	}, nil
}

// WriteURL записывает URL в файл в формате JSONL
func (p *Producer) WriteURL(url *URL) error {
	data, err := json.Marshal(url)
	if err != nil {
		return err
	}

	if _, err := p.writer.Write(data); err != nil {
		return err
	}

	if err := p.writer.WriteByte('\n'); err != nil {
		return err
	}

	return p.writer.Flush()
}

// Close закрывает producer (flush + file.Close)
func (p *Producer) Close() error {
	if p.writer != nil {
		if err := p.writer.Flush(); err != nil {
			return err
		}
	}
	if p.file != nil {
		return p.file.Close()
	}
	return nil
}

// NewConsumer создает consumer для чтения файла
func NewConsumer(filename string) (*Consumer, error) {
	file, err := os.OpenFile(filename, os.O_RDONLY|os.O_CREATE, 0666)
	if err != nil {
		return nil, err
	}

	return &Consumer{
		file:   file,
		reader: bufio.NewReader(file),
	}, nil
}

// ReadURL читает следующую URL из файла (JSONL)
func (c *Consumer) ReadURL() (*URL, error) {
	data, err := c.reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}

	url := URL{}
	err = json.Unmarshal(data, &url)
	if err != nil {
		return nil, err
	}

	return &url, nil
}

// Close закрывает consumer
func (c *Consumer) Close() error {
	if c.file != nil {
		return c.file.Close()
	}
	return nil
}

// Ping реализует интерфейс URLStorage (файловое хранилище всегда доступно)
func (fs *FileStorage) Ping(ctx context.Context) error {
	return nil
}
