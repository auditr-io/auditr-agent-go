package common

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/auditr-io/auditr-agent-go/collect"
	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/stretchr/testify/assert"
)

func TestBuild(t *testing.T) {
	parentOrgID := "parent-org-id"
	orgID := "org-id"
	userID := "user-id"
	orgIDField := "request.body.organization.id"
	clientIP := "1.2.3.4"

	reqBody := struct {
		Organization struct {
			ID string `json:"id"`
		} `json:"organization"`
	}{
		Organization: struct {
			ID string `json:"id"`
		}{
			ID: orgID,
		},
	}
	reqBodyBytes, _ := json.Marshal(reqBody)
	reqURL, _ := url.Parse("https://localhost/person/123")
	req := HTTPRequest{
		Method: http.MethodPost,
		URL:    reqURL,
		Headers: http.Header{
			"X-User-Id":       []string{userID},
			"X-Forwarded-For": []string{clientIP},
		},
		Body: string(reqBodyBytes),
	}

	res := json.RawMessage(`{
		"person": {
			"id": 123,
			"name": "homer"
		}
	}`)
	errorValue := json.RawMessage(`{"error": "nothin'"}`)

	wantEvt := &collect.EventRaw{
		Organization: &collect.EventOrganization{
			ID: "org-id",
		},

		Route: &collect.EventRoute{
			Type:   collect.RouteTypeSample,
			Method: http.MethodPost,
			Path:   "/person/:id",
		},

		User: &collect.EventUser{
			ID: "user-id",
		},

		Client: &collect.EventClient{
			IP: "1.2.3.4",
		},

		RequestedAt: time.Now().UnixNano() / int64(time.Millisecond),
		Request:     req,
		Response:    res,
		Error:       errorValue,
	}

	route := &config.Route{
		HTTPMethod: wantEvt.Route.Method,
		Path:       wantEvt.Route.Path,
	}

	h := &HTTPEventBuilder{}

	evt, err := h.Build(
		parentOrgID,
		orgIDField,
		collect.RouteTypeSample,
		route,
		req,
		res,
		errorValue,
	)
	assert.NoError(t, err)
	assert.Equal(t, wantEvt.Organization, evt.Organization)
	assert.Equal(t, wantEvt.Route, evt.Route)
	assert.Equal(t, wantEvt.User, evt.User)
	assert.Equal(t, wantEvt.Client, evt.Client)
	assert.Equal(t, wantEvt.RequestedAt, evt.RequestedAt)
	assert.Equal(t, wantEvt.Request, evt.Request)
	assert.Equal(t, wantEvt.Response, evt.Response)
	assert.Equal(t, wantEvt.Error, evt.Error)
}
