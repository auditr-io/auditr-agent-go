package lambda

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/auditr-io/lambdahooks-go"
	"github.com/aws/aws-lambda-go/events"
)

// Agent is an auditr agent that collects and reports events
type Agent struct {
	configOptions []config.Option
	publisher     Publisher

	router *Router
}

// Option is an option to override defaults
type Option func(*Agent) error

// ClientProvider is a function that returns an HTTP client
type ClientProvider func(context.Context) *http.Client

// Event is an audit event
type Event struct {
	ID          string       `json:"id"`
	Action      string       `json:"action"`
	Actor       string       `json:"actor"`
	ActorID     string       `json:"actor_id"`
	RouteType   string       `json:"route_type"`
	Route       config.Route `json:"route"`
	Location    string       `json:"location"`
	RequestID   string       `json:"request_id"`
	RequestedAt int64        `json:"requested_at"`
	Request     interface{}  `json:"request"`
	Response    interface{}  `json:"response"`
	Error       interface{}  `json:"error"`
}

// New creates a new agent instance
func New(options ...Option) (*Agent, error) {
	a := &Agent{
		publisher:     newPublisher(),
		configOptions: []config.Option{},
	}

	for _, opt := range options {
		if err := opt(a); err != nil {
			return nil, err
		}
	}

	config.Init(a.configOptions...)

	a.router = newRouter(
		config.TargetRoutes,
		config.SampledRoutes,
	)

	lambdahooks.Init(
		lambdahooks.WithPostHooks(
			a,
		),
	)

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

// Wrap wraps a handler with audit hooks
func (a *Agent) Wrap(handler interface{}) interface{} {
	return lambdahooks.Wrap(handler)
}

// AfterExecution captures the request as an audit event or a sample
// Only API Gateway events are supported at this time
func (a *Agent) AfterExecution(
	ctx context.Context,
	payload []byte,
	newPayload []byte,
	response interface{},
	err interface{},
) {
	if response == nil {
		// API Gateway expects a non-nil response
		return
	}

	responseInterface, ok := response.(interface{})
	if !ok {
		return
	}

	res, ok := (responseInterface).(events.APIGatewayProxyResponse)
	if !ok {
		return
	}

	var req events.APIGatewayProxyRequest
	e := json.Unmarshal(payload, &req)
	if e != nil {
		log.Printf("Error unmarshalling payload: %s", string(payload))
		return
	}

	// We only care about the original request, not the modified request
	a.capture(ctx, req, res, err)
}

// capture captures the request as an audit event or a sample
func (a *Agent) capture(
	ctx context.Context,
	req events.APIGatewayProxyRequest,
	res events.APIGatewayProxyResponse,
	err interface{},
) {
	route := config.Route{
		HTTPMethod: req.HTTPMethod,
		Path:       req.Path,
	}

	root, ok := a.router.target[req.HTTPMethod]
	if ok {
		handler, ps, _ := root.getValue(req.Path, a.router.getParams)
		if handler != nil {
			// route is targeted
			if ps != nil {
				a.router.putParams(ps)
			}
			// route = handler()
			a.publisher.Publish("target", route, req, res, err)
			log.Printf("route: %#v is targeted", route)
			return
		}
	}

	root, ok = a.router.sampled[req.HTTPMethod]
	if ok {
		handler, ps, _ := root.getValue(req.Path, a.router.getParams)
		if handler != nil {
			// route is already sampled
			if ps != nil {
				a.router.putParams(ps)
			}

			log.Printf("route: %#v is already sampled", route)
			return
		}
	}

	// sample the new route
	root = new(node)
	a.router.sampled[req.HTTPMethod] = root

	if req.Resource != "{proxy+}" {
		r := strings.NewReplacer("{", ":", "}", "")
		route = config.Route{
			HTTPMethod: req.HTTPMethod,
			Path:       r.Replace(req.Resource),
		}
	}

	a.publisher.Publish("sampled", route, req, res, err)
	root.addRoute(route.Path, newHandler(route.Path))
	log.Printf("route: %#v is sampled", route)
}
