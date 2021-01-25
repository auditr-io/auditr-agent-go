package lambda

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/aws/aws-lambda-go/events"
	"github.com/facebookgo/muster"
	"github.com/segmentio/ksuid"
)

// Actor is the user originating the action
type Actor struct {
	ID       string `json:"actor_id"`
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
	Email    string `json:"email,omitempty"`
}

// Publisher publishes events to a receiving endpoint
type Publisher interface {
	// Publish creates an audit event and sends it to a listener
	Publish(
		routeType RouteType,
		route *config.Route,
		request events.APIGatewayProxyRequest, // TODO: adapter for other requests
		response events.APIGatewayProxyResponse,
		errorValue interface{},
	)
}

const (
	// version of this publisher
	version string = "0.0.1"

	// max number of events in a batch
	maxBatchSize uint = 50

	// duration after which to send a pending batch
	batchTimeout time.Duration = 100 * time.Millisecond

	// how many parallel batches before we start blocking
	maxConcurrentBatches uint = 10

	// pending items to hold in the work channel before blocking
	pendingWorkCapacity uint = 10
)

// publisher publishes audit events to auditr.
// This batch handling implementation is shamelessly borrowed from
// Honeycomb's libhoney.
type publisher struct {
	batchMaker func() muster.Batch
	muster     *muster.Client
	musterLock sync.RWMutex
	responses  chan Response
}

// newPublisher creates a new publisher
func newPublisher() (*publisher, error) {
	p := &publisher{}
	p.responses = make(chan Response, pendingWorkCapacity*2)
	p.batchMaker = func() muster.Batch {
		return newBatchList(p.responses)
	}
	p.muster = p.createMuster()
	err := p.muster.Start()
	if err != nil {
		return nil, err
	}

	return p, nil
}

// createMuster creates the muster client that coordinates the batch processing
func (p *publisher) createMuster() *muster.Client {
	m := new(muster.Client)
	m.MaxBatchSize = maxBatchSize
	m.BatchTimeout = batchTimeout
	m.MaxConcurrentBatches = maxConcurrentBatches
	m.PendingWorkCapacity = pendingWorkCapacity
	m.BatchMaker = p.batchMaker
	return m
}

// Publish creates an audit event and sends it to auditr
func (p *publisher) Publish(
	routeType RouteType,
	route *config.Route,
	request events.APIGatewayProxyRequest,
	response events.APIGatewayProxyResponse,
	errorValue interface{},
) {
	// Map identity
	// https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-logging-variables.html
	identity := request.RequestContext.Identity
	authorizer := request.RequestContext.Authorizer

	event := &Event{
		ID:          fmt.Sprintf("evt_%s", ksuid.New().String()),
		Action:      request.HTTPMethod,
		Location:    identity.SourceIP,
		RequestID:   request.RequestContext.RequestID,
		RequestedAt: time.Now().UTC().Unix(),
		RouteType:   routeType,
		Route:       route,
		Request:     request,
		Response:    response,
		Error:       errorValue,
	}

	var actor *Actor
	// Default to cognito identity
	// https://docs.aws.amazon.com/cognito/latest/developerguide/amazon-cognito-user-pools-using-tokens-with-identity-providers.html
	if identity.CognitoIdentityID != "" {
		// go to openid config url
		// get userinfo endpoint
		// get userinfo w token
		// populate fields
		actor = &Actor{
			ID:       identity.CognitoIdentityID,
			Name:     authorizer["name"].(string),
			Username: authorizer["cognito:username"].(string),
			Email:    authorizer["email"].(string),
		}
	} else {
		// Try custom authorizer principal next
		principalID, ok := authorizer["principalId"]
		if ok {
			actor = &Actor{
				ID:       principalID.(string),
				Username: principalID.(string),
			}
		} else {
			// Finally, try IAM user
			actor = &Actor{
				ID:       identity.UserArn,
				Username: identity.User,
			}
		}
	}
	event.Actor = actor

	if request.RequestContext.RequestTimeEpoch > 0 {
		event.RequestedAt = request.RequestContext.RequestTimeEpoch
	}

	// TODO: hashSecrets

	e, err := json.Marshal(event)
	if err != nil {
		log.Printf("Error in marshalling %#v", err)
		return
	}

	err = p.send(e)
	if err != nil {
		log.Printf("Error sending event %#v", err)
		// TODO: retry
		return
	}
}

func (p *publisher) send(event []byte) error {
	method := http.MethodPost
	req, err := http.NewRequest(method, config.EventsURL, bytes.NewBuffer(event))
	if err != nil {
		log.Printf("Error creating request for %s %s: %#v", method, config.EventsURL, err)
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := config.GetClient(context.Background()).Do(req)
	if err != nil {
		log.Printf("Error sending %s %s: %#v", method, config.EventsURL, err)
		return err
	}

	if res.StatusCode != http.StatusOK {
		log.Printf("Error sending %s %s: %#v", method, config.EventsURL, err)
		return err
	}

	return nil
}
