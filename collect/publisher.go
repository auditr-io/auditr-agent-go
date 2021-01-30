package collect

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/facebookgo/muster"
)

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
	maxEventsPerBatch    uint
	sendInterval         time.Duration
	maxConcurrentBatches uint
	pendingWorkCapacity  uint
	blockOnSend          bool
	blockOnResponse      bool

	eventBuilders []EventBuilder
	batchMaker    func() muster.Batch
	muster        *muster.Client
	musterLock    sync.RWMutex
	responses     chan Response
}

// PublisherOption is an option to override defaults
type PublisherOption func(p *EventPublisher) error

// NewEventPublisher creates a new EventPublisher.
// A list of event builders is required to map the parameters
// to an Event. The event builders are evaluated in order and
// stops at the first builder that successfully maps to an Event.
func NewEventPublisher(
	eventBuilders []EventBuilder,
	options ...PublisherOption,
) (*EventPublisher, error) {
	p := &EventPublisher{
		eventBuilders:        eventBuilders,
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

// Publish creates an audit event and sends it to auditr.
// The event builders are evaluated in order and
// stops at the first builder that successfully maps to an Event.
func (p *EventPublisher) Publish(
	routeType RouteType,
	route *config.Route,
	request interface{},
	response interface{},
	errorValue interface{},
) {
	for _, b := range p.eventBuilders {
		event, err := b.Build(
			routeType,
			route,
			request,
			response,
			errorValue,
		)

		if err != nil {
			// Builder couldn't build event. Move to the next builder.
			continue
		}

		if event != nil {
			p.Add(event)
			return
		}
	}

	res := Response{
		Err: fmt.Errorf("Unable to build event"),
	}
	writeToChannel(p.responses, res, p.blockOnResponse)
}

// Responses returns the response channel to read responses from
func (p *EventPublisher) Responses() <-chan Response {
	return p.responses
}

// Flush sends anything pending in muster
func (p *EventPublisher) Flush() error {
	// There isn't a way to flush a muster.Client directly, so we have to stop
	// the old one (which has a side-effect of flushing the data) and make a new
	// one. We start the new one and swap it with the old one so that we minimize
	// the time we hold the musterLock for.
	m := p.muster
	newMuster := p.createMuster()
	err := newMuster.Start()
	if err != nil {
		return err
	}

	p.musterLock.Lock()
	p.muster = newMuster
	p.musterLock.Unlock()
	return m.Stop()
}
