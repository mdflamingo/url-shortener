// Package service предоставляет бизнес-логику приложения URL Shortener
//
// Пакет содержит сервисы для:
//   - Генерации коротких URL
//   - Аудита действий пользователей
//   - Наблюдателей для аудита (файл, HTTP)
package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/mdflamingo/url-shortener/internal/logger"
	"github.com/mdflamingo/url-shortener/internal/models"
	"github.com/mdflamingo/url-shortener/internal/repository"
)

var (
	ErrInvalidURL   = errors.New("invalid URL format")
	ErrEmptyURL     = errors.New("URL cannot be empty")
	ErrURLNotFound  = errors.New("URL not found")
	ErrURLDeleted   = errors.New("URL has been deleted")
	ErrConflict     = errors.New("URL already exists")
	ErrStorageNil   = errors.New("storage is nil")
	ErrUnauthorized = errors.New("user not authorized")
	ErrForbidden    = errors.New("access forbidden")
)

const (
	DefaultShortURLLength = 6
	MaxGenerateAttempts   = 10
	letters               = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// URLService предоставляет бизнес-логику для работы с URL
type URLService struct {
	storage repository.URLStorage
	baseURL string
	audit   *AuditService
	logger  *zap.Logger
}

// NewURLService создает новый экземпляр URLService
func NewURLService(storage repository.URLStorage, baseURL string, audit *AuditService, logger *zap.Logger) *URLService {
	return &URLService{
		storage: storage,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		audit:   audit,
		logger:  logger,
	}
}

// ValidateURL проверяет корректность URL
func (s *URLService) ValidateURL(rawURL string) error {
	if rawURL == "" {
		return ErrEmptyURL
	}

	parsedURL, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("%w: only HTTP/HTTPS protocols are allowed", ErrInvalidURL)
	}

	return nil
}

// GenerateShortURL генерирует случайный короткий URL заданной длины
func (s *URLService) GenerateShortURL(length int) string {
	short := make([]byte, length)
	for i := range short {
		short[i] = letters[rand.Intn(len(letters))]
	}
	return string(short)
}

// GenerateShortURLForBatch генерирует детерминированный короткий URL на основе исходного URL
func (s *URLService) GenerateShortURLForBatch(origURL string) string {
	hash := sha256.Sum256([]byte(origURL))
	return base64.URLEncoding.EncodeToString(hash[:])[:8]
}

// CreateShortURL создает короткий URL для данного оригинального URL
// Возвращает: короткий URL, флаг конфликта (если URL уже существует), ошибку
func (s *URLService) CreateShortURL(ctx context.Context, originalURL, userID string) (string, bool, error) {
	if err := s.ValidateURL(originalURL); err != nil {
		return "", false, err
	}

	if s.storage == nil {
		return "", false, ErrStorageNil
	}

	var lastErr error

	for attempt := 0; attempt < MaxGenerateAttempts; attempt++ {
		shortURL := s.GenerateShortURL(DefaultShortURLLength)
		s.logger.Info("Generated ID",
			zap.String("id", shortURL),
			zap.Int("attempt", attempt),
			zap.String("user_id", userID))

		savedShortURL, err := s.storage.Save(shortURL, originalURL, userID)
		if err == nil {
			if s.audit != nil {
				fullURL := s.BuildFullURL(shortURL)
				s.audit.Notify(AuditEvent{
					Action: "shorten",
					UserID: userID,
					URL:    fullURL,
					TS:     time.Now().Unix(),
				})
			}
			return savedShortURL, false, nil
		}

		lastErr = err

		if errors.Is(err, repository.ErrConflict) {
			if savedShortURL != "" {
				return savedShortURL, true, nil
			}
			continue
		}

		return "", false, err
	}

	return "", false, fmt.Errorf("failed to generate unique short URL after %d attempts: %w", MaxGenerateAttempts, lastErr)
}

// GetOriginalURL получает оригинальный URL по короткому идентификатору
func (s *URLService) GetOriginalURL(ctx context.Context, shortID, userID string) (string, error) {
	origURL, found, deleted := s.storage.Get(shortID)

	if !found {
		s.logger.Warn("short URL not found",
			zap.String("short_id", shortID),
			zap.String("user_id", userID))
		return "", ErrURLNotFound
	}

	if deleted {
		s.logger.Warn("short URL is deleted",
			zap.String("short_id", shortID),
			zap.String("user_id", userID))
		return "", ErrURLDeleted
	}

	if s.audit != nil {
		s.audit.Notify(AuditEvent{
			Action: "follow",
			UserID: userID,
			URL:    origURL,
			TS:     time.Now().Unix(),
		})
	}

	return origURL, nil
}

// BuildFullURL строит полный URL на основе короткого идентификатора
func (s *URLService) BuildFullURL(shortURL string) string {
	fullURL, err := url.JoinPath(s.baseURL, shortURL)
	if err != nil {
		s.logger.Error("failed to join URL path",
			zap.String("base_url", s.baseURL),
			zap.String("short_url", shortURL),
			zap.Error(err))
		return ""
	}
	return fullURL
}

