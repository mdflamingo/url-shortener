// Package config предоставляет функциональность для работы с конфигурацией приложения
//
// Содержит структуру Config и функции для парсинга флагов командной строки
// с поддержкой переменных окружения.
package config

import (
	"encoding/json"
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
	// EnabledHTTPS - включение HTTPS в веб-сервере (по умолчанию выключен	)
	EnabledHTTPS bool
}

// FileConfig содержит все настройки приложения URL Shortener из json файла
type FileConfig struct {
	// ServerAdress - адрес и порт для запуска HTTP сервера (по умолчанию ":8080")
	ServerAdress string
	// BaseURL - базовый URL для формирования коротких ссылок (по умолчанию "http://localhost:8080")
	BaseURL string
	// FileStoragePath - путь к файлу для хранения URL (по умолчанию "urls.csv")
	FileStoragePath string
	// DataBaseDSN - строка подключения к PostgreSQL базе данных
	DataBaseDSN string
	// EnabledHTTPS - включение HTTPS в веб-сервере (по умолчанию выключен	)
	EnabledHTTPS bool
}

// ParseFlags парсит флаги командной строки и переменные окружения,
// возвращает настроенную конфигурацию приложения
//
// Поддерживаемые флаги:
//
//		-a                      адрес и порт сервера
//		-b                      базовый URL для коротких ссылок
//		-l                      уровень логирования
//		-f                      путь к файлу хранилища
//		-d                      строка подключения к БД
//		-secret-key             секретный ключ для cookie
//		-audit-file=PATH        файл логов аудита
//		-audit-url=URL          API для логов аудита
//	    -s                      включение HTTPS в веб-сервере (true/false)
//
// Поддерживаемые переменные окружения:
//
//	SERVER_ADDRESS, BASE_URL, LOG_LEVEL, FILE_STORAGE_PATH,
//	DATABASE_CONN_STRING, COOKIE_SECRET_KEY, AUDIT_FILE, AUDIT_URL, ENABLE_HTTPS
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
	cookieSecretKey := flag.String("secret-key", "default-secret-key", "you secret key for cookie")
	auditFile := flag.String("audit-file", "logs.log", "file for audit logs")
	auditURL := flag.String("audit-url", "http://example.com/logs", "API to send audit logs")
	enabledHTTPS := flag.Bool("s", false, "enabled HTTPS")
	configFile := flag.String("c", "config.json", "config from json file")

	flag.Parse()

	fileConfig := loadConfigFile(*configFile)

	cfg.RunAddr = getValue(*RunAddr, "SERVER_ADDRESS", fileConfig.ServerAdress, ":8080")
	cfg.BaseShortURL = getValue(*baseURL, "BASE_URL", fileConfig.BaseURL, "http://localhost:8080")
	cfg.LogLevel = strings.ToUpper(getValue(*logLevel, "LOG_LEVEL", "", "INFO"))
	cfg.FileStoragePath = getValue(*fileStoragePath, "FILE_STORAGE_PATH", fileConfig.FileStoragePath, "urls.csv")
	cfg.DataBaseDSN = getValue(*dataBaseDSN, "DATABASE_CONN_STRING", fileConfig.DataBaseDSN, "")
	cfg.CookieSecretKey = getValue(*cookieSecretKey, "COOKIE_SECRET_KEY", "", "default-secret-key")
	cfg.AuditFile = getValue(*auditFile, "AUDIT_FILE", "", "logs.log")
	cfg.AuditURL = getValue(*auditURL, "AUDIT_URL", "", "http://example.com/logs")
	cfg.EnabledHTTPS = getBoolValue(*enabledHTTPS, "ENABLE_HTTPS", fileConfig.EnabledHTTPS, false)

	return cfg

}

// getValue возвращает значение переменной окружения, значение по умолчанию или занчение из файла
// Используется для приоритизации: переменные окружения > флаги > файл > значения по умолчанию.
func getValue(flagValue, envName, fileValue, defaultValue string) string {
	if envValue := os.Getenv(envName); envValue != "" {
		return envValue
	}
	if flagValue != "" && flagValue != defaultValue {
		return flagValue
	}
	if fileValue != "" {
		return fileValue
	}

	return defaultValue
}

// getBoolValue возвращает значение переменной окружения, значение по умолчанию или занчение из файла для типов bool
// Используется для приоритизации: переменные окружения > флаги > файл > значения по умолчанию.
func getBoolValue(flagValue bool, envName string, fileValue bool, defaultValue bool) bool {
	if flagValue != defaultValue {
		return flagValue
	}

	if envValue := os.Getenv(envName); envValue != "" {
		return strings.ToLower(envValue) == "true" || envValue == "1"
	}

	if fileValue != defaultValue {
		return fileValue
	}

	return defaultValue
}

// loadConfigFile возвращает значения из файла конфигурации
func loadConfigFile(path string) *FileConfig {
	data, err := os.ReadFile(path)
	if err != nil {
		return &FileConfig{}
	}

	var fileConfig FileConfig

	err = json.Unmarshal(data, &fileConfig)
	if err != nil {
		return &FileConfig{}
	}

	return &fileConfig
}
