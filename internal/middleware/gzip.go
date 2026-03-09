package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

type compressWriter struct {
	w            http.ResponseWriter
	zw           *gzip.Writer
	statusCode   int
	headerSet    bool
	disabled     bool
}

func newCompressWriter(w http.ResponseWriter) *compressWriter {
	return &compressWriter{
		w:         w,
		zw:        gzip.NewWriter(w),
		statusCode: http.StatusOK,
	}
}

func (c *compressWriter) Header() http.Header {
	return c.w.Header()
}

func (c *compressWriter) Write(p []byte) (int, error) {
	if c.disabled || (c.statusCode >= 300 && c.statusCode != 201) {
		return c.w.Write(p)
	}

	if !c.headerSet && c.statusCode < 300 {
		c.w.Header().Set("Content-Encoding", "gzip")
		c.headerSet = true
	}

	return c.zw.Write(p)
}

func (c *compressWriter) WriteHeader(statusCode int) {
	c.statusCode = statusCode

	if statusCode >= 300 && statusCode != 201 {
		c.disabled = true
		c.w.WriteHeader(statusCode)
		return
	}

	c.w.Header().Set("Content-Encoding", "gzip")
	c.headerSet = true
	c.w.WriteHeader(statusCode)
}

func (c *compressWriter) Close() error {
	if c.zw != nil {
		return c.zw.Close()
	}
	return nil
}

type compressReader struct {
	r  io.ReadCloser
	zr *gzip.Reader
}

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

func (c compressReader) Read(p []byte) (n int, err error) {
	return c.zr.Read(p)
}

func (c *compressReader) Close() error {
	if err := c.r.Close(); err != nil {
		return err
	}
	return c.zr.Close()
}

func GzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ow := w

		acceptEncoding := r.Header.Get("Accept-Encoding")
		supportsGzip := strings.Contains(acceptEncoding, "gzip")

		contentType := r.Header.Get("Content-Type")
		isTextContent := strings.Contains(contentType, "text/plain") ||
						strings.Contains(contentType, "application/json") ||
						contentType == ""

		path := r.URL.Path
		isShortPath := len(path) > 1 && len(path) < 10 && !strings.Contains(path, "/")

		if supportsGzip && isTextContent && !isShortPath {
			cw := newCompressWriter(w)
			ow = cw
			defer cw.Close()
		}

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

func GzipMiddlewareForTests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}
