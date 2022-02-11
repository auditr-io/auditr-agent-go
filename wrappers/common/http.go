package common

import (
	"net/http"
	"net/url"
)

// HTTPResponse encapsulates HTTP response
type HTTPResponse struct {
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers"`
	Body       string              `json:"body"`
}

// HTTPRequest encapsulates HTTP request
type HTTPRequest struct {
	Method  string      `json:"method"`
	URL     *url.URL    `json:"url"`
	Headers http.Header `json:"headers"`
	Body    string      `json:"body"`
}
