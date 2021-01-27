package lambda

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"testing"

	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type roundTrip func(m *mockTransport, req *http.Request) (*http.Response, error)

// mockTransport is a mock Transport client
type mockTransport struct {
	mock.Mock
	http.Transport
	fn roundTrip
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.fn(m, req)
}

func TestNewAgent_ReturnsAgent(t *testing.T) {
	configResponse := func() (int, []byte) {
		cfg := struct {
			BaseURL       string         `json:"base_url"`
			EventsPath    string         `json:"events_path"`
			TargetRoutes  []config.Route `json:"target"`
			SampledRoutes []config.Route `json:"sampled"`
		}{
			BaseURL:    "https://dev-api.auditr.io/v1",
			EventsPath: "/events",
			TargetRoutes: []config.Route{
				{
					HTTPMethod: http.MethodPost,
					Path:       "/events",
				}, {
					HTTPMethod: http.MethodPut,
					Path:       "/events/:id",
				},
			},
			SampledRoutes: []config.Route{
				{
					HTTPMethod: http.MethodGet,
					Path:       "/events",
				}, {
					HTTPMethod: http.MethodGet,
					Path:       "/events/:id",
				},
			},
		}

		responseBody, _ := json.Marshal(cfg)
		statusCode := 200

		return statusCode, responseBody
	}

	m := &mockTransport{
		fn: func(m *mockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			var statusCode int
			var responseBody []byte
			switch req.URL.String() {
			case config.ConfigURL:
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

	a, err := New(
		WithHTTPClient(mockClient),
	)
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

	configResponse := func() (int, []byte) {
		cfg := struct {
			BaseURL       string         `json:"base_url"`
			EventsPath    string         `json:"events_path"`
			TargetRoutes  []config.Route `json:"target"`
			SampledRoutes []config.Route `json:"sampled"`
		}{
			BaseURL:    "https://dev-api.auditr.io/v1",
			EventsPath: "/events",
			TargetRoutes: []config.Route{
				{
					HTTPMethod: http.MethodPost,
					Path:       "/events",
				}, {
					HTTPMethod: http.MethodPut,
					Path:       "/events/:id",
				},
			},
			SampledRoutes: []config.Route{
				{
					HTTPMethod: http.MethodGet,
					Path:       "/events",
				},
			},
		}

		responseBody, _ := json.Marshal(cfg)
		statusCode := 200

		return statusCode, responseBody
	}

	eventResponse := func() (int, []byte) {
		statusCode := 200
		// eventJSON, _ := json.Marshal(event)

		return statusCode, []byte(`[
			{
				"status": 200
			}
		]`)
	}

	m := &mockTransport{
		fn: func(m *mockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			var statusCode int
			var responseBody []byte
			switch req.URL.String() {
			case config.ConfigURL:
				statusCode, responseBody = configResponse()
			case config.EventsURL:
				reqBody, err := ioutil.ReadAll(req.Body)
				assert.NoError(t, err)

				var eventBatch []*Event
				err = json.Unmarshal(reqBody, &eventBatch)
				assert.NoError(t, err)
				event := eventBatch[0]
				assert.Equal(t, RouteTypeSampled, event.RouteType)

				statusCode, responseBody = eventResponse()
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
		Twice()

	mockClient := func(ctx context.Context) *http.Client {
		return &http.Client{
			Transport: m,
		}
	}

	a, err := New(
		WithHTTPClient(mockClient),
	)
	assert.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		p := a.publisher.(*EventPublisher)
		res := <-p.responses

		expectedResponse := Response{
			StatusCode: 200,
		}
		assert.Equal(t, expectedResponse, res)
	}()

	a.AfterExecution(context.Background(), payload, payload, res, nil)

	wg.Wait()

	assert.True(t, m.AssertExpectations(t))
}

func TestAfterExecution_SkipsSampledAPIGatewayEvent(t *testing.T) {
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

	configResponse := func() (int, []byte) {
		cfg := struct {
			BaseURL       string         `json:"base_url"`
			EventsPath    string         `json:"events_path"`
			TargetRoutes  []config.Route `json:"target"`
			SampledRoutes []config.Route `json:"sampled"`
		}{
			BaseURL:    "https://dev-api.auditr.io/v1",
			EventsPath: "/events",
			TargetRoutes: []config.Route{
				{
					HTTPMethod: http.MethodPost,
					Path:       "/events",
				}, {
					HTTPMethod: http.MethodPut,
					Path:       "/events/:id",
				},
			},
			SampledRoutes: []config.Route{
				{
					HTTPMethod: http.MethodGet,
					Path:       "/events",
				},
			},
		}

		responseBody, _ := json.Marshal(cfg)
		statusCode := 200

		return statusCode, responseBody
	}

	m := &mockTransport{
		fn: func(m *mockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			var statusCode int
			var responseBody []byte
			switch req.URL.String() {
			case config.ConfigURL:
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

	a, err := New(
		WithHTTPClient(mockClient),
	)
	assert.NoError(t, err)

	a.AfterExecution(context.Background(), payload, payload, res, nil)
	assert.True(t, m.AssertExpectations(t))
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

	configResponse := func() (int, []byte) {
		cfg := struct {
			BaseURL       string         `json:"base_url"`
			EventsPath    string         `json:"events_path"`
			TargetRoutes  []config.Route `json:"target"`
			SampledRoutes []config.Route `json:"sampled"`
		}{
			BaseURL:    "https://dev-api.auditr.io/v1",
			EventsPath: "/events",
			TargetRoutes: []config.Route{
				{
					HTTPMethod: http.MethodPost,
					Path:       "/events",
				}, {
					HTTPMethod: http.MethodPut,
					Path:       "/events/:id",
				},
			},
			SampledRoutes: []config.Route{
				{
					HTTPMethod: http.MethodGet,
					Path:       "/events",
				}, {
					HTTPMethod: http.MethodGet,
					Path:       "/events/:id",
				},
			},
		}

		responseBody, _ := json.Marshal(cfg)
		statusCode := 200

		return statusCode, responseBody
	}

	eventResponse := func() (int, []byte) {
		statusCode := 200
		// eventJSON, _ := json.Marshal(event)

		return statusCode, []byte(`[
			{
				"status": 200
			}
		]`)
	}

	m := &mockTransport{
		fn: func(m *mockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			var statusCode int
			var responseBody []byte
			switch req.URL.String() {
			case config.ConfigURL:
				statusCode, responseBody = configResponse()
			case config.EventsURL:
				reqBody, err := ioutil.ReadAll(req.Body)
				assert.NoError(t, err)

				var eventBatch []*Event
				err = json.Unmarshal(reqBody, &eventBatch)
				assert.NoError(t, err)
				event := eventBatch[0]
				assert.Equal(t, RouteTypeTarget, event.RouteType)

				statusCode, responseBody = eventResponse()
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
		Twice()

	mockClient := func(ctx context.Context) *http.Client {
		return &http.Client{
			Transport: m,
		}
	}

	a, err := New(
		WithHTTPClient(mockClient),
	)
	assert.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		p := a.publisher.(*EventPublisher)
		res := <-p.responses

		expectedResponse := Response{
			StatusCode: 200,
		}
		assert.Equal(t, expectedResponse, res)
	}()

	a.AfterExecution(context.Background(), payload, payload, res, nil)

	wg.Wait()

	assert.True(t, m.AssertExpectations(t))
}
