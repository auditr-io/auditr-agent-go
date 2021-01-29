package lambda

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/auditr-io/auditr-agent-go/collector"
	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/auditr-io/lambdahooks-go"
	"github.com/aws/aws-lambda-go/events"
)

// Agent is an auditr agent that collects and reports events
type Agent struct {
	configOptions []config.Option
	publisher     collector.Publisher
	router        *collector.Router
}

// AgentOption is an option to override defaults
type AgentOption func(*Agent) error

// ClientProvider is a function that returns an HTTP client
type ClientProvider func(context.Context) *http.Client

// NewAgent creates a new agent instance
func NewAgent(options ...AgentOption) (*Agent, error) {
	a := &Agent{
		configOptions: []config.Option{},
	}

	b := []collector.EventBuilder{
		&APIGatewayEventBuilder{},
	}

	p, err := collector.NewEventPublisher(b)
	if err != nil {
		return nil, err
	}

	a.publisher = p

	for _, opt := range options {
		if err := opt(a); err != nil {
			return nil, err
		}
	}

	// TODO: put on routine
	config.Init(a.configOptions...)

	a.router = collector.NewRouter(
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
func WithHTTPClient(client ClientProvider) AgentOption {
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
	errorValue interface{},
) {
	if response == nil {
		// API Gateway expects a non-nil response
		return
	}

	responseInterface, ok := response.(interface{})
	if !ok {
		return
	}

	// TODO: support HTTP API and Websockets
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
	a.capture(ctx, req, res, errorValue)
}

// capture captures the request as an audit event or a sample
func (a *Agent) capture(
	ctx context.Context,
	req events.APIGatewayProxyRequest,
	res events.APIGatewayProxyResponse,
	errorValue interface{},
) {
	route, err := a.router.FindRoute(collector.RouteTypeTarget, req.HTTPMethod, req.Path)
	if err != nil {
		panic(err)
	}

	if route != nil {
		a.publisher.Publish(collector.RouteTypeTarget, route, req, res, errorValue)
		log.Printf("route: %#v is targeted", route)
		return
	}

	route, err = a.router.FindRoute(collector.RouteTypeSampled, req.HTTPMethod, req.Path)
	if err != nil {
		panic(err)
	}

	if route != nil {
		log.Printf("route: %#v is already sampled", route)
		return
	}

	// Sample the new route
	route = a.router.SampleRoute(req.HTTPMethod, req.Path, req.Resource)
	if route != nil {
		log.Printf("route: %#v is sampled", route)
		a.publisher.Publish(collector.RouteTypeSampled, route, req, res, errorValue)
		return
	}
}
