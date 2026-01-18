package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mdflamingo/url-shortener/internal/handler"
)

type SignedCookieMiddleware struct {
	secretKey []byte
}

func NewSignedCookieMiddleware(secret string) *SignedCookieMiddleware {
	if secret == "" {
		secret = "default-secret-key"
	}
	return &SignedCookieMiddleware{
		secretKey: []byte(secret),
	}
}

func (m *SignedCookieMiddleware) CookieMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        cookieName := "user_id"
        var userID string
        var shouldSetCookie bool

        cookie, err := r.Cookie(cookieName)

        if err == nil && cookie != nil && cookie.Value != "" {
            if id, valid := m.validateSignedCookie(cookie.Value); valid {
                userID = id
                shouldSetCookie = false
            } else {
                userID = uuid.New().String()
                shouldSetCookie = true
            }
        } else {
            userID = uuid.New().String()
            shouldSetCookie = true
        }

        if shouldSetCookie {
			log.Printf("Creating new userID: %s, reason: cookie invalid or missing", userID)
            signedCookie := m.createSignedCookie(userID)
            http.SetCookie(w, &http.Cookie{
                Name:     cookieName,
                Value:    signedCookie,
                HttpOnly: true,
                Secure:   false,
                SameSite: http.SameSiteLaxMode,
                MaxAge:   30 * 24 * 3600,
                Path:     "/",
                Expires:  time.Now().Add(30 * 24 * time.Hour),
            })
        } else {
			log.Printf("Using existing userID: %s", userID)
		}

        ctx := context.WithValue(r.Context(), handler.UserIDKey, userID)
        r = r.WithContext(ctx)

        next.ServeHTTP(w, r)
    })
}

func (m *SignedCookieMiddleware) createSignedCookie(userID string) string {
	timestamp := time.Now().Unix()
	data := userID + "|" + strconv.FormatInt(timestamp, 10)

	h := hmac.New(sha256.New, m.secretKey)
	h.Write([]byte(data))
	signature := base64.URLEncoding.EncodeToString(h.Sum(nil))

	encodedData := base64.URLEncoding.EncodeToString([]byte(data))

	return encodedData + "." + signature
}

func (m *SignedCookieMiddleware) validateSignedCookie(cookieValue string) (string, bool) {
	parts := strings.Split(cookieValue, ".")
	if len(parts) != 2 {
		return "", false
	}

	encodedData, signature := parts[0], parts[1]

	dataBytes, err := base64.URLEncoding.DecodeString(encodedData)
	if err != nil {
		return "", false
	}

	data := string(dataBytes)

	h := hmac.New(sha256.New, m.secretKey)
	h.Write([]byte(data))
	expectedSig := base64.URLEncoding.EncodeToString(h.Sum(nil))

	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		return "", false
	}

	dataParts := strings.Split(data, "|")
	if len(dataParts) != 2 {
		return "", false
	}

	userID := dataParts[0]

	return userID, true
}

func GetUserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(handler.UserIDKey).(string)
	return userID, ok
}
