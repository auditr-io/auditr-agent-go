package collect

import (
	"context"
	"encoding/json"
	"log"

	"github.com/auditr-io/auditr-agent-go/config"
)

// Collector determines whether to collect a request as an audit or sample event
type Collector struct {
	configOptions []config.ConfigOption
	router        *Router
	publisher     Publisher

	setupReadyc chan struct{}
}

// CollectorOptions are options to override default settings
// type CollectorOptions struct {
// 	HTTPClient           *http.Client // do we need this?
// 	MaxEventsPerBatch    uint
// 	SendInterval         time.Duration
// 	MaxConcurrentBatches uint
// 	PendingWorkCapacity  uint
// 	BlockOnSend          bool
// 	BlockOnResponse      bool
// }

// NewCollector creates a new collector instance
func NewCollector(
	builders []EventBuilder,
	// options *CollectorOptions,
	options *PublisherOptions,
	configOptions ...config.ConfigOption,
) (*Collector, error) {
	c := &Collector{
		configOptions: []config.ConfigOption{},
		setupReadyc:   make(chan struct{}, 1),
	}

	go func() {
		config.Init(configOptions...)

		c.router = NewRouter(
			config.TargetRoutes,
			config.SampleRoutes,
		)

		close(c.setupReadyc)
	}()

	// p, err := NewEventPublisher(
	// 	builders,
	// 	WithMaxEventsPerBatch(options.MaxEventsPerBatch),
	// 	WithMaxConcurrentBatches(options.MaxConcurrentBatches),
	// 	WithSendInterval(options.SendInterval),
	// 	WithPendingWorkCapacity(options.PendingWorkCapacity),
	// 	WithBlockOnSend(options.BlockOnSend),
	// 	WithBlockOnResponse(options.BlockOnResponse),
	// )
	p, err := NewEventPublisherWithOptions(
		builders,
		options,
	)
	if err != nil {
		return nil, err
	}

	c.publisher = p

	return c, nil
}

// Collect captures the request as an audit event or a sample
func (c *Collector) Collect(
	ctx context.Context,
	httpMethod string,
	path string,
	resource string,
	request interface{},
	response json.RawMessage,
	errorValue json.RawMessage,
) {
	<-c.setupReadyc
	route, err := c.router.FindRoute(RouteTypeTarget, httpMethod, path)
	if err != nil {
		panic(err)
	}

	defer func() {
		if config.Flush {
			c.Flush()
		}
	}()

	if route != nil {
		c.publisher.Publish(RouteTypeTarget, route, request, response, errorValue)
		log.Printf("route: %#v is targeted", route)
		return
	}

	route, err = c.router.FindRoute(RouteTypeSample, httpMethod, path)
	if err != nil {
		panic(err)
	}

	if route == nil {
		log.Printf("route is nil when finding method %s path %s\n", httpMethod, path)
		log.Printf("sampled %#v\n", c.router.sample)
		root, ok := c.router.sample[httpMethod]
		if ok {
			log.Printf("sampled[GET] %#v\n", root)
		}
	}

	if route != nil {
		log.Printf("route: %#v is already sampled", route)
		return
	}

	// Sample the new route
	route = c.router.SampleRoute(httpMethod, path, resource)
	if route != nil {
		log.Printf("route: %#v is sampled", route)
		c.publisher.Publish(RouteTypeSample, route, request, response, errorValue)
		return
	}
}

// SetupReady returns a channel indicating whether setup is complete
func (c *Collector) SetupReady() <-chan struct{} {
	return c.setupReadyc
}

// Responses return a response channel
func (c *Collector) Responses() <-chan Response {
	return c.publisher.(*EventPublisher).Responses()
}

// Flush sends anything pending in queue
func (c *Collector) Flush() error {
	return c.publisher.(*EventPublisher).Flush()
}
