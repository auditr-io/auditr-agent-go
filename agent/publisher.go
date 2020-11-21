package agent

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/aws/aws-lambda-go/events"
	"github.com/segmentio/ksuid"
)

type publisher interface {
	// Publish creates an audit event and sends it to a listener
	Publish(route string, request events.APIGatewayProxyRequest, response interface{}, err error)
}

// Publisher facilitates the publishing of audit events to auditr
type Publisher struct {
	client *http.Client
}

func newPublisher() *Publisher {
	return &Publisher{
		client: createHTTPClient(&http.Transport{}),
	}
}

// Publish creates an audit event and sends it to auditr
func (p *Publisher) Publish(
	route string,
	request events.APIGatewayProxyRequest,
	response interface{},
	err error) {
	event := p.buildEvent(route, request, response, err)

	e, err := json.Marshal(event)
	if err != nil {
		log.Println("Error in marshalling ", err)
		return
	}

	p.sendEventBytes(e)
}

func (p *Publisher) buildEvent(
	route string,
	request events.APIGatewayProxyRequest,
	response interface{},
	err error) *Event {
	event := &Event{
		ID:          ksuid.New().String(),
		Actor:       "user@auditr.io",
		ActorID:     "6b45a096-0e41-42c0-ab71-e6ec29e23fee",
		Action:      request.HTTPMethod,
		Location:    request.RequestContext.Identity.SourceIP,
		RequestID:   request.RequestContext.RequestID,
		RequestedAt: time.Now().Unix(),
		Resource:    route,
		Request:     request,
		Response:    response,
		Error:       err,
	}

	if request.RequestContext.RequestTimeEpoch > 0 {
		event.RequestedAt = request.RequestContext.RequestTimeEpoch
	}

	return event
}

func (p *Publisher) sendEventBytes(event []byte) {
	req, err := http.NewRequest("POST", config.EventsUrl, bytes.NewBuffer(event))
	if err != nil {
		log.Println("Error http.NewRequest:", err)
		return
	}

	req.Close = true
	req.Header.Set("Authorization", "Bearer blablabla")
	req.Header.Set("X-Auditr-Org-ID", "1kXXAxhc0J0D7RqKjFTmq91TJ5J") // get from env
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
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

	log.Println("response Body:", string(body))

	resp.Body.Close()
}

func createHTTPClient(transport http.RoundTripper) *http.Client {
	// transport := &http.Transport{}

	return &http.Client{
		Transport: transport,
	}
}
