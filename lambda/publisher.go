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

	// DefaultMaxEventsPerBatch is the default max number of events in a batch
	DefaultMaxEventsPerBatch uint = 50

	// DefaultSendInterval is the duration after which to send a pending batch
	DefaultSendInterval time.Duration = 100 * time.Millisecond

	// DefaultMaxConcurrentBatches is how many parallel batches before we start blocking
	DefaultMaxConcurrentBatches uint = 10

	// DefaultPendingWorkCapacity is pending items to hold in the work channel before blocking
	DefaultPendingWorkCapacity uint = 10
)

// EventPublisher publishes audit events to auditr.
// This batch handling implementation is shamelessly borrowed from
// Honeycomb's libhoney.
type EventPublisher struct {
	batchMaker func() muster.Batch
	muster     *muster.Client
	musterLock sync.RWMutex
	responses  chan Response

	maxEventsPerBatch    uint
	sendInterval         time.Duration
	maxConcurrentBatches uint
	pendingWorkCapacity  uint
	blockOnSend          bool
	blockOnResponse      bool
}

// PublisherOption is an option to override defaults
type PublisherOption func(p *EventPublisher) error

// NewEventPublisher creates a new EventPublisher
func NewEventPublisher(options ...PublisherOption) (*EventPublisher, error) {
	p := &EventPublisher{
		maxEventsPerBatch:    DefaultMaxEventsPerBatch,
		sendInterval:         DefaultSendInterval,
		maxConcurrentBatches: DefaultMaxConcurrentBatches,
		pendingWorkCapacity:  DefaultPendingWorkCapacity,
	}

	for _, opt := range options {
		if err := opt(p); err != nil {
			return nil, err
		}
	}

	p.responses = make(chan Response, p.pendingWorkCapacity*2)

	p.batchMaker = func() muster.Batch {
		b := newBatchList(p.responses, p.maxConcurrentBatches)
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

// WithMaxEventsPerBatch sets the max events per batch
func WithMaxEventsPerBatch(events uint) PublisherOption {
	return func(p *EventPublisher) error {
		p.maxEventsPerBatch = events
		return nil
	}
}

// WithSendInterval sets the interval at which a batch is sent
func WithSendInterval(interval time.Duration) PublisherOption {
	return func(p *EventPublisher) error {
		p.sendInterval = interval
		return nil
	}
}

// WithMaxConcurrentBatches sets the max concurrent batches
func WithMaxConcurrentBatches(batches uint) PublisherOption {
	return func(p *EventPublisher) error {
		p.maxConcurrentBatches = batches
		return nil
	}
}

// WithPendingWorkCapacity sets the pending work capacity.
// This sets the number of pending events to hold in the queue before blocking.
func WithPendingWorkCapacity(capacity uint) PublisherOption {
	return func(p *EventPublisher) error {
		p.pendingWorkCapacity = capacity
		return nil
	}
}

// WithBlockOnResponse blocks on returning a response to the caller.
// When set to true, be sure to read the responses. Otherwise, if the
// channel fills up, this will block sending the events altogether.
// Defaults to false. Unread responses are simply dropped so processing
// continues uninterrupted.
func WithBlockOnResponse(block bool) PublisherOption {
	return func(p *EventPublisher) error {
		p.blockOnResponse = block
		return nil
	}
}

// WithBlockOnSend blocks when sending an event if the batch is full.
// When set to true, this will block sending the events altogether once
// the batch fills up to pendingWorkCapacity.
// Defaults to false. Overflowing events are simply dropped so processing
// continues uninterrupted.
func WithBlockOnSend(block bool) PublisherOption {
	return func(p *EventPublisher) error {
		p.blockOnSend = block
		return nil
	}
}

// Responses returns the response channel to read responses from
func (p *EventPublisher) Responses() <-chan Response {
	return p.responses
}

// createMuster creates the muster client that coordinates the batch processing
func (p *EventPublisher) createMuster() *muster.Client {
	m := new(muster.Client)
	m.MaxBatchSize = p.maxEventsPerBatch
	m.BatchTimeout = p.sendInterval
	m.MaxConcurrentBatches = p.maxConcurrentBatches
	m.PendingWorkCapacity = p.pendingWorkCapacity
	m.BatchMaker = p.batchMaker
	return m
}

// Add adds an event to the publish queue.
// Returns true if event was added, false otherwise due to a full queue.
func (p *EventPublisher) Add(event *Event) {
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
func (p *EventPublisher) Publish(
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

	p.Add(event)
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
