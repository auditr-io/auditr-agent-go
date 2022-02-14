package auditrgorilla

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"

	"github.com/auditr-io/auditr-agent-go/collect"
	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/auditr-io/auditr-agent-go/wrappers/common"
	"github.com/gorilla/mux"
)

// Agent is an auditr agent that collects and reports events
// Usage:
//   agent, err := auditrhttp.NewAgent()
type Agent struct {
	collector *collect.Collector
	fetcher   *config.Fetcher
}

// NewAgent creates a new agent with default configuration
func NewAgent() (*Agent, error) {
	f, err := config.NewFetcher(config.FetcherOptions{})
	if err != nil {
		return nil, err
	}

	f.Refresh(context.Background())

	a, err := NewAgentWithConfiguration(nil)
	if err != nil {
		return nil, err
	}

	a.fetcher = f
	return a, nil
}

// NewAgentWithConfigurartion creates a new agent with overriden configuration
func NewAgentWithConfiguration(
	configuration *config.Configuration,
) (*Agent, error) {
	a := &Agent{}

	c, err := collect.NewCollector(
		[]collect.EventBuilder{
			&common.HTTPEventBuilder{},
		},
		configuration,
	)
	if err != nil {
		return nil, err
	}

	a.collector = c
	return a, nil
}

// Middleware audits HTTP handlers
func (a *Agent) Middleware(handler http.Handler) http.Handler {
	wrappedHandler := func(w http.ResponseWriter, req *http.Request) {
		cw := common.NewCopyWriter(w)

		resource := ""
		route := mux.CurrentRoute(req)
		if route != nil {
			r, err := route.GetPathTemplate()
			if err != nil {
				// despite the error, we'll still send what we got
				log.Printf("resource path not defined")
			} else {
				resource = r
			}
		}

		reqCopy := common.HTTPRequest{
			Method:  req.Method,
			URL:     req.URL,
			Headers: req.Header.Clone(),
		}

		if reqCopy.Headers.Get("X-Forwarded-For") == "" {
			if req.RemoteAddr != "" {
				if ip, port, err := net.SplitHostPort(req.RemoteAddr); err == nil {
					reqCopy.Headers.Set("Remote-Address-Ip", ip)
					reqCopy.Headers.Set("Remote-Address-Port", port)
				}
			}
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

		result := cw.Response()

		bodyBytes, err := io.ReadAll(result.Body)
		if err != nil && err != io.ErrUnexpectedEOF {
			// despite the error, we'll still send what we got
			log.Printf("failed to read body")
		}

		res := common.HTTPResponse{
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

// Fetches returns the stream of refreshed configs
// Config may be nil if refresh failed
func (a *Agent) Fetches() <-chan []byte {
	return a.fetcher.Refreshes()
}

// FetchErrors returns the stream of errors
func (a *Agent) FetchErrors() <-chan error {
	return a.fetcher.Errors()
}
