package collect

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/stretchr/testify/assert"

	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/auditr-io/auditr-agent-go/lambda/events"
	"github.com/auditr-io/auditr-agent-go/test"
	"github.com/stretchr/testify/mock"
)

type mockBuilder struct {
	mock.Mock
	fn func(
		m *mockBuilder,
		routeType RouteType,
		route *config.Route,
		request interface{},
		response interface{},
		errorValue interface{},
	) (*Event, error)
}

func (m *mockBuilder) Build(
	routeType RouteType,
	route *config.Route,
	request interface{},
	response interface{},
	errorValue interface{},
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
		Action:    expectedRequest.HTTPMethod,
		Location:  expectedRequest.RequestContext.Identity.SourceIP,
		RequestID: expectedRequest.RequestContext.RequestID,
		RouteType: RouteTypeTarget,
		Route: &config.Route{
			HTTPMethod: expectedRequest.HTTPMethod,
			Path:       "/person/:id",
		},
		Request:  expectedRequest,
		Response: events.APIGatewayProxyResponse{},
		Error: errorMessage{
			Error: "test error",
		},
	}

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

	configResponse := func() (int, []byte) {
		statusCode := 200
		cfgJSON, _ := json.Marshal(cfg)

		return statusCode, cfgJSON
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

	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
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

	config.Init(
		config.WithHTTPClient(func(ctx context.Context) *http.Client {
			return &http.Client{
				Transport: m,
			}
		}),
	)

	b := &mockBuilder{
		fn: func(
			m *mockBuilder,
			routeType RouteType,
			route *config.Route,
			request interface{},
			response interface{},
			errorValue interface{},
		) (*Event, error) {
			m.MethodCalled(
				"Build",
				routeType,
				route,
				request,
				response,
				errorValue,
			)

			e := expectedEvent
			e.ID = "evt_xxxxx"

			return e, nil
		},
	}

	b.On(
		"Build",
		expectedEvent.RouteType,
		expectedEvent.Route,
		expectedEvent.Request.(events.APIGatewayProxyRequest),
		expectedEvent.Response.(events.APIGatewayProxyResponse),
		expectedEvent.Error,
	).Return(expectedEvent, nil).Once()

	p, err := NewEventPublisher([]EventBuilder{b})
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
		expectedEvent.Route,
		expectedEvent.Request.(events.APIGatewayProxyRequest),
		expectedEvent.Response.(events.APIGatewayProxyResponse),
		expectedEvent.Error,
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
		Action:    expectedRequest.HTTPMethod,
		Location:  expectedRequest.RequestContext.Identity.SourceIP,
		RequestID: expectedRequest.RequestContext.RequestID,
		RouteType: RouteTypeTarget,
		Route: &config.Route{
			HTTPMethod: expectedRequest.HTTPMethod,
			Path:       "/person/:id",
		},
		Request:  expectedRequest,
		Response: events.APIGatewayProxyResponse{},
		Error: errorMessage{
			Error: "test error",
		},
	}

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

	configResponse := func() (int, []byte) {
		statusCode := 200
		cfgJSON, _ := json.Marshal(cfg)

		return statusCode, cfgJSON
	}

	eventResponse := func() (int, []byte) {
		statusCode := 200

		return statusCode, []byte(`[
			{
				"status": 200
			}
		]`)
	}

	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			var statusCode int
			var responseBody []byte
			switch req.URL.String() {
			case config.ConfigURL:
				statusCode, responseBody = configResponse()
			case config.EventsURL:
				log.Printf("\n\nhello\n\n")
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

	config.Init(
		config.WithHTTPClient(func(ctx context.Context) *http.Client {
			return &http.Client{
				Transport: m,
			}
		}),
	)

	b := &mockBuilder{
		fn: func(
			m *mockBuilder,
			routeType RouteType,
			route *config.Route,
			request interface{},
			response interface{},
			errorValue interface{},
		) (*Event, error) {
			m.MethodCalled(
				"Build",
				routeType,
				route,
				request,
				response,
				errorValue,
			)

			e := expectedEvent
			e.ID = "evt_xxxxx"

			return e, nil
		},
	}

	b.On(
		"Build",
		expectedEvent.RouteType,
		expectedEvent.Route,
		expectedEvent.Request.(events.APIGatewayProxyRequest),
		expectedEvent.Response.(events.APIGatewayProxyResponse),
		expectedEvent.Error,
	).Return(expectedEvent, nil).Once()

	p, err := NewEventPublisher([]EventBuilder{b})
	assert.NoError(t, err)

	p.Publish(
		expectedEvent.RouteType,
		expectedEvent.Route,
		expectedEvent.Request.(events.APIGatewayProxyRequest),
		expectedEvent.Response.(events.APIGatewayProxyResponse),
		expectedEvent.Error,
	)

	p.Flush()

	assert.True(t, m.AssertExpectations(t))
	assert.True(t, b.AssertExpectations(t))
}
