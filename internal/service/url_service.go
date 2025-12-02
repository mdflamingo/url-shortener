package service

import (
	"crypto/sha256"
	"encoding/base64"
	"math/rand"
)

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func GenerateShortURL(length int) string {
	short := make([]byte, length)
	for i := range short {
		short[i] = letters[rand.Intn(len(letters))]
	}
	return string(short)
}

func GenerateShortURLForBatch(origURL string) string {
	hash := sha256.Sum256([]byte(origURL))
	return base64.URLEncoding.EncodeToString(hash[:])[:8]
}
