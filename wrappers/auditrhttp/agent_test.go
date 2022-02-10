package auditrhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/auditr-io/auditr-agent-go/collect"
	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/auditr-io/auditr-agent-go/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestNewAgent_ReturnsAgent(t *testing.T) {
	configurer, err := config.NewConfigurer(
		config.WithConfigProvider(func() ([]byte, error) {
			return []byte(`{
				"base_url": "https://dev-api.auditr.io/v1",
				"events_path": "/events",
				"target": [
					{
						"method": "GET",
						"path": "/person/:id"
					}
				],
				"sample": [],
				"flush": false,
				"cache_duration": 2,
				"max_events_per_batch": 10,
				"max_concurrent_batches": 10,
				"pending_work_capacity": 20,
				"send_interval": 20,
				"block_on_send": false,
				"block_on_response": true
			}`), nil
		}),
	)
	assert.NoError(t, err)

	configurer.Refresh(context.Background())

	a, err := NewAgentWithConfiguration(configurer.Configuration)
	assert.NoError(t, err)
	assert.NotNil(t, a)
}

func TestWrapHandler(t *testing.T) {
	expectedStatusCode := 200
	expectedHeaders := http.Header{
		"Content-Type": {"application/json"},
	}
	expectedBody := struct {
		Hi string
	}{
		Hi: "you",
	}
	expectedBodyBuf, _ := json.Marshal(expectedBody)

	reqBody := struct {
		Name string
	}{
		Name: "homer",
	}
	reqBodyBuf, _ := json.Marshal(reqBody)
	r, _ := http.NewRequest("POST", "/hi/123", bytes.NewBuffer(reqBodyBuf))
	w := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("/hi/", func(w http.ResponseWriter, _ *http.Request) {
		for k, v := range expectedHeaders {
			w.Header().Add(k, v[0])
		}
		w.WriteHeader(expectedStatusCode)
		w.Write(expectedBodyBuf)
	})

	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)
			log.Printf("roundtrip %s", req.URL.String())

			reqBody, err := ioutil.ReadAll(req.Body)
			assert.NoError(t, err)

			var eventBatch []*collect.EventRaw
			err = json.Unmarshal(reqBody, &eventBatch)
			assert.NoError(t, err)
			event := eventBatch[0]
			assert.Equal(t, collect.RouteTypeTarget, event.Route.Type)

			r := ioutil.NopCloser(bytes.NewBuffer([]byte(`[
				{
					"status": 200
				}
			]`)))

			return &http.Response{
				StatusCode: 200,
				Body:       r,
			}, nil
		},
	}

	m.
		On("RoundTrip", mock.AnythingOfType("*http.Request")).
		Return(mock.AnythingOfType("*http.Response"), nil).Once()

	mockClient := func() *http.Client {
		return &http.Client{
			Transport: m,
		}
	}

	configurer, err := config.NewConfigurer(
		config.WithConfigProvider(func() ([]byte, error) {
			return []byte(`{
				"base_url": "https://dev-api.auditr.io/v1",
				"events_path": "/events",
				"target": [
					{
						"method": "POST",
						"path": "/hi/:id"
					}
				],
				"sample": [],
				"flush": true,
				"cache_duration": 2,
				"max_events_per_batch": 10,
				"max_concurrent_batches": 10,
				"pending_work_capacity": 20,
				"send_interval": 20,
				"block_on_send": false,
				"block_on_response": true
			}`), nil
		}),
		config.WithHTTPClient(mockClient),
	)
	assert.NoError(t, err)

	configurer.Refresh(context.Background())

	a, err := NewAgentWithConfiguration(configurer.Configuration)
	assert.NoError(t, err)

	a.WrapHandler(mux).ServeHTTP(w, r)

	res := cw.recorder.Result()
	actual := w.Result()
	assert.Equal(t, expectedStatusCode, res.StatusCode)
	assert.Equal(t, expectedStatusCode, actual.StatusCode)
	assert.Equal(t, expectedHeaders, res.Header)
	assert.Equal(t, expectedHeaders, actual.Header)
	// resBody, _ := ioutil.ReadAll(res.Body)
	// assert.Equal(t, expectedBodyBuf, resBody)
	actualBody, _ := ioutil.ReadAll(actual.Body)
	assert.Equal(t, expectedBodyBuf, actualBody)
}
