package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"reflect"
	"time"

	"github.com/aws/aws-lambda-go/events"
)

// Agent is an auditr agent that collects and reports events
type Agent struct {
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
	return &Agent{}
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

			// payload := []byte(`{}`)
			// if request.HTTPMethod == "POST" {
			// 	payload = []byte(request.Body)
			// }

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
		sendEvent(request, elem.Interface(), val, err)

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

func sendEvent(request events.APIGatewayProxyRequest, originalEvent interface{}, val interface{}, err error) {
	// req, err := json.Unmarshal(args)
	// if err != nil {
	// 	log.Println("Error in unmarshalling ", err)
	// 	return
	// }
	// req, err := json.Unmarshal(args)
	// if err != nil {
	// 	log.Println("Error in unmarshalling ", err)
	// 	return
	// }
	// event := Event{
	// 	ID:      "",
	// 	Actor:   "user@auditr.io",
	// 	ActorID: "6b45a096-0e41-42c0-ab71-e6ec29e23fee",
	// }

	// err := json.Unmarshal([]byte(request.Body), &event)
	// if err != nil {
	// 	fmt.Println("json.Unmarshal err:", err)
	// 	return events.APIGatewayProxyResponse{
	// 		StatusCode: 500,
	// 		Body:       err.Error(),
	// 	}, nil
	// }

	event := Event{
		ID:       "1jnAFJlXHOm25Czq0CeZ8OrHA2l",
		Actor:    "user@auditr.io",
		ActorID:  "6b45a096-0e41-42c0-ab71-e6ec29e23fee",
		Request:  request,
		Response: val,
		Error:    err,
	}

	event.Action = request.HTTPMethod
	event.Resource = request.Resource
	event.Location = request.RequestContext.Identity.SourceIP
	event.RequestID = request.RequestContext.RequestID
	event.RequestedAt = time.Now().Unix()
	if request.RequestContext.RequestTimeEpoch > 0 {
		event.RequestedAt = request.RequestContext.RequestTimeEpoch
	}

	// event.Action = event.Request["httpMethod"].(string)
	// reqContext := event.Request["requestContext"].(map[string]interface{})
	// identity := reqContext["identity"].(map[string]interface{})
	// event.Location = identity["sourceIp"].(string)
	// event.RequestID = reqContext["requestId"].(string)
	// event.RequestedAt = reqContext["requestTimeEpoch"].(float64)

	// event.Response = val
	e, err := json.Marshal(event)
	if err != nil {
		log.Println("Error in marshalling ", err)
		return
	}

	et := Event{}
	json.Unmarshal(e, &et)
	log.Println(et)

	sendEventBytes(e)
}

func sendEventBytes(event []byte) {
	req, err := http.NewRequest("POST", "https://c8otdzrb0c.execute-api.us-west-2.amazonaws.com/dev/events", bytes.NewBuffer(event))
	if err != nil {
		log.Println("Error http.NewRequest:", err)
		return
	}

	req.Close = true
	req.Header.Set("Authorization", "Bearer blablabla")
	req.Header.Set("Content-Type", "application/json")

	client := createHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error client.Do(req):", err)
		return
	}

	if resp.Body == nil {
		return
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("ioutil.ReadAll(resp.Body):", err)
		return
	}

	log.Println("response Body:", string(body))

	resp.Body.Close()
}

func createHTTPClient() *http.Client {
	transport := &http.Transport{}

	return &http.Client{
		Transport: transport,
	}
}
