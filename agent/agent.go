package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"reflect"

	"github.com/aws/aws-lambda-go/events"
)

// Agent is an auditr agent that collects and reports events
type Agent struct {
	Publisher publisher
}

type lambdaFunction func(context.Context, events.APIGatewayProxyRequest) (interface{}, error)

type key struct{}
type eventTypeKey key

// Event is an audit event
type Event struct {
	ID          string      `json:"id"`
	Action      string      `json:"action"`
	Actor       string      `json:"actor"`
	ActorID     string      `json:"actor_id"`
	Resource    string      `json:"resource"`
	Location    string      `json:"location"`
	RequestID   string      `json:"request_id"`
	RequestedAt int64       `json:"requested_at"`
	Request     interface{} `json:"request"`
	Response    interface{} `json:"response"`
	Error       error       `json:"error"`
}

// New creates a new agent instance
func New() *Agent {
	return &Agent{
		Publisher: newPublisher(),
	}
}

// Wrap wraps the handler so the agent can intercept
// and record events
func (a *Agent) Wrap(handler interface{}) interface{} {
	if handler == nil {
		return errorHandler(fmt.Errorf("handler is nil"))
	}

	handlerType := reflect.TypeOf(handler)
	handlerValue := reflect.ValueOf(handler)
	takesContext, _ := validateArguments(handlerType)

	// return func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
	return func(ctx context.Context, request events.APIGatewayProxyRequest) (interface{}, error) {
		var args []reflect.Value
		var elem reflect.Value

		if (handlerType.NumIn() == 1 && !takesContext) || handlerType.NumIn() == 2 {
			newEventType := handlerType.In(handlerType.NumIn() - 1)
			newEvent := reflect.New(newEventType)

			payload, err := json.Marshal(request)
			if err != nil {
				log.Println("Error marshalling request", err)
				return nil, err
			}

			if err := json.Unmarshal(payload, newEvent.Interface()); err != nil {
				return nil, err
			}

			elem = newEvent.Elem()
			// ctx = utils.SetEventTypeToContext(ctx, newEventType)
			ctx = context.WithValue(ctx, eventTypeKey{}, newEventType)
		}

		// TODO: execute prehooks

		if takesContext {
			args = append(args, reflect.ValueOf(ctx))
		}

		if elem.IsValid() {
			args = append(args, elem)
		}

		response := handlerValue.Call(args)

		var err error
		if len(response) > 0 {
			if errVal, ok := response[len(response)-1].Interface().(error); ok {
				err = errVal
			}
		}
		var val interface{}
		if len(response) > 1 {
			val = response[0].Interface()
		}

		if err != nil {
			val = nil
		}

		// execute posthooks
		a.Publisher.Publish(request, val, err)

		return val, err
	}
}

// errorHandler returns a stand-in lambda function that
// handles error gracefully
func errorHandler(e error) lambdaFunction {
	return func(ctx context.Context, request events.APIGatewayProxyRequest) (interface{}, error) {
		return nil, e
	}
}

// validateArguments validates the handler arguments comply
// to lambda handler signature
func validateArguments(handler reflect.Type) (bool, error) {
	handlerTakesContext := false
	if handler.NumIn() > 2 {
		return false, fmt.Errorf("handlers may not take more than two arguments, but handler takes %d", handler.NumIn())
	}

	if handler.NumIn() > 0 {
		contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
		argumentType := handler.In(0)
		handlerTakesContext = argumentType.Implements(contextType)
		if handler.NumIn() > 1 && !handlerTakesContext {
			return false, fmt.Errorf("handler takes two arguments, but the first is not Context. got %s", argumentType.Kind())
		}
	}

	return handlerTakesContext, nil
}
