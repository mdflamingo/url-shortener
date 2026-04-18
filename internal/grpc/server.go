// internal/grpc/server.go
package grpc

import (
	"context"
	"errors"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	pb "github.com/mdflamingo/url-shortener/api/url_shortener"
	"github.com/mdflamingo/url-shortener/internal/middleware"
	"github.com/mdflamingo/url-shortener/internal/service"
)

// ShortenerServer реализует gRPC сервер для сервиса сокращения URL
type ShortenerServer struct {
	pb.UnimplementedShortenerServiceServer
	urlService *service.URLService
	logger     *zap.Logger
}

// NewShortenerServer создает новый gRPC сервер
func NewShortenerServer(urlService *service.URLService, logger *zap.Logger) *ShortenerServer {
	return &ShortenerServer{
		urlService: urlService,
		logger:     logger,
	}
}

// ShortenURL реализует gRPC метод для сокращения URL
func (s *ShortenerServer) ShortenURL(ctx context.Context, req *pb.URLShortenRequest) (*pb.URLShortenResponse, error) {
	userID, err := getUserIDFromContext(ctx)
	if err != nil {
		s.logger.Warn("failed to get userID from context", zap.Error(err))
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}
	if req.GetUrl() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "URL cannot be empty")
	}

	shortURL, isConflict, err := s.urlService.CreateShortURL(ctx, req.GetUrl(), userID)
	if err != nil {
		s.logger.Error("failed to create short URL",
			zap.String("original_url", req.GetUrl()),
			zap.String("user_id", userID),
			zap.Error(err))

		switch {
		case errors.Is(err, service.ErrEmptyURL):
			return nil, status.Errorf(codes.InvalidArgument, "URL cannot be empty")
		case errors.Is(err, service.ErrInvalidURL):
			return nil, status.Errorf(codes.InvalidArgument, "invalid URL format")
		default:
			return nil, status.Errorf(codes.Internal, "failed to create short URL")
		}
	}

	fullURL := s.urlService.BuildFullURL(shortURL)
	if fullURL == "" {
		return nil, status.Errorf(codes.Internal, "failed to build full URL")
	}

	if isConflict {
		s.logger.Info("URL already exists",
			zap.String("short_url", shortURL),
			zap.String("user_id", userID))
		return nil, status.Errorf(codes.AlreadyExists, "Already Exists")
	}

	var response pb.URLShortenResponse
	response.SetResult(fullURL)
	return &response, nil
}

// ExpandURL реализует gRPC метод для получения оригинального URL по короткому ID
func (s *ShortenerServer) ExpandURL(ctx context.Context, req *pb.URLExpandRequest) (*pb.URLExpandResponse, error) {
	userID, err := getUserIDFromContext(ctx)
	if err != nil {
		s.logger.Warn("failed to get userID from context", zap.Error(err))
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}

	if req.GetId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "short URL ID cannot be empty")
	}

	originalURL, err := s.urlService.GetOriginalURL(ctx, req.GetId(), userID)
	if err != nil {
		s.logger.Error("failed to get original URL",
			zap.String("short_id", req.GetId()),
			zap.String("user_id", userID),
			zap.Error(err))

		switch {
		case errors.Is(err, service.ErrURLNotFound):
			return nil, status.Errorf(codes.NotFound, "short URL not found")
		case errors.Is(err, service.ErrURLDeleted):
			return nil, status.Errorf(codes.PermissionDenied, "URL has been deleted")
		default:
			return nil, status.Errorf(codes.Internal, "failed to get original URL")
		}
	}

	response := pb.URLExpandResponse_builder{
		Result: &originalURL,
	}.Build()
	return response, nil
}

// ListUserURLs реализует gRPC метод для получения всех URL пользователя
func (s *ShortenerServer) ListUserURLs(ctx context.Context, _ *emptypb.Empty) (*pb.UserURLsResponse, error) {
	userID, err := getUserIDFromContext(ctx)
	if err != nil {
		s.logger.Warn("failed to get userID from context", zap.Error(err))
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}

	// Получаем URL пользователя
	urls, err := s.urlService.GetUserURLs(ctx, userID)
	if err != nil {
		s.logger.Error("failed to get user URLs",
			zap.String("user_id", userID),
			zap.Error(err))
		return nil, status.Errorf(codes.Internal, "failed to get user URLs")
	}

	pbURLs := make([]*pb.URLData, 0, len(urls))
	for _, u := range urls {
		pbURLs = append(pbURLs, (&pb.URLData_builder{
			ShortUrl:    &u.ShortURL,
			OriginalUrl: &u.OriginalURL,
		}).Build())
	}

	response := pb.UserURLsResponse_builder{
		Url: pbURLs,
	}.Build()

	return response, nil
}

// getUserIDFromContext извлекает userID из контекста gRPC
func getUserIDFromContext(ctx context.Context) (string, error) {
	userID, ok := ctx.Value(middleware.UserIDKey).(string)
	if !ok || userID == "" {
		return "", errors.New("user ID not found in context")
	}
	return userID, nil
}
