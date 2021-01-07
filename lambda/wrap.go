package lambda

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	awslambda "github.com/aws/aws-lambda-go/lambda"
)

// lambdaFunction is old
type lambdaFunction func(context.Context, events.APIGatewayProxyRequest) (interface{}, error)

// lambdaHandler is the generic function type
type lambdaHandler func(context.Context, []byte) (interface{}, error)

// Invoke calls the handler, and serializes the response.
// If the underlying handler returned an error, or an error occurs during serialization, error is returned.
func (handler lambdaHandler) Invoke(ctx context.Context, payload []byte) ([]byte, error) {
	response, err := handler(ctx, payload)
	if err != nil {
		return nil, err
	}

	responseBytes, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}

	return responseBytes, nil
}

// Wrap wraps the handler so the agent can intercept and record events
// Processes the handler according to the lambda rules below:
// Rules:
//
// 	* handler must be a function
// 	* handler may take between 0 and two arguments.
// 	* if there are two arguments, the first argument must satisfy the "context.Context" interface.
// 	* handler may return between 0 and two arguments.
// 	* if there are two return values, the second argument must be an error.
// 	* if there is one return value it must be an error.
//
// Valid function signatures:
//
// 	func ()
// 	func () error
// 	func (TIn) error
// 	func () (TOut, error)
// 	func (TIn) (TOut, error)
// 	func (context.Context) error
// 	func (context.Context, TIn) error
// 	func (context.Context) (TOut, error)
// 	func (context.Context, TIn) (TOut, error)
//
// Where "TIn" and "TOut" are types compatible with the "encoding/json" standard library.
// See https://golang.org/pkg/encoding/json/#Unmarshal for how deserialization behaves
//
// TODO: return handler or interface{}?
func (a *Agent) Wrap(handlerFunc interface{}) awslambda.Handler {
	if handlerFunc == nil {
		return errorHandler(fmt.Errorf("handler is nil"))
	}

	handler := reflect.ValueOf(handlerFunc)
	handlerType := reflect.TypeOf(handlerFunc)
	if handlerType.Kind() != reflect.Func {
		return errorHandler(fmt.Errorf("handler kind %s is not %s", handlerType.Kind(), reflect.Func))
	}

	takesContext, err := validateArguments(handlerType)
	if err != nil {
		return errorHandler(err)
	}

	if err := validateReturns(handlerType); err != nil {
		return errorHandler(err)
	}

	return lambdaHandler(
		func(ctx context.Context, payload []byte) (interface{}, error) {
			var args []reflect.Value
			if takesContext {
				args = append(args, reflect.ValueOf(ctx))
			}

			if (handlerType.NumIn() == 1 && !takesContext) || handlerType.NumIn() == 2 {
				// Deserialize the last input argument and pass that on to the handler
				eventType := handlerType.In(handlerType.NumIn() - 1)
				event := reflect.New(eventType)

				if err := json.Unmarshal(payload, event.Interface()); err != nil {
					return nil, err
				}

				args = append(args, event.Elem())
			}

			response := handler.Call(args)

			var err error
			if len(response) > 0 {
				// Convert last return argument to error
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

			a.RunPostHooks(ctx, payload, &val, err)

			return val, err
		},
	)
}

// WrapOld wraps the handler so the agent can intercept
// and record events
// func (a *Agent) WrapOld(handler interface{}) interface{} {
// 	if handler == nil {
// 		return errorHandler(fmt.Errorf("handler is nil"))
// 	}

// 	handlerType := reflect.TypeOf(handler)
// 	handlerValue := reflect.ValueOf(handler)
// 	takesContext, _ := validateArguments(handlerType)

// 	// return func(ctx context.Context, payload json.RawMessage) (interface{}, error) {
// 	return func(ctx context.Context, request events.APIGatewayProxyRequest) (interface{}, error) {
// 		var args []reflect.Value
// 		var elem reflect.Value

// 		if (handlerType.NumIn() == 1 && !takesContext) || handlerType.NumIn() == 2 {
// 			newEventType := handlerType.In(handlerType.NumIn() - 1)
// 			newEvent := reflect.New(newEventType)

// 			payload, err := json.Marshal(request)
// 			if err != nil {
// 				log.Println("Error marshalling request", err)
// 				return nil, err
// 			}

// 			if err := json.Unmarshal(payload, newEvent.Interface()); err != nil {
// 				return nil, err
// 			}

// 			elem = newEvent.Elem()
// 			ctx = context.WithValue(ctx, eventTypeKey{}, newEventType)
// 		}

// 		if takesContext {
// 			args = append(args, reflect.ValueOf(ctx))
// 		}

// 		if elem.IsValid() {
// 			args = append(args, elem)
// 		}

// 		response := handlerValue.Call(args)

// 		var err error
// 		if len(response) > 0 {
// 			if errVal, ok := response[len(response)-1].Interface().(error); ok {
// 				err = errVal
// 			}
// 		}
// 		var val interface{}
// 		if len(response) > 1 {
// 			val = response[0].Interface()
// 		}

// 		if err != nil {
// 			val = nil
// 		}

// 		a.auditOrSample(request, val, err)

// 		return val, err
// 	}
// }

func (a *Agent) auditOrSample(
	request events.APIGatewayProxyRequest,
	val interface{},
	err error) {
	var route string
	handler, _, _ := a.target.getValue(request.Path, getParams)
	if handler != nil {
		// route is targeted
		route = handler()
		a.Publisher.Publish("target", route, request, val, err)
		log.Printf("route: %s is targeted", route)
		return
	}

	handler, _, _ = a.sampled.getValue(request.Path, getParams)
	if handler != nil {
		// route is already sampled
		log.Printf("route: %s is already sampled", handler())
		return
	}

	// sample the new route
	if request.Resource == "{proxy+}" {
		route = request.Path
	} else {
		r := strings.NewReplacer("{", ":", "}", "")
		route = r.Replace(request.Resource)
	}

	a.Publisher.Publish("sampled", route, request, val, err)
	a.sampled.addRoute(route, newHandler(route))
	log.Printf("route: %s is sampled", route)
}

// errorHandler returns a stand-in lambda function that
// handles error gracefully
func errorHandler(e error) lambdaHandler {
	return func(ctx context.Context, event []byte) (interface{}, error) {
		return nil, e
	}
}

// validateArguments validates the handler arguments comply
// to lambda handler signature:
// 	* handler may take between 0 and two arguments.
// 	* if there are two arguments, the first argument must satisfy the "context.Context" interface.
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
			return false, fmt.Errorf(
				"handler takes two arguments, but the first is not Context. got %s",
				argumentType.Kind(),
			)
		}
	}

	return handlerTakesContext, nil
}

// validateReturns ensures the return arguments are legal
// Return arguments rules:
// 	* handler may return between 0 and two arguments.
// 	* if there are two return values, the second argument must be an error.
// 	* if there is one return value it must be an error.
func validateReturns(handler reflect.Type) error {
	errorType := reflect.TypeOf((*error)(nil)).Elem()

	switch n := handler.NumOut(); {
	case n > 2:
		return fmt.Errorf("handler may not return more than two values")
	case n > 1:
		if !handler.Out(1).Implements(errorType) {
			return fmt.Errorf("handler returns two values, but the second does not implement error")
		}
	case n == 1:
		if !handler.Out(0).Implements(errorType) {
			return fmt.Errorf("handler returns a single value, but it does not implement error")
		}
	}

	return nil
}

// RunPostHooks executes all post hooks in the order they are registered
func (a *Agent) RunPostHooks(
	ctx context.Context,
	payload []byte,
	returnValue interface{},
	err error,
) {
	for _, hook := range a.postHooks {
		hook.AfterExecution(ctx, payload, returnValue, err)
	}
}
