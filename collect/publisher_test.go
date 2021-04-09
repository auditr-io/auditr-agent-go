package collect

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/auditr-io/auditr-agent-go/lambda/events"
	"github.com/auditr-io/auditr-agent-go/test"
	"github.com/mitchellh/mapstructure"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockBuilder struct {
	mock.Mock
	fn func(
		m *mockBuilder,
		routeType RouteType,
		route *config.Route,
		request interface{},
		response json.RawMessage,
		errorValue json.RawMessage,
	) (*Event, error)
}

func (m *mockBuilder) Build(
	routeType RouteType,
	route *config.Route,
	request interface{},
	response json.RawMessage,
	errorValue json.RawMessage,
) (*Event, error) {
	return m.fn(m, routeType, route, request, response, errorValue)
}

func TestPublish_PublishesEvent(t *testing.T) {
	expectedRequest := events.APIGatewayProxyRequest{
		HTTPMethod: http.MethodGet,
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "request-id",
			Identity: events.APIGatewayRequestIdentity{
				SourceIP: "1.2.3.4",
			},
			Authorizer: map[string]interface{}{
				"claims": map[string]interface{}{
					"username": "google_114080443625699078741",
					"iss":      "https://cognito-idp.us-west-2.amazonaws.com/us-west-2_0c4UH1XR0",
				},
			},
		},
	}

	expectedResponse := Response{
		StatusCode: 200,
	}

	type errorMessage struct {
		Error string
	}

	expectedEvent := &Event{
		Action:     expectedRequest.HTTPMethod,
		Location:   expectedRequest.RequestContext.Identity.SourceIP,
		RequestID:  expectedRequest.RequestContext.RequestID,
		RouteType:  RouteTypeTarget,
		HTTPMethod: expectedRequest.HTTPMethod,
		RoutePath:  "/person/:id",
		Request:    expectedRequest,
		Response:   events.APIGatewayProxyResponse{},
		Error: errorMessage{
			Error: "test error",
		},
	}

	expectedRoute := &config.Route{
		HTTPMethod: expectedEvent.HTTPMethod,
		Path:       expectedEvent.RoutePath,
	}

	gwRes, _ := json.Marshal(expectedEvent.Response.(events.APIGatewayProxyResponse))
	errRes, _ := json.Marshal(expectedEvent.Error)

	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			reqBody, err := ioutil.ReadAll(req.Body)
			assert.NoError(t, err)

			var eventBatch []*Event
			err = json.Unmarshal(reqBody, &eventBatch)
			assert.NoError(t, err)
			event := eventBatch[0]
			assert.True(t, strings.HasPrefix(event.ID, "evt_"))
			assert.Equal(t, expectedEvent.Action, event.Action)
			assert.Equal(t, expectedEvent.Location, event.Location)
			assert.Equal(t, expectedEvent.RequestID, event.RequestID)
			assert.GreaterOrEqual(t, time.Now().UTC().Unix(), event.RequestedAt)
			assert.Equal(t, expectedEvent.RouteType, event.RouteType)
			var eventReq events.APIGatewayProxyRequest
			mapstructure.Decode(event.Request, &eventReq)
			assert.Equal(t, expectedEvent.Request, eventReq)

			var eventRes events.APIGatewayProxyResponse
			mapstructure.Decode(event.Response, &eventRes)
			assert.Equal(t, expectedEvent.Response, eventRes)

			var eventErr errorMessage
			mapstructure.Decode(event.Error, &eventErr)
			assert.Equal(t, expectedEvent.Error, eventErr)

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
		config.WithHTTPClient(func() *http.Client {
			return &http.Client{
				Transport: m,
			}
		}),
	)

	configurer.Refresh(context.Background())

	b := &mockBuilder{
		fn: func(
			m *mockBuilder,
			routeType RouteType,
			route *config.Route,
			request interface{},
			response json.RawMessage,
			errorValue json.RawMessage,
		) (*Event, error) {
			m.MethodCalled(
				"Build",
				routeType,
				route,
				request,
				gwRes,
				errRes,
			)

			e := expectedEvent
			e.ID = "evt_xxxxx"

			return e, nil
		},
	}

	b.On(
		"Build",
		expectedEvent.RouteType,
		expectedRoute,
		expectedEvent.Request.(events.APIGatewayProxyRequest),
		gwRes,
		errRes,
	).Return(expectedEvent, nil).Once()

	p, err := NewEventPublisher(
		configurer.Configuration,
		[]EventBuilder{b},
	)
	assert.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		res := <-p.responses
		assert.Equal(t, expectedResponse, res)
	}()

	p.Publish(
		expectedEvent.RouteType,
		expectedRoute,
		expectedEvent.Request.(events.APIGatewayProxyRequest),
		gwRes,
		errRes,
	)

	wg.Wait()

	assert.True(t, m.AssertExpectations(t))
	assert.True(t, b.AssertExpectations(t))
}

