package auditrhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/auditr-io/auditr-agent-go/collect"
	"github.com/auditr-io/auditr-agent-go/config"
)

type copyWriter struct {
	recorder       *httptest.ResponseRecorder
	bodyWriter     io.Writer
	responseWriter http.ResponseWriter
}

func CopyWriter(w http.ResponseWriter) *copyWriter {
	recorder := httptest.NewRecorder()

	return &copyWriter{
		recorder:       recorder,
		bodyWriter:     io.MultiWriter(w, recorder),
		responseWriter: w,
	}
}

func (c *copyWriter) Header() http.Header {
	return c.responseWriter.Header()
}

func (c *copyWriter) Write(p []byte) (int, error) {
	return c.bodyWriter.Write(p)
}

func (c *copyWriter) WriteHeader(statusCode int) {
	for k, v := range c.responseWriter.Header() {
		c.recorder.Header().Add(k, v[0])
	}
	c.responseWriter.WriteHeader(statusCode)
	c.recorder.WriteHeader(statusCode)
}

var (
	cw *copyWriter
)

// Agent is an auditr agent that collects and reports events
// Usage:
//   agent, err := auditrhttp.NewAgent()
type Agent struct {
	collector *collect.Collector
}

// NewAgent creates a new agent with default configuration
func NewAgent() (*Agent, error) {
	return NewAgentWithConfiguration(nil)
}

// NewAgentWithConfigurartion creates a new agent with overriden configuration
func NewAgentWithConfiguration(
	configuration *config.Configuration,
) (*Agent, error) {
	a := &Agent{}

	c, err := collect.NewCollector(
		[]collect.EventBuilder{
			&HTTPEventBuilder{},
		},
		configuration,
	)
	if err != nil {
		return nil, err
	}

	a.collector = c
	return a, nil
}

// WrapHandler wraps an HTTP Handler (e.g. http.ServeMux) to enable auditing
func (a *Agent) WrapHandler(handler http.Handler) http.Handler {
	wrappedHandler := func(w http.ResponseWriter, req *http.Request) {
		cw = CopyWriter(w)

		reqCopy := HTTPRequest{
			Method:  req.Method,
			URL:     req.URL,
			Headers: req.Header,
		}

		if req.Body != nil {
			reqBody, err := ioutil.ReadAll(req.Body)
			if err != nil {
				// despite the error, we'll still send what we got
				log.Printf("error reading request body: %v", err)
			}

			// reset body for actual & copy
			req.Body = ioutil.NopCloser(bytes.NewBuffer(reqBody))
			reqCopy.Body = string(reqBody)
		}

		handler.ServeHTTP(cw, req)

		resource := ""
		mux, ok := handler.(*http.ServeMux)
		if ok {
			// http.ServeMux only matches on exact paths
			// we can match on parameterized paths and will still
			// achieve the results of filtering/aggregating events
			// by the same matching parameterized path
			_, rsrc := mux.Handler(req)
			resource = rsrc
		}

		result := cw.recorder.Result()

		bodyBytes := make([]byte, 100000)
		_, err := io.ReadFull(result.Body, bodyBytes)
		if err != nil && err != io.ErrUnexpectedEOF {
			// despite the error, we'll still send what we got
			log.Printf("failed to read body")
		}

		res := HTTPResponse{
			StatusCode: result.StatusCode,
			Headers:    result.Header,
			Body:       string(bodyBytes),
		}

		resBytes, err := json.Marshal(res)
		if err != nil {
			// despite the error, we'll still send what we got
			log.Printf("failed to marshal response")
		}

		a.collector.Collect(
			context.Background(),
			reqCopy.Method,
			reqCopy.URL.Path,
			resource,
			reqCopy,
			resBytes,
			nil,
		)
	}

	return http.HandlerFunc(wrappedHandler)
}

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
