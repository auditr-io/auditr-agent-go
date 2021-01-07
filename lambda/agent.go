package lambda

import (
	"context"
	"log"
	"net/http"

	"github.com/auditr-io/auditr-agent-go/config"
)

type PostHook interface {
	AfterExecution(
		ctx context.Context,
		payload []byte,
		returnValue interface{},
		err error,
	)
}

func (a *Agent) RegisterPostHook(hook PostHook) {
	a.postHooks = append(a.postHooks, hook)
}

// Agent is an auditr agent that collects and reports events
type Agent struct {
	Publisher     publisher
	target        *node
	sampled       *node
	configOptions []config.Option
	postHooks     []PostHook
}

// Option is an option to override defaults
type Option func(*Agent) error

// ClientProvider is a function that returns an HTTP client
type ClientProvider func(context.Context) *http.Client

type key struct{}
type eventTypeKey key

// Event is an audit event
type Event struct {
	ID          string      `json:"id"`
	Action      string      `json:"action"`
	Actor       string      `json:"actor"`
	ActorID     string      `json:"actor_id"`
	RouteType   string      `json:"route_type"`
	Route       string      `json:"route"`
	Location    string      `json:"location"`
	RequestID   string      `json:"request_id"`
	RequestedAt int64       `json:"requested_at"`
	Request     interface{} `json:"request"`
	Response    interface{} `json:"response"`
	Error       error       `json:"error"`
}

// Handle is a function that can be registered to a route to handle HTTP
// requests. Like http.HandlerFunc, but has a third parameter for the values of
// wildcards (path variables).
type Handle func() string

// Param is a single URL parameter, consisting of a key and a value.
type Param struct {
	Key   string
	Value string
}

// Params is a Param-slice, as returned by the router.
// The slice is ordered, the first URL parameter is also the first slice value.
// It is therefore safe to read values by the index.
type Params []Param

func getParams() *Params {
	ps := make(Params, 0, 20)
	return &ps
}

func newHandler(route string) Handle {
	return func() string {
		return route
	}
}

// New creates a new agent instance
func New(options ...Option) (*Agent, error) {
	a := &Agent{
		Publisher:     newPublisher(),
		target:        &node{},
		sampled:       &node{},
		configOptions: []config.Option{},
	}

	for _, opt := range options {
		if err := opt(a); err != nil {
			return nil, err
		}
	}

	config.Init(a.configOptions...)

	for _, route := range config.TargetRoutes {
		a.target.addRoute(route, newHandler(route))
	}
	for _, route := range config.SampledRoutes {
		a.sampled.addRoute(route, newHandler(route))
	}

	return a, nil
}

// WithHTTPClient overrides the default HTTP client with given client
func WithHTTPClient(client ClientProvider) Option {
	return func(a *Agent) error {
		a.configOptions = append(
			a.configOptions,
			config.WithHTTPClient(config.ClientProvider(client)),
		)
		return nil
	}
}

// AfterExecution captures the request as an audit event or a sample
func (a *Agent) AfterExecution(
	ctx context.Context,
	payload []byte,
	returnValue interface{},
	err error,
) {
	log.Printf("Capture %#v %s %#v %s", ctx, string(payload), returnValue, err)
}

// Capture captures the request as an audit event or a sample
func (a *Agent) Capture(
	ctx context.Context,
	payload []byte,
	returnValue interface{},
	err error,
) {
	log.Printf("Capture %#v %s %#v %s", ctx, string(payload), returnValue, err)
}
