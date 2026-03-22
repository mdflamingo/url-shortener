// Package service предоставляет бизнес-логику приложения URL Shortener
//
// Пакет содержит сервисы для:
//   - Генерации коротких URL
//   - Аудита действий пользователей
//   - Наблюдателей для аудита (файл, HTTP)
package service

import (
	"crypto/sha256"
	"encoding/base64"
	"math/rand"
)

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// GenerateShortURL генерирует случайный короткий URL заданной длины
//
// Параметры:
//   - length: требуемая длина генерируемой строки
//
// Возвращает:
//   - string: случайная строка, состоящая из букв (латиница, оба регистра) и цифр
func GenerateShortURL(length int) string {
	short := make([]byte, length)
	for i := range short {
		short[i] = letters[rand.Intn(len(letters))]
	}
	return string(short)
}

// GenerateShortURLForBatch генерирует детерминированный короткий URL на основе исходного URL
//
// Параметры:
//   - origURL: исходный длинный URL для хеширования
func GenerateShortURLForBatch(origURL string) string {
	hash := sha256.Sum256([]byte(origURL))
	return base64.URLEncoding.EncodeToString(hash[:])[:8]
}
