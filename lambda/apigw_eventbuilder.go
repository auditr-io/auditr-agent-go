package lambda

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/auditr-io/auditr-agent-go/collect"
	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/auditr-io/auditr-agent-go/lambda/events"
	"github.com/segmentio/ksuid"
)

// APIGatewayEventBuilder builds an event from APIGateway request and response
type APIGatewayEventBuilder struct{}

// Build builds an event from APIGateway request and response
func (b *APIGatewayEventBuilder) Build(
	routeType collect.RouteType,
	route *config.Route,
	request interface{},
	response json.RawMessage,
	errorValue json.RawMessage,
) (*collect.Event, error) {
	req, ok := request.(events.APIGatewayProxyRequest)
	if !ok {
		return nil, fmt.Errorf("request is not of type APIGatewayProxyRequest")
	}

	// Map identity
	// https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-logging-variables.html
	identity := req.RequestContext.Identity
	authorizer := req.RequestContext.Authorizer

	event := &collect.Event{
		ID:          fmt.Sprintf("evt_%s", ksuid.New().String()),
		Action:      req.HTTPMethod,
		Location:    identity.SourceIP,
		RequestID:   req.RequestContext.RequestID,
		RequestedAt: time.Now().UTC().Unix(),
		RouteType:   routeType,
		HTTPMethod:  route.HTTPMethod,
		RoutePath:   route.Path,
		Request:     req,
		Response:    response,
		Error:       errorValue,
	}

	// Default to cognito identity
	// https://docs.aws.amazon.com/cognito/latest/developerguide/amazon-cognito-user-pools-using-tokens-with-identity-providers.html
	if identity.CognitoIdentityID != "" {
		// go to openid config url
		// get userinfo endpoint
		// get userinfo w token
		// populate fields
		event.ActorID = identity.CognitoIdentityID
		event.ActorName = authorizer["name"].(string)
		event.ActorUsername = authorizer["cognito:username"].(string)
		event.ActorEmail = authorizer["email"].(string)
	} else {
		// Try custom authorizer principal next
		principalID, ok := authorizer["principalId"]
		if ok {
			event.ActorID = principalID.(string)
			event.ActorUsername = principalID.(string)
		} else {
			// Finally, try IAM user
			event.ActorID = identity.UserArn
			event.ActorUsername = identity.User
		}
	}

	if req.RequestContext.RequestTimeEpoch > 0 {
		event.RequestedAt = req.RequestContext.RequestTimeEpoch
	}

	return event, nil
}
