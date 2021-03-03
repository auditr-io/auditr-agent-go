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
		Route:       route,
		Request:     req,
		Response:    response,
		Error:       errorValue,
	}

	var actor *collect.Actor
	// Default to cognito identity
	// https://docs.aws.amazon.com/cognito/latest/developerguide/amazon-cognito-user-pools-using-tokens-with-identity-providers.html
	if identity.CognitoIdentityID != "" {
		// go to openid config url
		// get userinfo endpoint
		// get userinfo w token
		// populate fields
		actor = &collect.Actor{
			ID:       identity.CognitoIdentityID,
			Name:     authorizer["name"].(string),
			Username: authorizer["cognito:username"].(string),
			Email:    authorizer["email"].(string),
		}
	} else {
		// Try custom authorizer principal next
		principalID, ok := authorizer["principalId"]
		if ok {
			actor = &collect.Actor{
				ID:       principalID.(string),
				Username: principalID.(string),
			}
		} else {
			// Finally, try IAM user
			actor = &collect.Actor{
				ID:       identity.UserArn,
				Username: identity.User,
			}
		}
	}
	event.Actor = actor

	if req.RequestContext.RequestTimeEpoch > 0 {
		event.RequestedAt = req.RequestContext.RequestTimeEpoch
	}

	return event, nil
}
