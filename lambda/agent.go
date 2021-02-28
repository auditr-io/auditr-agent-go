package lambda

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"

	"github.com/auditr-io/auditr-agent-go/collect"
	"github.com/auditr-io/auditr-agent-go/lambda/events"
	"github.com/auditr-io/lambdahooks-go"
)

// Agent is an auditr agent that collects and reports events
type Agent struct {
	collector *collect.Collector
	hooksInit sync.Once
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

	return a, nil
}

// Wrap wraps a handler with audit hooks
func (a *Agent) Wrap(handler interface{}) interface{} {
	a.hooksInit.Do(func() {
		lambdahooks.Init(
			lambdahooks.WithPostHooks(a),
		)
	})

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
	res, _ := json.Marshal(response)
	errValue, _ := json.Marshal(errorValue)

	a.Collect(
		ctx,
		payload,
		newPayload,
		res,
		errValue,
	)
}

// Collect captures the request as an audit event or a sample.
// Only API Gateway events are supported at this time.
func (a *Agent) Collect(
	ctx context.Context,
	payload json.RawMessage,
	newPayload json.RawMessage,
	response json.RawMessage,
	errorValue json.RawMessage,
) {
	// TODO: support HTTP API and Websockets
	if len(response) == 0 {
		// API Gateway expects a non-nil response
		return
	}

	// var res events.APIGatewayProxyResponse
	// err := json.Unmarshal(response, &res)
	// if err != nil {
	// 	// Non API Gateway response is not supported at this time
	// 	return
	// }

	var req events.APIGatewayProxyRequest
	// We only care about the original request, not the modified request.
	// So, we use payload here.
	err := json.Unmarshal(payload, &req)
	if err != nil {
		log.Printf("Error unmarshalling payload: %s\n%v", string(payload), err)
		return
	}

	path := req.Path
	if req.RequestContext.Stage != "" {
		path = strings.TrimPrefix(path, "/"+req.RequestContext.Stage)
	}

	a.collector.Collect(
		ctx,
		req.HTTPMethod,
		path,
		req.Resource,
		req,
		response,
		errorValue,
	)
}

// Flush sends anything pending in queue
func (a *Agent) Flush() error {
	return a.collector.Flush()
}

// Responses return a response channel
func (a *Agent) Responses() <-chan collect.Response {
	return a.collector.Responses()
}
