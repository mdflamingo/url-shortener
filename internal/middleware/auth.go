// Package middleware содержит HTTP middleware для аутентификации пользователей через подписанные JWT cookie
//
// Реализует механизм идентификации пользователей по cookie "user_id" с JWT токеном.
// Автоматически создает нового пользователя (UUID) при отсутствии/невалидности cookie.
// Добавляет userID в контекст запроса для использования в обработчиках.
package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/mdflamingo/url-shortener/internal/logger"
)

// contextKey - тип для ключей контекста (избегаем коллизий с другими пакетами)
type contextKey string

const UserIDKey contextKey = "userID"

// SignedCookieMiddleware - middleware для работы с подписанными JWT cookie пользователей
type SignedCookieMiddleware struct {
	secretKey []byte // секретный ключ для подписи JWT токенов
}

// NewSignedCookieMiddleware создает новый middleware для аутентификации по cookie
//
// secret - секретный ключ для подписи JWT (рекомендуется 32+ байта случайных данных).
//
// Пример:
//
//	middleware := middleware.NewSignedCookieMiddleware("my-secret-key-32-chars-long")
func NewSignedCookieMiddleware(secret string) *SignedCookieMiddleware {
	return &SignedCookieMiddleware{
		secretKey: []byte(secret),
	}
}

// CookieMiddleware - HTTP middleware для аутентификации пользователей
//
// 1. Читает cookie "user_id" с JWT токеном
// 2. Валидирует JWT или создает новый UUID пользователя
// 3. Устанавливает cookie на 30 дней (HttpOnly, SameSite=Lax)
// 4. Добавляет userID в context запроса
//
// Регистрация в chi:
//
//	r.Use(cookieMiddleware.CookieMiddleware)
//
// Cookie параметры:
//
//	Name: "user_id", HttpOnly: true, MaxAge: 30 дней, Path: "/"
func (m *SignedCookieMiddleware) CookieMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookieName := "user_id"
		var userID string
		var shouldSetCookie bool

		cookie, err := r.Cookie(cookieName)
		if err == nil && cookie != nil && cookie.Value != "" {
			if id, err := m.validateJWT(cookie.Value); err != nil {
				logger.Log.Error("JWT validation failed", zap.Error(err))
				userID = uuid.New().String()
				shouldSetCookie = true
			} else {
				userID = id
				shouldSetCookie = false
			}
		} else {
			userID = uuid.New().String()
			shouldSetCookie = true
		}

		if shouldSetCookie {
			logger.Log.Info("Creating new userID", zap.String("userID", userID))
			jwtToken := m.createJWT(userID)
			http.SetCookie(w, &http.Cookie{
				Name:     cookieName,
				Value:    jwtToken,
				HttpOnly: true,
				Secure:   false,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   30 * 24 * 3600,
				Path:     "/",
				Expires:  time.Now().Add(30 * 24 * time.Hour),
			})
		} else {
			logger.Log.Info("Using existing userID", zap.String("userID", userID))
		}

		ctx := context.WithValue(r.Context(), UserIDKey, userID)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}

// createJWT создает JWT токен для указанного userID
//
// Токен содержит claims: userID, exp (30 дней), iat.
// Подписывается HS256 с использованием secretKey.
func (m *SignedCookieMiddleware) createJWT(userID string) string {
	claims := jwt.MapClaims{
		"userID": userID,
		"exp":    time.Now().Add(30 * 24 * time.Hour).Unix(),
		"iat":    time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(m.secretKey)
	if err != nil {
		logger.Log.Error("Error creating JWT", zap.Error(err))
		return ""
	}
	return tokenString
}

// validateJWT валидирует JWT токен и извлекает userID
//
// Проверяет подпись, срок действия, формат claims.
// Возвращает userID или ошибку валидации.
func (m *SignedCookieMiddleware) validateJWT(tokenString string) (string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return m.secretKey, nil
	})

	if err != nil {
		return "", fmt.Errorf("failed to parse JWT: %w", err)
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

// GetUserIDFromRequest извлекает userID из контекста запроса
//
// Должен вызываться после прохождения через CookieMiddleware.
// Возвращает userID или ошибку если не найден в контексте.
//
// Пример использования в handler:
//
//	userID, err := middleware.GetUserIDFromRequest(r)
//	if err != nil {
//	    http.Error(w, "Unauthorized", http.StatusUnauthorized)
//	    return
//	}
func GetUserIDFromRequest(r *http.Request) (string, error) {
	ctx := r.Context()
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
