package collect

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"sync"
	"testing"

	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/auditr-io/auditr-agent-go/test"
	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestRefreshRouter(t *testing.T) {
	configs := []struct {
		bytes  []byte
		config *config.Configuration
	}{
		{
			bytes: []byte(`{
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
			}`),
		},
		{
			bytes: []byte(`{
				"base_url": "https://dev-api.auditr.io/v1",
				"events_path": "/events",
				"target": [],
				"sample": [
					{
						"method": "GET",
						"path": "/person/:id"
					}
				],
				"flush": false,
				"cache_duration": 5,
				"max_events_per_batch": 10,
				"max_concurrent_batches": 10,
				"pending_work_capacity": 20,
				"send_interval": 20,
				"block_on_send": false,
				"block_on_response": true
			}`),
		},
	}

	for i := range configs {
		json.Unmarshal(configs[i].bytes, &configs[i].config)
	}

	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

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

	fileEventChan := make(chan fsnotify.Event)

	configProviders := func() func() ([]byte, error) {
		i := 0
		return func() ([]byte, error) {
			bytes := configs[i].bytes
			i = (i + 1) % len(configs)
			return bytes, nil
		}
	}

	configIter := configProviders()
	c, err := config.NewConfigurer(
		config.WithConfigProvider(configIter),
		config.WithFileEventChan(fileEventChan),
		config.WithHTTPClient(func() *http.Client {
			return &http.Client{
				Transport: m,
			}
		}),
	)
	assert.NoError(t, err)

	ctx := context.Background()
	err = c.Refresh(ctx)
	assert.NoError(t, err)

	collector, err := NewCollector(
		[]EventBuilder{},
		c.Configuration,
	)
	assert.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		for i := 0; i < len(configs); i++ {
			cfg := <-c.Configured()

			if i == 0 {
				assert.Equal(t, configs[i].config.TargetRoutes, cfg.TargetRoutes)

				c.OnRefresh(func() {})

				route, err := collector.router.FindRoute(RouteTypeTarget, http.MethodGet, "/person/xyz")
				assert.NoError(t, err)
				assert.NotNil(t, route)

				fileEventChan <- fsnotify.Event{
					Op:   fsnotify.Write,
					Name: config.ConfigPath,
				}
			} else {
				assert.Equal(t, configs[i].config.SampleRoutes, cfg.SampleRoutes)

				c.OnRefresh(func() {})

				<-collector.routerRefreshedc

				route, err := collector.router.FindRoute(RouteTypeSample, http.MethodGet, "/person/xyz")
				assert.NoError(t, err)
				assert.NotNil(t, route)
			}
		}
	}()

	wg.Wait()
}
