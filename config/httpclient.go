package config

import (
	"net"
	"net/http"
	"time"

	"golang.org/x/net/http2"
)

var (
	transports map[string]*http.Transport

	DefaultHTTPClientSettings = HTTPClientSettings{
		Connect:          2 * time.Second,
		ExpectContinue:   1 * time.Second,
		IdleConn:         90 * time.Second,
		ConnKeepAlive:    30 * time.Second,
		MaxAllIdleConns:  100,
		MaxHostIdleConns: 10,
		ResponseHeader:   2 * time.Second,
		TLSHandshake:     2 * time.Second,
	}
)

// HTTPClientSettings defines the HTTP setting for clients
type HTTPClientSettings struct {
	Connect          time.Duration
	ConnKeepAlive    time.Duration
	ExpectContinue   time.Duration
	IdleConn         time.Duration
	MaxAllIdleConns  int
	MaxHostIdleConns int
	ResponseHeader   time.Duration
	TLSHandshake     time.Duration
}

type Transport struct {
	Base http.RoundTripper
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	reqBodyClosed := false
	if req.Body != nil {
		defer func() {
			if !reqBodyClosed {
				req.Body.Close()
			}
		}()
	}

	req2 := cloneRequest(req)
	req2.Header.Set("Authorization", APIKey)

	reqBodyClosed = true
	return t.Base.RoundTrip(req2)
}

func cloneRequest(r *http.Request) *http.Request {
	// shallow copy of the struct
	r2 := new(http.Request)
	*r2 = *r
	// deep copy of the Header
	r2.Header = make(http.Header, len(r.Header))
	for k, s := range r.Header {
		r2.Header[k] = append([]string(nil), s...)
	}
	return r2
}

// NewHTTPClient returns an HTTP client with reasonable default settings
// The client also attempts to re-use the same transport
func NewHTTPClient(url string) (*http.Client, error) {
	return NewHTTPClientWithSettings(url, DefaultHTTPClientSettings)
}

// NewHTTPClientWithSettings creates an HTTP client with some custom settings
func NewHTTPClientWithSettings(
	url string,
	settings HTTPClientSettings,
) (*http.Client, error) {
	tr, ok := transports[url]
	if !ok {
		tr = &http.Transport{
			ResponseHeaderTimeout: settings.ResponseHeader,
			Proxy:                 http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				KeepAlive: settings.ConnKeepAlive,
				Timeout:   settings.Connect,
			}).DialContext,
			MaxIdleConns:          settings.MaxAllIdleConns,
			IdleConnTimeout:       settings.IdleConn,
			TLSHandshakeTimeout:   settings.TLSHandshake,
			MaxIdleConnsPerHost:   settings.MaxHostIdleConns,
			ExpectContinueTimeout: settings.ExpectContinue,
		}
		transports[url] = tr
	}

	client := &http.Client{
		Transport: &Transport{
			Base: tr,
		},
	}

	// So client makes HTTP/2 requests
	err := http2.ConfigureTransport(tr)
	if err != nil {
		return client, err
	}

	return client, nil
}
