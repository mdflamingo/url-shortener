// Package middleware содержит Gzip middleware для сжатия HTTP запросов и ответов
//
// Поддерживает:
// - Сжатие ответов для клиентов с Accept-Encoding: gzip (статусы 2xx, кроме 204)
// - Распаковку gzip запросов (Content-Encoding: gzip)
// - Автоматическое добавление Content-Encoding: gzip заголовка
//
// Регистрация в chi:
//
//	r.Use(middleware.GzipMiddleware)
package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

// compressWriter - обертка над http.ResponseWriter для сжатия ответа gzip
//
// Сжимает только успешные ответы (2xx, кроме 204 NoContent).
// Автоматически устанавливает Content-Encoding: gzip.
type compressWriter struct {
	w             http.ResponseWriter // оригинальный ResponseWriter
	zw            *gzip.Writer        // gzip writer
	statusCode    int                 // HTTP статус кода
	headerWritten bool                // флаг записи заголовков
}

// newCompressWriter создает новый compressWriter
func newCompressWriter(w http.ResponseWriter) *compressWriter {
	return &compressWriter{
		w:          w,
		zw:         gzip.NewWriter(w),
		statusCode: http.StatusOK,
	}
}

// Header возвращает заголовки оригинального ResponseWriter
func (c *compressWriter) Header() http.Header {
	return c.w.Header()
}

// Write записывает данные в ответ
//
// Для статусов 2xx (кроме 204) - сжимает через gzip.
// Для остальных статусов - пишет без сжатия.
func (c *compressWriter) Write(p []byte) (int, error) {
	if !c.headerWritten {
		c.WriteHeader(c.statusCode)
	}

	// Сжимаем только успешные ответы (кроме 204 NoContent)
	if c.statusCode >= 200 && c.statusCode < 300 && c.statusCode != http.StatusNoContent {
		return c.zw.Write(p)
	}
	return c.w.Write(p)
}

// WriteHeader устанавливает HTTP статус и Content-Encoding: gzip
//
// Устанавливает заголовок только для сжимаемых статусов (2xx, кроме 204).
// Игнорирует повторные вызовы WriteHeader.
func (c *compressWriter) WriteHeader(statusCode int) {
	if c.headerWritten {
		return
	}

	c.statusCode = statusCode
	c.headerWritten = true

	// ИСПРАВЛЕНО: убрана лишняя проверка statusCode = 409 и исправлен синтаксис
	// Content-Encoding только для сжимаемых ответов
	if statusCode >= 200 && statusCode < 300 && statusCode != http.StatusNoContent {
		c.w.Header().Set("Content-Encoding", "gzip")
	}

	c.w.WriteHeader(statusCode)
}

// Close закрывает gzip writer и освобождает ресурсы
func (c *compressWriter) Close() error {
	if c.zw != nil {
		return c.zw.Close()
	}
	return nil
}

// compressReader - обертка над io.ReadCloser для распаковки gzip запросов
type compressReader struct {
	r  io.ReadCloser // оригинальный body
	zr *gzip.Reader  // gzip reader
}

// newCompressReader создает новый compressReader для распаковки запроса
func newCompressReader(r io.ReadCloser) (*compressReader, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}

	return &compressReader{
		r:  r,
		zr: zr,
	}, nil
}

// Read читает распакованные данные из gzip
func (c *compressReader) Read(p []byte) (n int, err error) {
	return c.zr.Read(p)
}

// Close закрывает gzip reader и оригинальный body
func (c *compressReader) Close() error {
	if err := c.r.Close(); err != nil {
		return err
	}
	return c.zr.Close()
}

// GzipMiddleware - middleware для автоматического gzip сжатия запросов/ответов
//
// Логика:
// 1. Если Content-Encoding: gzip - распаковывает r.Body
// 2. Если Accept-Encoding: gzip - сжимает w.Write() (только 2xx, кроме 204)
// 3. Иначе - пропускает без изменений
//
// Поддерживаемые заголовки:
// - Request: Content-Encoding: gzip, Accept-Encoding: gzip
// - Response: Content-Encoding: gzip (автоматически)
//
// Регистрация:
//
//	r.Use(middleware.GzipMiddleware)
//
// GzipMiddleware - middleware для автоматического gzip сжатия запросов/ответов
func GzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Распаковка сжатого запроса
		if strings.Contains(r.Header.Get("Content-Encoding"), "gzip") {
			cr, err := newCompressReader(r.Body)
			if err != nil {
				http.Error(w, "Failed to decompress request", http.StatusBadRequest)
				return
			}
			defer cr.Close()
			r.Body = cr

			contentType := r.Header.Get("Content-Type")
			if strings.Contains(contentType, "application/x-gzip") ||
				strings.Contains(contentType, "gzip") {
				r.Header.Set("Content-Type", "text/plain; charset=utf-8")
			}

			// Удаляем заголовок Content-Encoding
			r.Header.Del("Content-Encoding")
		}

		// 2. Сжатие ответа для клиента
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			cw := newCompressWriter(w)
			defer cw.Close()
			next.ServeHTTP(cw, r)
			return
		}

		// 3. Обычный запрос без сжатия
		next.ServeHTTP(w, r)
	})
}
