package config

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// mockTransport is a mock Transport client
type mockTransport struct {
	mock.Mock
	http.Transport
	fn func(m *mockTransport, req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if m.fn != nil {
		return m.fn(m, req)
	}

	return m.successRoundTripResponse()
}

func (m *mockTransport) successRoundTripResponse() (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
	}, nil
}

func TestSeedConfig(t *testing.T) {
	configResponse := func() (int, []byte) {
		statusCode := 200

		return statusCode, []byte(`{}`)
	}

	m := &mockTransport{
		fn: func(m *mockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			var statusCode int
			var responseBody []byte
			switch req.URL.String() {
			case ConfigURL:
				statusCode, responseBody = configResponse()
			}

			r := ioutil.NopCloser(bytes.NewBuffer(responseBody))

			return &http.Response{
				StatusCode: statusCode,
				Body:       r,
			}, nil
		},
	}

	m.
		On("RoundTrip", mock.AnythingOfType("*http.Request")).
		Return(mock.AnythingOfType("*http.Response"), nil).
		Once()

	mockClient := func(ctx context.Context) *http.Client {
		return &http.Client{
			Transport: m,
		}
	}

	err := Init(
		withHTTPClient(mockClient),
	)

	assert.NoError(t, err)
	assert.True(t, m.AssertExpectations(t))

	tests := map[string]struct {
		getVar func() interface{}
		value  interface{}
	}{
		"ConfigURL": {
			getVar: func() interface{} { return viper.GetString("auditr_config_url") },
			value:  ConfigURL,
		},
		"TokenURL": {
			getVar: func() interface{} { return viper.GetString("auditr_token_url") },
			value:  TokenURL,
		},
		"ClientID": {
			getVar: func() interface{} { return viper.GetString("auditr_client_id") },
			value:  ClientID,
		},
		"ClientSecret": {
			getVar: func() interface{} { return viper.GetString("auditr_client_secret") },
			value:  ClientSecret,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.getVar(), tc.value)
		})
	}
}

func TestAcquiredConfig(t *testing.T) {
	expected := struct {
		BaseURL       string   `json:"base_url"`
		EventsPath    string   `json:"events_path"`
		TargetRoutes  []string `json:"target"`
		SampledRoutes []string `json:"sampled"`
	}{
		BaseURL:       "https://dev-api.auditr.io/v1",
		EventsPath:    "/events",
		TargetRoutes:  []string{"POST /events", "PUT /events/:id"},
		SampledRoutes: []string{"GET /events", "GET /events/:id"},
	}

	configResponse := func() (int, []byte) {
		statusCode := 200
		expectedJSON, _ := json.Marshal(expected)

		return statusCode, expectedJSON
	}

	m := &mockTransport{
		fn: func(m *mockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			var statusCode int
			var responseBody []byte
			switch req.URL.String() {
			case ConfigURL:
				statusCode, responseBody = configResponse()
			}

			r := ioutil.NopCloser(bytes.NewBuffer(responseBody))

			return &http.Response{
				StatusCode: statusCode,
				Body:       r,
			}, nil
		},
	}

	m.
		On("RoundTrip", mock.AnythingOfType("*http.Request")).
		Return(mock.AnythingOfType("*http.Response"), nil).
		Once()

	mockClient := func(ctx context.Context) *http.Client {
		return &http.Client{
			Transport: m,
		}
	}

	err := Init(
		withHTTPClient(mockClient),
	)

	assert.NoError(t, err)
	assert.True(t, m.AssertExpectations(t))
	assert.Equal(t, expected.BaseURL, BaseURL)

	expectedEventsURL, err := url.Parse(expected.BaseURL)
	assert.NoError(t, err)
	expectedEventsURL.Path = path.Join(expectedEventsURL.Path, expected.EventsPath)
	assert.Equal(t, expectedEventsURL.String(), EventsURL)
	assert.Equal(t, expected.TargetRoutes, TargetRoutes)
	assert.Equal(t, expected.SampledRoutes, SampledRoutes)
}
