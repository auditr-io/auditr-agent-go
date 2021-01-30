package lambda

import (
	"context"
	"encoding/json"
	"log"

	"github.com/auditr-io/auditr-agent-go/collect"
	"github.com/auditr-io/lambdahooks-go"
	"github.com/aws/aws-lambda-go/events"
)

// Agent is an auditr agent that collects and reports events
type Agent struct {
	collector *collect.Collector
}

// NewAgent creates a new agent instance
func NewAgent(options ...collect.CollectorOption) (*Agent, error) {
	a := &Agent{}

	c, err := collect.NewCollector(
		[]collect.EventBuilder{
			&APIGatewayEventBuilder{},
		},
		options...,
	)
	if err != nil {
		return nil, err
	}

	a.collector = c

	lambdahooks.Init(
		lambdahooks.WithPostHooks(
			a,
		),
	)

	return a, nil
}

// Wrap wraps a handler with audit hooks
func (a *Agent) Wrap(handler interface{}) interface{} {
	return lambdahooks.Wrap(handler)
}

// AfterExecution captures the request as an audit event or a sample.
// Only API Gateway events are supported at this time.
func (a *Agent) AfterExecution(
	ctx context.Context,
	payload []byte,
	newPayload []byte,
	response interface{},
	errorValue interface{},
) {
	a.Collect(
		ctx,
		payload,
		newPayload,
		response,
		errorValue,
	)
}

// Collect captures the request as an audit event or a sample.
// Only API Gateway events are supported at this time.
func (a *Agent) Collect(
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

	// TODO: support HTTP API and Websockets
	_, ok := response.(events.APIGatewayProxyResponse)
	if !ok {
		return
	}

	var req events.APIGatewayProxyRequest
	// We only care about the original request, not the modified request.
	// So, we use payload here.
	err := json.Unmarshal(payload, &req)
	if err != nil {
		log.Printf("Error unmarshalling payload: %s", string(payload))
		return
	}

	a.collector.Collect(
		ctx,
		req.HTTPMethod,
		req.Path,
		req.Resource,
		req,
		response,
		errorValue,
	)
}