func TestFlush_PublishesEvent(t *testing.T) {
	expectedRequest := events.APIGatewayProxyRequest{
		HTTPMethod: http.MethodGet,
		RequestContext: events.APIGatewayProxyRequestContext{
			RequestID: "request-id",
			Identity: events.APIGatewayRequestIdentity{
				SourceIP: "1.2.3.4",
			},
			Authorizer: map[string]interface{}{
				"claims": map[string]interface{}{
					"username": "google_114080443625699078741",
					"iss":      "https://cognito-idp.us-west-2.amazonaws.com/us-west-2_0c4UH1XR0",
				},
			},
		},
	}

	type errorMessage struct {
		Error string
	}

	expectedEvent := &Event{
		Action:     expectedRequest.HTTPMethod,
		Location:   expectedRequest.RequestContext.Identity.SourceIP,
		RequestID:  expectedRequest.RequestContext.RequestID,
		RouteType:  RouteTypeTarget,
		HTTPMethod: expectedRequest.HTTPMethod,
		RoutePath:  "/person/:id",
		Request:    expectedRequest,
		Response:   events.APIGatewayProxyResponse{},
		Error: errorMessage{
			Error: "test error",
		},
	}

	expectedRoute := &config.Route{
		HTTPMethod: expectedEvent.HTTPMethod,
		Path:       expectedEvent.RoutePath,
	}

	gwRes, _ := json.Marshal(expectedEvent.Response.(events.APIGatewayProxyResponse))
	errRes, _ := json.Marshal(expectedEvent.Error)

	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			reqBody, err := ioutil.ReadAll(req.Body)
			assert.NoError(t, err)

			var eventBatch []*Event
			err = json.Unmarshal(reqBody, &eventBatch)
			assert.NoError(t, err)
			event := eventBatch[0]
			assert.True(t, strings.HasPrefix(event.ID, "evt_"))
			assert.Equal(t, expectedEvent.Action, event.Action)
			assert.Equal(t, expectedEvent.Location, event.Location)
			assert.Equal(t, expectedEvent.RequestID, event.RequestID)
			assert.GreaterOrEqual(t, time.Now().UTC().Unix(), event.RequestedAt)
			assert.Equal(t, expectedEvent.RouteType, event.RouteType)
			var eventReq events.APIGatewayProxyRequest
			mapstructure.Decode(event.Request, &eventReq)
			assert.Equal(t, expectedEvent.Request, eventReq)

			var eventRes events.APIGatewayProxyResponse
			mapstructure.Decode(event.Response, &eventRes)
			assert.Equal(t, expectedEvent.Response, eventRes)

			var eventErr errorMessage
			mapstructure.Decode(event.Error, &eventErr)
			assert.Equal(t, expectedEvent.Error, eventErr)

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
		config.WithHTTPClient(func() *http.Client {
			return &http.Client{
				Transport: m,
			}
		}),
	)

	configurer.Refresh(context.Background())

	b := &mockBuilder{
		fn: func(
			m *mockBuilder,
			routeType RouteType,
			route *config.Route,
			request interface{},
			response json.RawMessage,
			errorValue json.RawMessage,
		) (*Event, error) {
			m.MethodCalled(
				"Build",
				routeType,
				route,
				request,
				gwRes,
				errRes,
			)

			e := expectedEvent
			e.ID = "evt_xxxxx"

			return e, nil
		},
	}

	b.On(
		"Build",
		expectedEvent.RouteType,
		expectedRoute,
		expectedEvent.Request.(events.APIGatewayProxyRequest),
		gwRes,
		errRes,
	).Return(expectedEvent, nil).Once()

	p, err := NewEventPublisher(
		configurer.Configuration,
		[]EventBuilder{b},
	)
	assert.NoError(t, err)

	p.Publish(
		expectedEvent.RouteType,
		expectedRoute,
		expectedEvent.Request.(events.APIGatewayProxyRequest),
		gwRes,
		errRes,
	)

	p.Flush()

	assert.True(t, m.AssertExpectations(t))
	assert.True(t, b.AssertExpectations(t))
}
