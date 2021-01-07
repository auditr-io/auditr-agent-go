package lambda

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/segmentio/ksuid"
	"github.com/stretchr/testify/assert"
)

type TestTransport func(req *http.Request) (*http.Response, error)

func (f TestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestPublisher(fn TestTransport) *Publisher {
	return &Publisher{
		client: createHTTPClient(TestTransport(fn)),
	}
}

func TestPublish(t *testing.T) {
	request := events.APIGatewayProxyRequest{}

	type resp struct {
		hello string
	}

	response := &resp{
		hello: "world",
	}

	p := newTestPublisher(func(req *http.Request) (*http.Response, error) {
		assert.NotNil(t, req)

		body, err := ioutil.ReadAll(req.Body)

		event := Event{}
		err = json.Unmarshal(body, &event)
		assert.Nil(t, err)
		assert.NotEmpty(t, event.ID)

		return &(http.Response{}), nil
	})

	p.Publish("sampled", "/hello", request, response, nil)
}

func TestBuildEvent(t *testing.T) {
	request := events.APIGatewayProxyRequest{
		HTTPMethod: "POST",
		Resource:   "/events/{id}",
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				SourceIP: "123.45.67.89",
			},
			RequestID:        ksuid.New().String(),
			RequestTimeEpoch: time.Now().Unix(),
		},
	}

	type resp struct {
		hello string
	}

	response := &resp{
		hello: "world",
	}

	err := errors.New("test error")

	p := newPublisher()
	routeType := "sampled"
	route := "/events/:id"
	event := p.buildEvent(routeType, route, request, response, err)

	assert.NotEmpty(t, event.ID, "EventID is empty")
	assert.Equal(t, request.HTTPMethod, event.Action, "Action didn't match")
	assert.Equal(t, routeType, event.RouteType, "RouteType didn't match")
	assert.Equal(t, route, event.Route, "Route didn't match")
	assert.Equal(t, request.RequestContext.Identity.SourceIP, event.Location, "Location didn't match")
	assert.Equal(t, request.RequestContext.RequestID, event.RequestID, "RequestID didn't match")
	assert.NotEmpty(t, event.RequestedAt, "RequestedAt is empty")
	assert.Equal(t, request, event.Request, "Request didn't match")
	assert.Equal(t, response, event.Response, "Response didn't match")
	assert.Equal(t, err, event.Error, "Error didn't match")
}
