package collect

import (
	"context"
	"log"
	"net/http"

	"github.com/auditr-io/auditr-agent-go/config"
)

// Collector determines whether to collect a request as an audit or sample event
type Collector struct {
	configOptions []config.ConfigOption
	router        *Router
	publisher     Publisher
}

// CollectorOption is an option to override defaults
type CollectorOption func(*Collector) error

// ClientProvider is a function that returns an HTTP client
type ClientProvider func(context.Context) *http.Client

// NewCollector creates a new collector instance
func NewCollector(
	builders []EventBuilder,
	options ...CollectorOption,
) (*Collector, error) {
	c := &Collector{
		configOptions: []config.ConfigOption{},
	}

	for _, opt := range options {
		if err := opt(c); err != nil {
			return nil, err
		}
	}

	// TODO: put on routine
	config.Init(c.configOptions...)

	c.router = NewRouter(
		config.TargetRoutes,
		config.SampledRoutes,
	)

	p, err := NewEventPublisher(builders)
	if err != nil {
		return nil, err
	}

	c.publisher = p

	return c, nil
}

// WithHTTPClient overrides the default HTTP client with given client
func WithHTTPClient(client ClientProvider) CollectorOption {
	return func(c *Collector) error {
		c.configOptions = append(
			c.configOptions,
			config.WithHTTPClient(config.ClientProvider(client)),
		)
		return nil
	}
}

// Collect captures the request as an audit event or a sample
func (c *Collector) Collect(
	ctx context.Context,
	httpMethod string,
	path string,
	resource string,
	request interface{},
	response interface{},
	errorValue interface{},
) {
	route, err := c.router.FindRoute(RouteTypeTarget, httpMethod, path)
	if err != nil {
		panic(err)
	}

	if route != nil {
		c.publisher.Publish(RouteTypeTarget, route, request, response, errorValue)
		log.Printf("route: %#v is targeted", route)
		return
	}

	route, err = c.router.FindRoute(RouteTypeSampled, httpMethod, path)
	if err != nil {
		panic(err)
	}

	if route != nil {
		log.Printf("route: %#v is already sampled", route)
		return
	}

	// Sample the new route
	route = c.router.SampleRoute(httpMethod, path, resource)
	if route != nil {
		log.Printf("route: %#v is sampled", route)
		c.publisher.Publish(RouteTypeSampled, route, request, response, errorValue)
		return
	}
}

// Responses return a response channel
func (c *Collector) Responses() <-chan Response {
	return c.publisher.(*EventPublisher).Responses()
}

// Flush sends anything pending in queue
func (c *Collector) Flush() error {
	return c.publisher.(*EventPublisher).Flush()
}
