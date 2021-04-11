package lambda

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"testing"

	"github.com/auditr-io/auditr-agent-go/collect"
	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/auditr-io/auditr-agent-go/lambda/events"
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

	configurer.Refresh(context.Background())

	a, err := NewAgentWithConfiguration(configurer.Configuration)
	assert.NoError(t, err)
	assert.NotNil(t, a)
}

func TestAfterExecution_SamplesAPIGatewayEvent(t *testing.T) {
	id := "xyz"
	req := events.APIGatewayProxyRequest{
		HTTPMethod:     http.MethodGet,
		Resource:       "/events/{id}",
		Path:           fmt.Sprintf(`/events/%s`, id),
		PathParameters: map[string]string{"id": id},
	}
	payload, err := json.Marshal(req)
	assert.NoError(t, err)

	body := fmt.Sprintf(`{"id": %s}`, id)
	res := events.APIGatewayProxyResponse{
		StatusCode: 200,
		Body:       body,
	}

	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)
			log.Printf("roundtrip %s", req.URL.String())

			reqBody, err := ioutil.ReadAll(req.Body)
			assert.NoError(t, err)

			var eventBatch []*collect.Event
			err = json.Unmarshal(reqBody, &eventBatch)
			assert.NoError(t, err)
			event := eventBatch[0]
			assert.Equal(t, collect.RouteTypeSample, event.RouteType)

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
				"target": [],
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
		config.WithHTTPClient(mockClient),
	)

	configurer.Refresh(context.Background())

	a, err := NewAgentWithConfiguration(configurer.Configuration)
	assert.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		res := <-a.Responses()

		expectedResponse := collect.Response{
			StatusCode: 200,
		}
		assert.Equal(t, expectedResponse, res)
	}()

	a.AfterExecution(context.Background(), payload, payload, res, nil)

	wg.Wait()

	m.AssertExpectations(t)
}

func TestAfterExecution_SkipsSampleAPIGatewayEvent(t *testing.T) {
	id := "xyz"
	req := events.APIGatewayProxyRequest{
		HTTPMethod: http.MethodGet,
		Resource:   "/events",
		Path:       "/events",
	}
	payload, err := json.Marshal(req)
	assert.NoError(t, err)

	body := fmt.Sprintf(`{"id": %s}`, id)
	res := events.APIGatewayProxyResponse{
		StatusCode: 200,
		Body:       body,
	}

	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			r := ioutil.NopCloser(bytes.NewBuffer([]byte("")))

			return &http.Response{
				StatusCode: 200,
				Body:       r,
			}, nil
		},
	}

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
				"target": [],
				"sample": [
					{
						"method": "GET",
						"path": "/events"
					}
				],
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
		config.WithHTTPClient(mockClient),
	)

	configurer.Refresh(context.Background())

	a, err := NewAgentWithConfiguration(configurer.Configuration)
	assert.NoError(t, err)

	a.AfterExecution(context.Background(), payload, payload, res, nil)

	m.AssertNotCalled(t, "RoundTrip", req)
}

func TestAfterExecution_TargetsAPIGatewayEvent(t *testing.T) {
	id := "xyz"
	req := events.APIGatewayProxyRequest{
		HTTPMethod: http.MethodPut,
		Resource:   "/events/{id}",
		Path:       fmt.Sprintf("/events/%s", id),
	}
	payload, err := json.Marshal(req)
	assert.NoError(t, err)

	body := fmt.Sprintf(`{"id": %s}`, id)
	res := events.APIGatewayProxyResponse{
		StatusCode: 200,
		Body:       body,
	}

	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			reqBody, err := ioutil.ReadAll(req.Body)
			assert.NoError(t, err)

			var eventBatch []*collect.Event
			err = json.Unmarshal(reqBody, &eventBatch)
			assert.NoError(t, err)
			event := eventBatch[0]
			assert.Equal(t, collect.RouteTypeTarget, event.RouteType)

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
						"method": "PUT",
						"path": "/events/:id"
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
		config.WithHTTPClient(mockClient),
	)

	configurer.Refresh(context.Background())

	a, err := NewAgentWithConfiguration(configurer.Configuration)
	assert.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		res := <-a.Responses()

		expectedResponse := collect.Response{
			StatusCode: 200,
		}
		assert.Equal(t, expectedResponse, res)
	}()

	a.AfterExecution(context.Background(), payload, payload, res, nil)

	wg.Wait()

	m.AssertExpectations(t)
}

func TestAfterExecution_TargetsAPIGatewayEventTwice(t *testing.T) {
	expectedCalls := 2
	id := "xyz"
	req := events.APIGatewayProxyRequest{
		HTTPMethod: http.MethodPut,
		Resource:   "/events/{id}",
		Path:       fmt.Sprintf("/events/%s", id),
	}
	payload, err := json.Marshal(req)
	assert.NoError(t, err)

	body := fmt.Sprintf(`{"id": %s}`, id)
	res := events.APIGatewayProxyResponse{
		StatusCode: 200,
		Body:       body,
	}

	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			reqBody, err := ioutil.ReadAll(req.Body)
			assert.NoError(t, err)

			var eventBatch []*collect.Event
			err = json.Unmarshal(reqBody, &eventBatch)
			assert.NoError(t, err)
			event := eventBatch[0]
			assert.Equal(t, collect.RouteTypeTarget, event.RouteType)

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
		Return(mock.AnythingOfType("*http.Response"), nil).Twice()

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
						"method": "PUT",
						"path": "/events/:id"
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

	configurer.Refresh(context.Background())
	configurer.OnRefresh(func() {})

	a, err := NewAgentWithConfiguration(configurer.Configuration)
	assert.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(expectedCalls)
	for i := 0; i < expectedCalls; i++ {
		go func() {
			defer wg.Done()

			// make sure to flush, else will block
			res := <-a.Responses()

			expectedResponse := collect.Response{
				StatusCode: 200,
			}
			assert.Equal(t, expectedResponse, res)
		}()
	}

	for i := 0; i < expectedCalls; i++ {
		a.AfterExecution(context.Background(), payload, payload, res, nil)
	}

	wg.Wait()

	assert.GreaterOrEqual(t, len(m.Calls), expectedCalls)
}
