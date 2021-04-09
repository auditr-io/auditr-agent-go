package collect

import (
	"context"
	"encoding/json"
	"log"

	"github.com/auditr-io/auditr-agent-go/config"
)

// Collector determines whether to collect a request as an audit or sample event
type Collector struct {
	configuration *config.Configuration
	router        *Router
	publisher     Publisher
}

// NewCollector creates a new collector instance
func NewCollector(
	builders []EventBuilder,
	configuration *config.Configuration, // can be nil
) (*Collector, error) {
	c := &Collector{
		configuration: configuration,
	}

	if configuration == nil {
		config.Init()
		c.configuration = config.GetConfig()
	}

	c.router = NewRouter(
		c.configuration.TargetRoutes,
		c.configuration.SampleRoutes,
	)

	p, err := NewEventPublisher(
		c.configuration,
		builders,
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
	c.configuration.Configurer.Refresh(ctx)

	log.Printf("config: BaseURL: %s, EventsURL: %s, TargetRoutes: %v, SampleRoutes %v, Flush: %t, MaxEventsPerBatch: %d, MaxConcurrentBatches: %d, PendingWorkCapacity: %d, SendInterval: %d, BlockOnSend: %t, BlockOnResponse: %t",
		c.configuration.BaseURL,
		c.configuration.EventsURL,
		c.configuration.TargetRoutes,
		c.configuration.SampleRoutes,
		c.configuration.Flush,
		c.configuration.MaxEventsPerBatch,
		c.configuration.MaxConcurrentBatches,
		c.configuration.PendingWorkCapacity,
		c.configuration.SendInterval,
		c.configuration.BlockOnSend,
		c.configuration.BlockOnResponse,
	)

	route, err := c.router.FindRoute(RouteTypeTarget, httpMethod, path)
	if err != nil {
		panic(err)
	}

	defer func() {
		if c.configuration.Flush {
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

// Responses return a response channel
func (c *Collector) Responses() <-chan Response {
	return c.publisher.(*EventPublisher).Responses()
}

// Flush sends anything pending in queue
func (c *Collector) Flush() error {
	return c.publisher.(*EventPublisher).Flush()
}
