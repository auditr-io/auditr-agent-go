package common

import (
	"io"
	"net/http"
	"net/http/httptest"
)

// CopyWriter copies writes to a http.ResponseWriter
type CopyWriter struct {
	origWriter http.ResponseWriter

	bodyWriter io.Writer
	recorder   *httptest.ResponseRecorder
}

// NewCopyWriter creates a CopyWriter for given ResponseWriter
func NewCopyWriter(w http.ResponseWriter) *CopyWriter {
	recorder := httptest.NewRecorder()

	return &CopyWriter{
		recorder:   recorder,
		bodyWriter: io.MultiWriter(w, recorder),
		origWriter: w,
	}
}

// Header returns the headers
func (c *CopyWriter) Header() http.Header {
	return c.origWriter.Header()
}

// Response returns the HTTP response
func (c *CopyWriter) Response() *http.Response {
	return c.recorder.Result()
}

// Write dual writes to the original and its copy
func (c *CopyWriter) Write(p []byte) (int, error) {
	return c.bodyWriter.Write(p)
}

// WriteHeader writes headers and status code to original and copy
func (c *CopyWriter) WriteHeader(statusCode int) {
	for k, v := range c.origWriter.Header() {
		c.recorder.Header().Add(k, v[0])
	}
	c.origWriter.WriteHeader(statusCode)
	c.recorder.WriteHeader(statusCode)
}
