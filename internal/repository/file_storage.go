package repository

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/mdflamingo/url-shortener/internal/service"
)

type FileStorage struct {
	producer *Producer
	filename string
	mu       sync.RWMutex
}

type URL struct {
	ShortURL    string `json:"short_url"`
	OriginalURL string `json:"original_url"`
}

type Producer struct {
	file   *os.File
	writer *bufio.Writer
}

type Consumer struct {
	file   *os.File
	reader *bufio.Reader
}

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

func (fs *FileStorage) Save(shortURL, originalURL string) (string, error) {
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
	}

	if err := fs.producer.WriteURL(url); err != nil {
		return "", fmt.Errorf("failed to write URL to file: %w", err)
	}

	return url.ShortURL, nil
}

func (fs *FileStorage) SaveMany(urls []URLPair) ([]URLPair, error) {
	for i, url := range urls {
		for {
			_, err := fs.Save(url.ShortURL, url.OriginalURL)
			if err != nil {
				if fmt.Sprintf("%v", err) == fmt.Sprintf("short URL already exists: %s", url.ShortURL) {
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

func (fs *FileStorage) Get(shortURL string) (string, bool) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	originalURL, exists := fs.findInFile(shortURL)
	return originalURL, exists
}

func (fs *FileStorage) Close() error {
	if fs.producer != nil {
		return fs.producer.Close()
	}
	return nil
}

func (fs *FileStorage) checkExists(shortURL string) (bool, error) {
	_, exists := fs.findInFile(shortURL)
	return exists, nil
}

func (fs *FileStorage) findInFile(shortURL string) (string, bool) {
	file, err := os.OpenFile(fs.filename, os.O_RDONLY|os.O_CREATE, 0666)
	if err != nil {
		return "", false
	}
	defer file.Close()

	reader := bufio.NewReader(file)

	for {
		data, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", false
		}

		if len(data) == 0 || (len(data) == 1 && data[0] == '\n') {
			continue
		}

		url := URL{}
		if err := json.Unmarshal(data, &url); err != nil {
			continue
		}

		if url.ShortURL == shortURL {
			return url.OriginalURL, true
		}
	}

	return "", false
}

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

func (c *Consumer) Close() error {
	if c.file != nil {
		return c.file.Close()
	}
	return nil
}

func (fs *FileStorage) Ping(ctx context.Context) error {
	return nil
}
