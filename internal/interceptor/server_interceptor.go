// Package grpcinterceptor содержит gRPC интерсепторы для аутентификации
package interceptor

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/mdflamingo/url-shortener/internal/logger"
)

// contextKey - тип для ключей контекста (должен совпадать с HTTP middleware)
type contextKey string

const UserIDKey contextKey = "userID"

// AuthInterceptor - интерсептор для аутентификации в gRPC
type AuthInterceptor struct {
	secretKey []byte
}

// NewAuthInterceptor создает новый gRPC auth интерсептор
func NewAuthInterceptor(secret string) *AuthInterceptor {
	return &AuthInterceptor{
		secretKey: []byte(secret),
	}
}

// UnaryInterceptor - унарный интерсептор для gRPC
func (a *AuthInterceptor) UnaryInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	ctx, err := a.authenticate(ctx)
	if err != nil {
		logger.Log.Error("gRPC authentication failed", zap.Error(err))
		return nil, status.Errorf(codes.Unauthenticated, "authentication failed: %v", err)
	}

	return handler(ctx, req)
}

// authenticate - основная логика аутентификации
func (a *AuthInterceptor) authenticate(ctx context.Context) (context.Context, error) {
	var userID string
	var shouldCreateUser bool

	// metadata из контекста
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		logger.Log.Warn("No metadata in gRPC context, creating new user")
		shouldCreateUser = true
	} else {
		authHeaders := md.Get("authorization")
		var token string

		if len(authHeaders) > 0 {
			token = authHeaders[0]
			token = strings.TrimPrefix(token, "Bearer ")
			token = strings.TrimSpace(token)
		}

		if token != "" {
			id, err := a.validateJWT(token)
			if err != nil {
				logger.Log.Warn("JWT validation failed, creating new user",
					zap.Error(err))
				shouldCreateUser = true
			} else {
				userID = id
				shouldCreateUser = false
				logger.Log.Info("Using existing userID from gRPC metadata",
					zap.String("userID", userID))
			}
		} else {
			logger.Log.Info("No authorization token in gRPC metadata, creating new user")
			shouldCreateUser = true
		}
	}

	if shouldCreateUser {
		userID = uuid.New().String()
		logger.Log.Info("Created new userID for gRPC", zap.String("userID", userID))
	}

	ctx = context.WithValue(ctx, UserIDKey, userID)

	if shouldCreateUser {
		jwtToken := a.createJWT(userID)
		grpc.SetTrailer(ctx, metadata.Pairs("new-authorization", jwtToken))
	}

	return ctx, nil
}

// createJWT создает JWT токен для указанного userID
func (a *AuthInterceptor) createJWT(userID string) string {
	claims := jwt.MapClaims{
		"userID": userID,
		"exp":    time.Now().Add(30 * 24 * time.Hour).Unix(),
		"iat":    time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(a.secretKey)
	if err != nil {
		logger.Log.Error("Error creating JWT for gRPC", zap.Error(err))
		return ""
	}
	return tokenString
}

// validateJWT валидирует JWT токен и извлекает userID
func (a *AuthInterceptor) validateJWT(tokenString string) (string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return a.secretKey, nil
	})

	if err != nil {
		return "", err
	}

	if !token.Valid {
		return "", errors.New("invalid JWT token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", errors.New("invalid claims format")
	}

	userID, exists := claims["userID"].(string)
	if !exists || userID == "" {
		return "", errors.New("userID not found in claims")
	}

	return userID, nil
}

// GetUserIDFromContext извлекает userID из контекста gRPC
func GetUserIDFromContext(ctx context.Context) (string, error) {
	userIDValue := ctx.Value(UserIDKey)
	if userIDValue == nil {
		return "", errors.New("userID not found in context")
	}
	userID, ok := userIDValue.(string)
	if !ok {
		return "", errors.New("userID is not a string")
	}
	if userID == "" {
		return "", errors.New("userID is empty")
	}
	return userID, nil
}
