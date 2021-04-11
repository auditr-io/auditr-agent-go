package collect

import (
	"encoding/json"
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
		response json.RawMessage,
		errorValue json.RawMessage,
	)
}

const (
	// version of this publisher
	version string = "0.0.1"

	// DefaultMaxEventsPerBatch is the default max number of events in a batch
	DefaultMaxEventsPerBatch uint = 10

	// DefaultSendInterval is the duration after which to send a pending batch
	DefaultSendInterval time.Duration = 100 * time.Millisecond

	// DefaultMaxConcurrentBatches is how many parallel batches before
	// we start blocking
	DefaultMaxConcurrentBatches uint = 10

	// DefaultPendingWorkCapacity is pending items to hold in the work channel
	// before blocking or overflowing
	DefaultPendingWorkCapacity uint = DefaultMaxEventsPerBatch * PendingWorkToMaxEventsRatio

	// PendingWorkToMaxEventsRatio is a safeguard to hold enough pending work
	// to minimize overflowing
	PendingWorkToMaxEventsRatio uint = 2
)

// EventPublisher publishes audit events to auditr.
// This batch handling implementation is shamelessly borrowed from
// Honeycomb's libhoney.
type EventPublisher struct {
	configuration        *config.Configuration
	eventBuilders        []EventBuilder
	maxEventsPerBatch    uint
	sendInterval         time.Duration
	maxConcurrentBatches uint
	pendingWorkCapacity  uint
	blockOnSend          bool
	blockOnResponse      bool

	batchMaker func() muster.Batch
	muster     *muster.Client
	musterLock sync.RWMutex
	responses  chan Response
}

// PublisherOption is an option to override defaults
type PublisherOption func(p *EventPublisher) error

// PublisherOptions are options to override default settings
type PublisherOptions struct {
	MaxEventsPerBatch    uint
	SendInterval         time.Duration
	MaxConcurrentBatches uint
	PendingWorkCapacity  uint
	BlockOnSend          bool
	BlockOnResponse      bool
}

// NewEventPublisher creates a new EventPublisher.
// A list of event builders is required to map the parameters
// to an Event. The event builders are evaluated in order and
// stops at the first builder that successfully maps to an Event.
func NewEventPublisher(
	configuration *config.Configuration,
	eventBuilders []EventBuilder,
) (*EventPublisher, error) {
	p := &EventPublisher{
		configuration:        configuration,
		eventBuilders:        eventBuilders,
		maxEventsPerBatch:    DefaultMaxEventsPerBatch,
		sendInterval:         DefaultSendInterval,
		maxConcurrentBatches: DefaultMaxConcurrentBatches,
		pendingWorkCapacity:  DefaultPendingWorkCapacity,
	}

	go p.configuration.Configurer.OnRefresh(func() {
		if p.configuration.MaxEventsPerBatch > 0 {
			p.maxEventsPerBatch = p.configuration.MaxEventsPerBatch
			p.pendingWorkCapacity = p.configuration.MaxEventsPerBatch * PendingWorkToMaxEventsRatio
		}

		if p.configuration.SendInterval > 0 {
			p.sendInterval = p.configuration.SendInterval
		}

		if p.configuration.MaxConcurrentBatches > 0 {
			p.maxConcurrentBatches = p.configuration.MaxConcurrentBatches
		}

		if p.configuration.PendingWorkCapacity > 0 {
			p.pendingWorkCapacity = p.configuration.PendingWorkCapacity
		}

		p.blockOnSend = p.configuration.BlockOnSend
		p.blockOnResponse = p.configuration.BlockOnResponse
	})

	// todo: recreate on config refresh?
	p.responses = make(chan Response, p.pendingWorkCapacity*2)

	p.batchMaker = func() muster.Batch {
		b := newBatchList(
			p.configuration,
			p.responses,
			// capture snapshot of the values so the batch size is
			// static once created
			p.maxEventsPerBatch,
			p.maxConcurrentBatches,
		)
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
	response json.RawMessage,
	errorValue json.RawMessage,
) {
	var event *Event
	var err error
	for _, b := range p.eventBuilders {
		event, err = b.Build(
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
		Err: fmt.Errorf("Unable to build event: %s", err),
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
