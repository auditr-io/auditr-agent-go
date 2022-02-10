package lambda

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/auditr-io/auditr-agent-go/collect"
	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/auditr-io/auditr-agent-go/lambda/events"
	"github.com/stretchr/testify/assert"
)

func TestBuild(t *testing.T) {
	parentOrgID := "parent-org-id"
	externalOrgID := "ext-org-id"
	xOrgIDFieldName := "x-org-id"
	orgIDField := "request.header." + xOrgIDFieldName
	route := &config.Route{
		HTTPMethod: http.MethodGet,
		Path:       "/person/:id",
	}

	user := &collect.EventUser{
		ID:       "user-id",
		Email:    "email",
		FullName: "full-name",
		Name:     "username",
		Domain:   "domain",
	}

	client := &collect.EventClient{
		IP: "1.2.3.4",
	}

	requestedAt := time.Now().UnixNano() / int64(time.Millisecond)
	req := events.APIGatewayProxyRequest{
		Headers: map[string]string{
			(strings.Title(xOrgIDFieldName)): externalOrgID,
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			Authorizer: map[string]interface{}{
				"claims": map[string]interface{}{
					"sub":              user.ID,
					"token_use":        "id",
					"given_name":       user.FullName,
					"email":            user.Email,
					"cognito:username": user.Name,
					"iss":              user.Domain,
				},
			},
			Identity: events.APIGatewayRequestIdentity{
				SourceIP: client.IP,
			},
			RequestTimeEpoch: requestedAt,
		},
	}

	res := json.RawMessage(`{"bla":1}`)

	errorValue := json.RawMessage(`{"message":"bla"}`)

	a := &APIGatewayEventBuilder{}
	eventRaw, err := a.Build(
		parentOrgID,
		orgIDField,
		collect.RouteTypeTarget,
		route,
		req,
		res,
		errorValue,
	)
	assert.NoError(t, err)
	assert.NotNil(t, eventRaw)

	assert.Equal(t, externalOrgID, eventRaw.Organization.ID)

	assert.Equal(t, collect.RouteTypeTarget, eventRaw.Route.Type)
	assert.Equal(t, route.HTTPMethod, eventRaw.Route.Method)
	assert.Equal(t, route.Path, eventRaw.Route.Path)

	assert.Equal(t, user, eventRaw.User)

	assert.Equal(t, client, eventRaw.Client)

	assert.Equal(t, requestedAt, eventRaw.RequestedAt)

	assert.Equal(t, req, eventRaw.Request)
	assert.Equal(t, res, eventRaw.Response)
	assert.Equal(t, errorValue, eventRaw.Error)
}
