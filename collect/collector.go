package collect

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	"github.com/auditr-io/auditr-agent-go/config"
)

// Collector determines whether to collect a request as an audit or sample event
type Collector struct {
	configuration *config.Configuration
	router        *Router
	routerLock    sync.Mutex
	publisher     Publisher

	routerRefreshedc chan struct{}
}

// NewCollector creates a new collector instance
func NewCollector(
	builders []EventBuilder,
	configuration *config.Configuration, // can be nil
) (*Collector, error) {
	c := &Collector{
		configuration:    configuration,
		routerRefreshedc: make(chan struct{}),
	}

	if configuration == nil {
		config.Init()
		c.configuration = config.GetConfig()
	}

	c.router = NewRouter(
		c.configuration.TargetRoutes,
		c.configuration.SampleRoutes,
	)

	c.configuration.Configurer.OnRefresh(c.refreshRouter)

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

// refreshRouter refreshes the routes upon a config refresh
// not thread safe
func (c *Collector) refreshRouter() {
	log.Printf("refreshRouter %+v", c.configuration)
	r := NewRouter(
		c.configuration.TargetRoutes,
		c.configuration.SampleRoutes,
	)

	c.routerLock.Lock()
	c.router = r
	c.routerLock.Unlock()

	select {
	case c.routerRefreshedc <- struct{}{}:
	default:
	}
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

	log.Printf("config: %+v", c.configuration)

	c.routerLock.Lock()
	route, err := c.router.FindRoute(RouteTypeTarget, httpMethod, path)
	c.routerLock.Unlock()
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

	c.routerLock.Lock()
	route, err = c.router.FindRoute(RouteTypeSample, httpMethod, path)
	c.routerLock.Unlock()
	if err != nil {
		panic(err)
	}

	if route == nil {
		c.routerLock.Lock()
		log.Printf("route is nil when finding method %s path %s\n", httpMethod, path)
		log.Printf("sampled %#v\n", c.router.sample)
		root, ok := c.router.sample[httpMethod]
		c.routerLock.Unlock()
		if ok {
			log.Printf("sampled[GET] %#v\n", root)
		}
	}

	if route != nil {
		log.Printf("route: %#v is already sampled", route)
		return
	}

	// Sample the new route
	c.routerLock.Lock()
	route = c.router.SampleRoute(httpMethod, path, resource)
	c.routerLock.Unlock()
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
