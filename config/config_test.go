package config

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"io/ioutil"
	"net/http"
	"os"
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
		Return(mock.AnythingOfType("*http.Response"), nil)

	mockClient := func(ctx context.Context) *http.Client {
		return &http.Client{
			Transport: m,
		}
	}

	err := Init(
		WithHTTPClient(mockClient),
	)

	assert.NoError(t, err)
	// assert.True(t, m.AssertExpectations(t))

	tests := map[string]struct {
		getVar func() interface{}
		value  interface{}
	}{
		"ConfigURL": {
			getVar: func() interface{} { return viper.GetString("auditr_config_url") },
			value:  ConfigURL,
		},
		"APIKey": {
			getVar: func() interface{} { return viper.GetString("auditr_api_key") },
			value:  APIKey,
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
		BaseURL       string  `json:"base_url"`
		EventsPath    string  `json:"events_path"`
		TargetRoutes  []Route `json:"target"`
		SampleRoutes  []Route `json:"sample"`
		CacheDuration int64   `json:"cache_duration"`
		Flush         bool    `json:"flush"`
	}{
		BaseURL:    "https://dev-api.auditr.io/v1",
		EventsPath: "/events",
		TargetRoutes: []Route{
			{
				HTTPMethod: http.MethodPost,
				Path:       "/events",
			}, {
				HTTPMethod: http.MethodPut,
				Path:       "/events/:id",
			},
		},
		SampleRoutes: []Route{
			{
				HTTPMethod: http.MethodGet,
				Path:       "/events",
			}, {
				HTTPMethod: http.MethodGet,
				Path:       "/events/:id",
			},
		},
		CacheDuration: int64(3 * 60),
		Flush:         true,
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
		Return(mock.AnythingOfType("*http.Response"), nil)
		// .Once()

	mockClient := func(ctx context.Context) *http.Client {
		return &http.Client{
			Transport: m,
		}
	}

	err := Init(
		WithHTTPClient(mockClient),
	)

	assert.NoError(t, err)
	assert.True(t, m.AssertExpectations(t))
	// assert.Equal(t, expected.BaseURL, BaseURL)

	// expectedEventsURL, err := url.Parse(expected.BaseURL)
	// assert.NoError(t, err)
	// expectedEventsURL.Path = path.Join(expectedEventsURL.Path, expected.EventsPath)
	// assert.Equal(t, expectedEventsURL.String(), EventsURL)
	// assert.Equal(t, expected.TargetRoutes, TargetRoutes)
	// assert.Equal(t, expected.SampleRoutes, SampleRoutes)
}

func TestWatcher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir, err := os.MkdirTemp("/tmp", "cfg*")
	assert.NoError(t, err)

	done, err := watchFile(ctx, dir)
	assert.NoError(t, err)

	f1, err := os.CreateTemp(dir, "cfgfile*")
	assert.NoError(t, err)
	f1.Sync()
	defer f1.Close()
	f1Info, err := f1.Stat()
	assert.NoError(t, err)
	assert.Equal(t, f1.Name(), dir+"/"+f1Info.Name())

	err = ioutil.WriteFile(f1.Name(), []byte(`test`), fs.ModeAppend)
	assert.NoError(t, err)

	cancel()

	<-done
}
