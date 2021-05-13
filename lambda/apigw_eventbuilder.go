package lambda

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/auditr-io/auditr-agent-go/collect"
	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/auditr-io/auditr-agent-go/lambda/events"
)

// APIGatewayEventBuilder builds an event from APIGateway request and response
type APIGatewayEventBuilder struct{}

// Build builds an event from APIGateway request and response
func (b *APIGatewayEventBuilder) Build(
	rootOrgID string,
	orgIDField string,
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

	orgID, err := b.mapOrgID(rootOrgID, orgIDField, &req)
	if err != nil {
		return nil, err
	}

	user, err := b.mapUser(&req)
	if err != nil {
		return nil, err
	}

	identity := req.RequestContext.Identity

	event := &collect.EventRaw{
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
	}

	if req.RequestContext.RequestTimeEpoch > 0 {
		event.RequestedAt = req.RequestContext.RequestTimeEpoch
	}

	return event, nil
}

// mapOrgID maps the configured orgIDField to org ID
func (b *APIGatewayEventBuilder) mapOrgID(
	rootOrgID string,
	orgIDField string,
	req *events.APIGatewayProxyRequest,
) (string, error) {
	// todo: extract orgID from request based on mapping
	// eg. from jwt, header, field in body
	// Default org ID to root org ID
	orgID := rootOrgID
	if orgIDField == "" {
		return orgID, nil
	}

	fieldParts := strings.Split(orgIDField, ".")
	if len(fieldParts) < 3 {
		return "", fmt.Errorf("invalid org ID field %s", orgIDField)
	}

	// the first field part is always "request"
	switch fieldParts[1] {
	case "header":
		val, ok := req.Headers[fieldParts[2]]
		if !ok {
			return "", fmt.Errorf("org ID field %s not found", orgIDField)
		}
		orgID = val
		if len(fieldParts) == 5 {
			if fieldParts[3] == "jwt" {
				// decode jwt and set org id
			}
		}
	case "querystring":
		val, ok := req.QueryStringParameters[fieldParts[2]]
		if !ok {
			return "", fmt.Errorf("org ID field %s not found", orgIDField)
		}
		orgID = val
		if len(fieldParts) == 5 {
			if fieldParts[3] == "jwt" {
				// decode jwt and set org id
			}
		}
	case "body":
		var body map[string]interface{}
		if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
			return "", fmt.Errorf("error unmarshalling request body")
		}
		val, ok := body[fieldParts[2]]
		if !ok {
			return "", fmt.Errorf("org ID field %s not found", orgIDField)
		}
		orgID, ok = val.(string)
		if !ok {
			return "", fmt.Errorf("org ID field %s can't be converted to a string", orgIDField)
		}
		if len(fieldParts) == 5 {
			if fieldParts[3] == "jwt" {
				// decode jwt and set org id
			}
		}
	}

	return orgID, nil
}

// mapUser maps user related fields to user
func (b *APIGatewayEventBuilder) mapUser(
	req *events.APIGatewayProxyRequest,
) (*collect.EventUser, error) {
	// Map identity
	// https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-logging-variables.html
	identity := req.RequestContext.Identity
	authorizer := req.RequestContext.Authorizer

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

	return user, nil
}
