package service

import "math/rand"

const Letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func GenerateShortURL(length int) string {
	short := make([]byte, length)
	for i := range short {
		short[i] = Letters[rand.Intn(len(Letters))]
	}
	return string(short)
}
