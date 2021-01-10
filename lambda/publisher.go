package lambda

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/aws/aws-lambda-go/events"
	"github.com/segmentio/ksuid"
)

// Publisher publishes events to a receiving endpoint
type Publisher interface {
	// Publish creates an audit event and sends it to a listener
	Publish(
		routeType RouteType,
		route *config.Route,
		request events.APIGatewayProxyRequest,
		response events.APIGatewayProxyResponse,
		errorValue interface{},
	)
}

// publisher publishes audit events to auditr
type publisher struct{}

func newPublisher() *publisher {
	return &publisher{}
}

// Publish creates an audit event and sends it to auditr
func (p *publisher) Publish(
	routeType RouteType,
	route *config.Route,
	request events.APIGatewayProxyRequest,
	response events.APIGatewayProxyResponse,
	errorValue interface{},
) {
	event := p.buildEvent(routeType, route, request, response, errorValue)

	e, err := json.Marshal(event)
	if err != nil {
		log.Println("Error in marshalling ", err)
		return
	}

	p.sendEventBytes(e)
}

// buildEvent builds an event from given parameters
func (p *publisher) buildEvent(
	routeType RouteType,
	route *config.Route,
	request events.APIGatewayProxyRequest,
	response events.APIGatewayProxyResponse,
	errorValue interface{},
) *Event {
	event := &Event{
		ID:          ksuid.New().String(),
		Actor:       "user@auditr.io",
		ActorID:     "6b45a096-0e41-42c0-ab71-e6ec29e23fee",
		Action:      request.HTTPMethod,
		Location:    request.RequestContext.Identity.SourceIP,
		RequestID:   request.RequestContext.RequestID,
		RequestedAt: time.Now().UTC().Unix(),
		RouteType:   routeType,
		Route:       route,
		Request:     request,
		Response:    response,
		Error:       errorValue,
	}

	if request.RequestContext.RequestTimeEpoch > 0 {
		event.RequestedAt = request.RequestContext.RequestTimeEpoch
	}

	return event
}

func (p *publisher) sendEventBytes(event []byte) {
	req, err := http.NewRequest("POST", config.EventsURL, bytes.NewBuffer(event))
	if err != nil {
		log.Println("Error http.NewRequest:", err)
		return
	}

	req.Close = true
	req.Header.Set("Content-Type", "application/json")

	resp, err := config.GetClient(context.Background()).Do(req)
	if err != nil {
		log.Println("Error client.Do(req):", err)
		return
	}

	if resp.Body == nil {
		return
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("ioutil.ReadAll(resp.Body):", err)
		return
	}
	defer resp.Body.Close()

	log.Println("response Body:", string(body))
}
