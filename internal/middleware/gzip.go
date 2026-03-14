package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

type compressWriter struct {
	w             http.ResponseWriter
	zw            *gzip.Writer
	statusCode    int
	headerWritten bool
}

func newCompressWriter(w http.ResponseWriter) *compressWriter {
	return &compressWriter{
		w:          w,
		zw:         gzip.NewWriter(w),
		statusCode: http.StatusOK,
	}
}

func (c *compressWriter) Header() http.Header {
	return c.w.Header()
}

func (c *compressWriter) Write(p []byte) (int, error) {
	if !c.headerWritten {
		c.WriteHeader(c.statusCode)
	}

	if c.statusCode >= 200 && c.statusCode < 300 {
		return c.zw.Write(p)
	}
	return c.w.Write(p)
}

func (c *compressWriter) WriteHeader(statusCode int) {
	if c.headerWritten {
		return
	}

	c.statusCode = statusCode
	c.headerWritten = true

	if statusCode >= 200 && statusCode < 300 {
		c.w.Header().Set("Content-Encoding", "gzip")
		c.w.WriteHeader(statusCode)
	} else {
		c.w.WriteHeader(statusCode)
	}
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
		if strings.Contains(r.Header.Get("Content-Encoding"), "gzip") {
			cr, err := newCompressReader(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			r.Body = cr
			defer cr.Close()
		}

		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		path := r.URL.Path
		if len(path) > 1 && !strings.Contains(path[1:], "/") {
			next.ServeHTTP(w, r)
			return
		}

		cw := newCompressWriter(w)
		defer cw.Close()

		next.ServeHTTP(cw, r)
	})
}
