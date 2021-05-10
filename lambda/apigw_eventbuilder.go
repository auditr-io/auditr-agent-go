package lambda

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/auditr-io/auditr-agent-go/collect"
	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/auditr-io/auditr-agent-go/lambda/events"
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
) (*collect.EventRaw, error) {
	req, ok := request.(events.APIGatewayProxyRequest)
	if !ok {
		return nil, fmt.Errorf("request is not of type APIGatewayProxyRequest")
	}

	// Map identity
	// https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-logging-variables.html
	identity := req.RequestContext.Identity
	authorizer := req.RequestContext.Authorizer

	// todo: extract orgID from request based on mapping
	// eg. from jwt, header, field in body
	orgID := ""

	user := &collect.EventUser{}
	if claims, ok := authorizer["claims"]; ok {
		// Default to cognito identity
		// https://docs.aws.amazon.com/cognito/latest/developerguide/amazon-cognito-user-pools-using-tokens-with-identity-providers.html
		//
		// Also handles custom JWT authorizers for HTTP APIs
		// https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-jwt-authorizer.html
		// go to openid config url
		// get userinfo endpoint
		// get userinfo w token
		// populate fields
		claims := claims.(map[string]interface{})

		if subject, ok := claims["sub"]; ok {
			user.ID = subject.(string)
		}

		if tokenUse, ok := claims["token_use"]; ok {
			switch tokenUse {
			case "id":
				// ID token
				if name, ok := claims["given_name"]; ok {
					user.FullName = name.(string)
				}

				if email, ok := claims["email"]; ok {
					user.Email = email.(string)
				}

				if username, ok := claims["cognito:username"]; ok {
					user.Name = username.(string)
				}

				if issuer, ok := claims["iss"]; ok {
					// User pool
					user.Domain = issuer.(string)
				}
			case "access":
				// Access token
				if name, ok := claims["given_name"]; ok {
					user.FullName = name.(string)
				}

				if email, ok := claims["email"]; ok {
					user.Email = email.(string)
				}

				if username, ok := claims["cognito:username"]; ok {
					user.Name = username.(string)
				}

				if issuer, ok := claims["iss"]; ok {
					// User pool
					user.Domain = issuer.(string)
				}
			}
		}
	} else if principalID, ok := authorizer["principalId"]; ok {
		// Custom authorizer principal
		user.ID = principalID.(string)
		user.Name = principalID.(string)
	} else if identity.UserArn != "" {
		// Finally, try IAM user
		user.ID = identity.UserArn
		user.Name = identity.User
	}

	event := &collect.EventRaw{
		// todo: assign new org id if not found in config
		// why do we even need this here? after mapping the org id field, just map it at write
		// but if we don't do it here, and we can't find the org id anywhere, this record is unusable?
		Organization: &collect.EventOrganization{
			ID: orgID,
		},

		Route: &collect.EventRoute{
			Type:   routeType,
			Method: route.HTTPMethod,
			Path:   route.Path,
		},

		User: user,

		Client: &collect.EventClient{
			Address: identity.SourceIP,
		},

		RequestedAt: time.Now().UnixNano() / int64(time.Millisecond),

		Request:  req,
		Response: response,
		Error:    errorValue,

		// OLD fields
		// RouteType:   routeType,
		// HTTPMethod:  route.HTTPMethod,
		// RoutePath:   route.Path,

		// Action:      req.HTTPMethod,
		// Location:    identity.SourceIP,
		// RequestID:   req.RequestContext.RequestID,
	}

	if req.RequestContext.RequestTimeEpoch > 0 {
		event.RequestedAt = req.RequestContext.RequestTimeEpoch
	}

	return event, nil
}
