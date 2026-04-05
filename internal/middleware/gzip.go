// Package middleware предоставляет HTTP middleware для сжатия данных.
// Поддерживает сжатие ответов (gzip) и распаковку запросов (gzip).
package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

// compressWriter реализует http.ResponseWriter с поддержкой gzip сжатия.
// Прозрачно сжимает данные ответа перед отправкой клиенту.
type compressWriter struct {
	w  http.ResponseWriter // оригинальный ResponseWriter
	zw *gzip.Writer        // gzip writer для сжатия данных
}

// newCompressWriter создает новый compressWriter.
// Параметры:
//   - w: оригинальный http.ResponseWriter
//
// Возвращает: инициализированный compressWriter
func newCompressWriter(w http.ResponseWriter) *compressWriter {
	return &compressWriter{
		w:  w,
		zw: gzip.NewWriter(w),
	}
}

// Header возвращает заголовки HTTP ответа.
// Реализует интерфейс http.ResponseWriter.
func (c *compressWriter) Header() http.Header {
	return c.w.Header()
}

// Write записывает сжатые данные в ответ.
// Данные сжимаются с помощью gzip и отправляются клиенту.
// Реализует интерфейс http.ResponseWriter.
func (c *compressWriter) Write(p []byte) (int, error) {
	return c.zw.Write(p)
}

// WriteHeader отправляет HTTP статус код.
// Добавляет заголовок "Content-Encoding: gzip" только для успешных ответов (статус < 300).
// Реализует интерфейс http.ResponseWriter.
func (c *compressWriter) WriteHeader(statusCode int) {
	if statusCode < 300 {
		c.w.Header().Set("Content-Encoding", "gzip")
	}
	c.w.WriteHeader(statusCode)
}

// Close закрывает gzip writer и освобождает ресурсы.
// Должен вызываться после завершения записи ответа.
func (c *compressWriter) Close() error {
	return c.zw.Close()
}

// compressReader реализует io.ReadCloser для чтения сжатых gzip данных.
// Прозрачно распаковывает данные при чтении.
type compressReader struct {
	r  io.ReadCloser // оригинальный ReadCloser
	zr *gzip.Reader  // gzip reader для распаковки
}

// newCompressReader создает новый compressReader.
// Параметры:
//   - r: оригинальный io.ReadCloser (обычно r.Body)
//
// Возвращает:
//   - *compressReader: инициализированный compressReader
//   - error: ошибка при создании gzip.Reader
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

// Read читает распакованные данные.
// Реализует интерфейс io.Reader.
func (c compressReader) Read(p []byte) (n int, err error) {
	return c.zr.Read(p)
}

// Close закрывает оригинальный ReadCloser и gzip reader.
// Реализует интерфейс io.Closer.
func (c *compressReader) Close() error {
	if err := c.r.Close(); err != nil {
		return err
	}
	return c.zr.Close()
}

// GzipMiddleware предоставляет middleware для сжатия HTTP ответов и распаковки запросов.
//
// Функциональность:
//   - Сжатие ответов: если клиент поддерживает gzip (заголовок Accept-Encoding),
//     ответ будет сжат перед отправкой
//   - Распаковка запросов: если тело запроса сжато (заголовок Content-Encoding: gzip),
//     оно будет автоматически распаковано
//
// Использование:
//
//	router := http.NewServeMux()
//	router.HandleFunc("/api/data", dataHandler)
//
//	// Применяем middleware
//	handler := middleware.GzipMiddleware(router)
//	http.ListenAndServe(":8080", handler)
//
// Параметры:
//   - next: следующий обработчик в цепочке middleware
//
// Возвращает: http.Handler с поддержкой gzip сжатия/распаковки
func GzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ow := w
		// Проверяем поддержку gzip клиентом
		acceptEncoding := r.Header.Get("Accept-Encoding")
		supportsGzip := strings.Contains(acceptEncoding, "gzip")
		if supportsGzip {
			cw := newCompressWriter(w)
			ow = cw
			defer cw.Close()
		}

		// Проверяем, сжато ли тело запроса
		contentEncoding := r.Header.Get("Content-Encoding")
		sendsGzip := strings.Contains(contentEncoding, "gzip")
		if sendsGzip {
			cr, err := newCompressReader(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			r.Body = cr
			defer cr.Close()
		}

		next.ServeHTTP(ow, r)
	})
}
