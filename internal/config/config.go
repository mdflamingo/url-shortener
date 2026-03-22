// Package config предоставляет функциональность для работы с конфигурацией приложения
//
// Содержит структуру Config и функции для парсинга флагов командной строки
// с поддержкой переменных окружения.
package config

import (
	"flag"
	"os"
	"strings"
)

// Config содержит все настройки приложения URL Shortener
type Config struct {
	// RunAddr - адрес и порт для запуска HTTP сервера (по умолчанию ":8080")
	RunAddr string
	// BaseShortURL - базовый URL для формирования коротких ссылок (по умолчанию "http://localhost:8080")
	BaseShortURL string
	// LogLevel - уровень логирования (INFO, DEBUG, WARN, ERROR) (по умолчанию "INFO")
	LogLevel string
	// FileStoragePath - путь к файлу для хранения URL (по умолчанию "urls.csv")
	FileStoragePath string
	// DataBaseDSN - строка подключения к PostgreSQL базе данных
	DataBaseDSN string
	// CookieSecretKey - секретный ключ для подписи cookie (по умолчанию "default-secret-key")
	CookieSecretKey string
	// AuditFile - путь к файлу для логов аудита (по умолчанию "logs.log")
	AuditFile string
	// AuditURL - URL API для отправки логов аудита (по умолчанию "http://example.com/logs")
	AuditURL string
}

// ParseFlags парсит флаги командной строки и переменные окружения,
// возвращает настроенную конфигурацию приложения
//
// Поддерживаемые флаги:
//
//	-a, --address=ADDR           адрес и порт сервера
//	-b, --base-url=URL           базовый URL для коротких ссылок
//	-l, --log-level=LEVEL        уровень логирования
//	-f, --file=FILENAME          путь к файлу хранилища
//	-d, --database=DSN           строка подключения к БД
//	-s, --secret=KEY             секретный ключ для cookie
//	-audit-file=PATH             файл логов аудита
//	-audit-url=URL               API для логов аудита
//
// Поддерживаемые переменные окружения:
//
//	SERVER_ADDRESS, BASE_URL, LOG_LEVEL, FILE_STORAGE_PATH,
//	DATABASE_CONN_STRING, COOKIE_SECRET_KEY, AUDIT_FILE, AUDIT_URL
//
// Пример использования:
//
//	./urlshortener -a ":8080" -b "https://short.ly" -f "./urls.json"
func ParseFlags() *Config {
	cfg := &Config{}

	RunAddr := flag.String("a", ":8080", "address and port to run server")
	baseURL := flag.String("b", "http://localhost:8080", "base address before short url")
	logLevel := flag.String("l", "INFO", "log level")
	fileStoragePath := flag.String("f", "urls.csv", "urls file path")
	dataBaseDSN := flag.String("d", "", "connect to postgres")
	cookieSecretKey := flag.String("s", "default-secret-key", "you secret key for cookie")
	auditFile := flag.String("audit-file", "logs.log", "file for audit logs")
	auditURL := flag.String("audit-url", "http://example.com/logs", "API to send audit logs")

	flag.Parse()

	cfg.RunAddr = getEnvOrDefault("SERVER_ADDRESS", *RunAddr)
	cfg.BaseShortURL = getEnvOrDefault("BASE_URL", *baseURL)
	cfg.LogLevel = strings.ToUpper(getEnvOrDefault("LOG_LEVEL", *logLevel))
	cfg.FileStoragePath = getEnvOrDefault("FILE_STORAGE_PATH", *fileStoragePath)
	cfg.DataBaseDSN = getEnvOrDefault("DATABASE_CONN_STRING", *dataBaseDSN)
	cfg.CookieSecretKey = getEnvOrDefault("COOKIE_SECRET_KEY", *cookieSecretKey)
	cfg.AuditFile = getEnvOrDefault("AUDIT_FILE", *auditFile)
	cfg.AuditURL = getEnvOrDefault("AUDIT_URL", *auditURL)

	return cfg
}

// getEnvOrDefault возвращает значение переменной окружения или значение по умолчанию
//
// Если переменная окружения задана и не пустая, возвращает её значение.
// В противном случае возвращает defaultValue.
//
// Используется для приоритизации: переменные окружения > флаги > значения по умолчанию.
func getEnvOrDefault(envName, defaultValue string) string {
	if envValue := os.Getenv(envName); envValue != "" {
		return envValue
	}
	return defaultValue
}
