package lambda

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/aws/aws-lambda-go/events"
	"github.com/facebookgo/muster"
	"github.com/segmentio/ksuid"
)

// Actor is the user originating the action
type Actor struct {
	ID       string `json:"actor_id"`
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
	Email    string `json:"email,omitempty"`
}

// Publisher publishes events to a receiving endpoint
type Publisher interface {
	// Publish creates an audit event and sends it to a listener
	Publish(
		routeType RouteType,
		route *config.Route,
		request interface{},
		response interface{},
		errorValue interface{},
	)
}

const (
	// version of this publisher
	version string = "0.0.1"

	// max number of events in a batch
	maxBatchSize uint = 50

	// duration after which to send a pending batch
	batchTimeout time.Duration = 100 * time.Millisecond

	// how many parallel batches before we start blocking
	maxConcurrentBatches uint = 10

	// pending items to hold in the work channel before blocking
	pendingWorkCapacity uint = 10
)

// publisher publishes audit events to auditr.
// This batch handling implementation is shamelessly borrowed from
// Honeycomb's libhoney.
type publisher struct {
	batchMaker func() muster.Batch
	muster     *muster.Client
	musterLock sync.RWMutex
	responses  chan Response

	blockOnSend     bool
	blockOnResponse bool
}

// newPublisher creates a new publisher
func newPublisher() (*publisher, error) {
	p := &publisher{
		responses: make(chan Response, pendingWorkCapacity*2),
	}

	p.batchMaker = func() muster.Batch {
		b := newBatchList(p.responses)
		// TODO: withBlockOnResponse()
		b.blockOnResponse = p.blockOnResponse
		return b
	}
	p.muster = p.createMuster()
	err := p.muster.Start()
	if err != nil {
		return nil, err
	}

	return p, nil
}

// createMuster creates the muster client that coordinates the batch processing
func (p *publisher) createMuster() *muster.Client {
	m := new(muster.Client)
	m.MaxBatchSize = maxBatchSize
	m.BatchTimeout = batchTimeout
	m.MaxConcurrentBatches = maxConcurrentBatches
	m.PendingWorkCapacity = pendingWorkCapacity
	m.BatchMaker = p.batchMaker
	return m
}

// add adds an event to the publish queue.
// Returns true if event was added, false otherwise due to a full queue.
func (p *publisher) add(event *Event) {
	p.musterLock.RLock()
	defer p.musterLock.RUnlock()

	if p.blockOnSend {
		p.muster.Work <- event
		// Event queued successfully
		return
	}

	select {
	case p.muster.Work <- event:
		// Event queued successfully
		return
	default:
		// Queue is full
		res := Response{
			Err: errors.New("Queue overflow"),
		}
		writeToChannel(p.responses, res, p.blockOnResponse)
	}
}

// Publish creates an audit event and sends it to auditr
func (p *publisher) Publish(
	routeType RouteType,
	route *config.Route,
	request interface{},
	response interface{},
	errorValue interface{},
) {

	builder := &APIGatewayEventBuilder{}
	event, err := builder.Build(
		routeType,
		route,
		request,
		response,
		errorValue,
	)
	if err != nil {
		res := Response{
			Err: err,
		}
		writeToChannel(p.responses, res, p.blockOnResponse)
	}

	p.add(event)
}

// EventBuilder builds an event from the given parameters
type EventBuilder interface {
	// Build builds an event from the given parameters
	Build(
		routeType RouteType,
		route *config.Route,
		request interface{},
		response interface{},
		errorValue interface{},
	) (*Event, error)
}

// APIGatewayEventBuilder builds an event from APIGateway request and response
type APIGatewayEventBuilder struct{}

// Build builds an event from APIGateway request and response
func (b *APIGatewayEventBuilder) Build(
	routeType RouteType,
	route *config.Route,
	request interface{},
	response interface{},
	errorValue interface{},
) (*Event, error) {
	req, ok := request.(events.APIGatewayProxyRequest)
	if !ok {
		return nil, fmt.Errorf("request is not of type APIGatewayProxyRequest")
	}

	// Map identity
	// https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-logging-variables.html
	identity := req.RequestContext.Identity
	authorizer := req.RequestContext.Authorizer

	event := &Event{
		ID:          fmt.Sprintf("evt_%s", ksuid.New().String()),
		Action:      req.HTTPMethod,
		Location:    identity.SourceIP,
		RequestID:   req.RequestContext.RequestID,
		RequestedAt: time.Now().UTC().Unix(),
		RouteType:   routeType,
		Route:       route,
		Request:     req,
		Response:    response,
		Error:       errorValue,
	}

	var actor *Actor
	// Default to cognito identity
	// https://docs.aws.amazon.com/cognito/latest/developerguide/amazon-cognito-user-pools-using-tokens-with-identity-providers.html
	if identity.CognitoIdentityID != "" {
		// go to openid config url
		// get userinfo endpoint
		// get userinfo w token
		// populate fields
		actor = &Actor{
			ID:       identity.CognitoIdentityID,
			Name:     authorizer["name"].(string),
			Username: authorizer["cognito:username"].(string),
			Email:    authorizer["email"].(string),
		}
	} else {
		// Try custom authorizer principal next
		principalID, ok := authorizer["principalId"]
		if ok {
			actor = &Actor{
				ID:       principalID.(string),
				Username: principalID.(string),
			}
		} else {
			// Finally, try IAM user
			actor = &Actor{
				ID:       identity.UserArn,
				Username: identity.User,
			}
		}
	}
	event.Actor = actor

	if req.RequestContext.RequestTimeEpoch > 0 {
		event.RequestedAt = req.RequestContext.RequestTimeEpoch
	}

	return event, nil
}
