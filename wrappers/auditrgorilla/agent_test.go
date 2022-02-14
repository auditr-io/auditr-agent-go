package auditrgorilla

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
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type hiHandler struct {
	ServeFn func(h hiHandler, w http.ResponseWriter, r *http.Request)
}

func (h hiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.ServeFn(h, w, r)
}

func TestNewAgent_ReturnsAgent(t *testing.T) {
	configurer, err := config.NewConfigurer(
		config.WithConfigProvider(func() ([]byte, error) {
			return []byte(`{
				"parent_org_id": "org_xxx",
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

func TestMiddleware(t *testing.T) {
	wantResBodyBytes := []byte(`{
		"id": 123,
		"name": "homer"
	}`)
	wantResStatusCode := 200

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
				"parent_org_id": "org_xxx",
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

	router := mux.NewRouter()
	router.Use(a.Middleware)
	router.Handle("/hi/{id}", hiHandler{
		ServeFn: func(h hiHandler, w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(wantResStatusCode)
			w.Write(wantResBodyBytes)
		},
	})

	reqBodyBytes := []byte(`{
		"name": "homer"
	}`)
	req, _ := http.NewRequest("POST", "/hi/123", bytes.NewBuffer(reqBodyBytes))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	result := rec.Result()
	assert.Equal(t, wantResStatusCode, result.StatusCode)

	resBodyBytes, _ := ioutil.ReadAll(result.Body)
	assert.Equal(t, wantResBodyBytes, resBodyBytes)
}
