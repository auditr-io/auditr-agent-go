package test

import (
	"net/http"

	"github.com/stretchr/testify/mock"
)

type RoundTrip func(m *MockTransport, req *http.Request) (*http.Response, error)

// MockTransport is a mock Transport client
type MockTransport struct {
	mock.Mock
	http.Transport
	Fn RoundTrip
}

func (m *MockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.Fn(m, req)
}