// CreateBatchShortURLs создает пакет коротких URL
func (s *URLService) CreateBatchShortURLs(ctx context.Context, batches []models.BatchRequest, userID string) ([]models.BatchResponse, error) {
	if len(batches) == 0 {
		return nil, errors.New("empty batch request")
	}

	urlPairs := make([]repository.URLPair, 0, len(batches))

	for _, row := range batches {
		if err := s.ValidateURL(row.OriginalURL); err != nil {
			return nil, fmt.Errorf("invalid URL %s: %w", row.OriginalURL, err)
		}

		shortURL := s.GenerateShortURLForBatch(row.OriginalURL)
		urlPairs = append(urlPairs, repository.URLPair{
			ShortURL:    shortURL,
			OriginalURL: row.OriginalURL,
			UserID:      userID,
		})
	}

	updatedPairs, err := s.storage.SaveMany(urlPairs)
	if err != nil {
		s.logger.Error("Failed to save URLs in batch",
			zap.Error(err),
			zap.String("user_id", userID))
		return nil, err
	}

	responses := make([]models.BatchResponse, 0, len(updatedPairs))
	for i, pair := range updatedPairs {
		fullURL := s.BuildFullURL(pair.ShortURL)
		if fullURL == "" {
			return nil, errors.New("failed to build full URL")
		}

		responses = append(responses, models.BatchResponse{
			CorrelationID: batches[i].CorrelationID,
			ShortURL:      fullURL,
		})
	}

	return responses, nil
}

// GetUserURLs возвращает все URL пользователя
func (s *URLService) GetUserURLs(ctx context.Context, userID string) ([]models.ResponseByUser, error) {
	if userID == "" {
		return nil, ErrUnauthorized
	}

	urls, err := s.storage.GetByUserID(userID)
	if err != nil {
		s.logger.Error("Failed to get URLs from storage",
			zap.Error(err),
			zap.String("user_id", userID))
		return nil, err
	}

	if len(urls) == 0 {
		return []models.ResponseByUser{}, nil
	}

	responses := make([]models.ResponseByUser, 0, len(urls))
	for _, pair := range urls {
		fullURL := s.BuildFullURL(pair.ShortURL)
		if fullURL == "" {
			continue
		}

		responses = append(responses, models.ResponseByUser{
			OriginalURL: pair.OriginalURL,
			ShortURL:    fullURL,
		})
	}

	return responses, nil
}

// DeleteUserURLs удаляет URL пользователя асинхронно
func (s *URLService) DeleteUserURLs(ctx context.Context, shortURLs []string, userID string) error {
	if userID == "" {
		return ErrUnauthorized
	}

	if len(shortURLs) == 0 {
		return errors.New("empty URLs list")
	}

	inputCh := make(chan string, len(shortURLs))
	doneCh := make(chan struct{})

	go func() {
		defer close(inputCh)
		for _, url := range shortURLs {
			select {
			case <-doneCh:
				s.logger.Info("Delete cancelled", zap.String("user_id", userID))
				return
			case inputCh <- url:
				s.logger.Info("Sending URL for delete",
					zap.String("url", url),
					zap.String("user_id", userID))
			}
		}
	}()

	// Запуск удаления
	resultCh := s.storage.Delete(doneCh, inputCh, userID)

	go func() {
		defer close(doneCh)
		for err := range resultCh {
			if err != nil {
				s.logger.Error("Failed to delete URL batch",
					zap.Error(err),
					zap.String("user_id", userID))
			} else {
				s.logger.Info("URL batch deleted successfully",
					zap.String("user_id", userID))
			}
		}
		s.logger.Info("All delete operations completed",
			zap.String("user_id", userID))
	}()

	return nil
}

// GetStats возвращает статистику сервиса
func (s *URLService) GetStats(ctx context.Context) (*models.ResponseStats, error) {
	stats, err := s.storage.GetStats()
	if err != nil {
		s.logger.Error("Failed to get stats from storage", zap.Error(err))
		return nil, err
	}

	return &models.ResponseStats{
		Urls:  stats.Urls,
		Users: stats.Users,
	}, nil
}

// Ping проверяет доступность хранилища
func (s *URLService) Ping(ctx context.Context) error {
	return s.storage.Ping(ctx)
}

// ValidateTrustedIP проверяет, принадлежит ли IP доверенной подсети
func (s *URLService) ValidateTrustedIP(clientIP, trustedSubnet string) bool {
	if trustedSubnet == "" {
		s.logger.Warn("trusted subnet is empty")
		return false
	}

	if clientIP == "" {
		s.logger.Warn("X-Real-IP header is missing")
		return false
	}

	return isIPInTrustedSubnet(clientIP, trustedSubnet)
}

// isIPInTrustedSubnet проверяет, принадлежит ли IP указанной подсети CIDR
func isIPInTrustedSubnet(ipStr, cidrStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		logger.Log.Warn("Invalid IP address format", zap.String("ip", ipStr))
		return false
	}

	_, cidrNet, err := net.ParseCIDR(cidrStr)
	if err != nil {
		logger.Log.Error("Invalid trusted subnet CIDR",
			zap.String("trusted_subnet", cidrStr),
			zap.Error(err))
		return false
	}

	return cidrNet.Contains(ip)
}
