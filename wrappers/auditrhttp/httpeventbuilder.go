package auditrhttp

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/auditr-io/auditr-agent-go/collect"
	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/tidwall/gjson"
)

// HTTPEventBuilder maps custom HTTP requests to events
// todo: move to central builders package
type HTTPEventBuilder struct{}

// Build builds an event from HTTP request and response
func (b *HTTPEventBuilder) Build(
	parentOrgID string,
	orgIDField string,
	routeType collect.RouteType,
	route *config.Route,
	request interface{},
	response json.RawMessage,
	errorValue json.RawMessage,
) (*collect.EventRaw, error) {
	req, ok := request.(HTTPRequest)
	if !ok {
		return nil, fmt.Errorf("request is not of type HTTPRequest")
	}

	orgID, err := b.mapOrgID(parentOrgID, orgIDField, req)
	if err != nil {
		// failed to map to an org ID
		// safer to raise error and lose the event than to
		// store the event under the wrong org ID
		return nil, err
	}

	user, err := b.mapUser(req)
	if err != nil {
		return nil, err
	}

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
			IP: req.Headers.Get("X-Forwarded-For"),
		},

		RequestedAt: time.Now().UnixNano() / int64(time.Millisecond),

		Request:  req,
		Response: response,
		Error:    errorValue,
	}

	return event, nil
}

// mapOrgID maps the configured orgIDField to org ID
// todo: extract this to a mapper.OrgID?
func (b *HTTPEventBuilder) mapOrgID(
	parentOrgID string,
	orgIDField string,
	req HTTPRequest,
) (string, error) {
	if orgIDField == "" {
		// orgIDField not configured, default org ID to root org ID
		return parentOrgID, nil
	}

	return getMappedValue(req, orgIDField)
}

// mapUser maps user related fields to user
func (b *HTTPEventBuilder) mapUser(
	req HTTPRequest,
) (*collect.EventUser, error) {
	// todo: config user fields
	// gradual increase in value w more config
	// min map user id, email, or username

	type EventUserMapping struct {
		ID       string `json:"id,omitempty"`
		Email    string `json:"email,omitempty"`
		FullName string `json:"full_name,omitempty"`
		Name     string `json:"name,omitempty"`
		Domain   string `json:"domain,omitempty"`
	}

	mapping := EventUserMapping{
		ID:    "request.header.x-user-id",
		Email: "request.body.email",
		Name:  "request.querystring.username",
	}

	user := &collect.EventUser{}

	if userID, err := getMappedValue(req, mapping.ID); err == nil {
		user.ID = userID
	}

	if email, err := getMappedValue(req, mapping.Email); err == nil {
		user.Email = email
	}

	if username, err := getMappedValue(req, mapping.Name); err == nil {
		user.Name = username
	}

	if fullName, err := getMappedValue(req, mapping.FullName); err == nil {
		user.FullName = fullName
	}

	if domain, err := getMappedValue(req, mapping.Domain); err == nil {
		user.Domain = domain
	}

	return user, nil
}

// getMappedValue extracts the field value from a HTTPRequest
func getMappedValue(
	req HTTPRequest,
	fieldName string,
) (string, error) {
	if fieldName == "" {
		return "", fmt.Errorf("invalid field %s", fieldName)
	}

	fieldParts := strings.SplitN(fieldName, ".", 3)
	if len(fieldParts) < 3 {
		return "", fmt.Errorf("invalid field %s", fieldName)
	}

	// the first field part is always "request"
	switch fieldParts[1] {
	case "header":
		val := req.Headers.Get(fieldParts[2])
		if val == "" {
			return "", fmt.Errorf("field %s not found", fieldName)
		}

		if len(val) == 0 {
			// fieldName is present but empty
			return "", fmt.Errorf("field %s is empty", fieldName)
		}

		lastParts := strings.Split(fieldParts[2], ".")
		if len(lastParts) == 1 {
			return val, nil
		}

		if len(lastParts) == 3 && lastParts[2] == "jwt" {
			// todo: decode jwt and set org id
		}
	case "querystring":
		q := req.URL.Query()
		val, ok := q[fieldParts[2]]
		if !ok {
			return "", fmt.Errorf("field %s not found", fieldName)
		}

		if len(val) == 0 {
			// fieldName is present but empty
			return "", fmt.Errorf("field %s is empty", fieldName)
		}

		lastParts := strings.Split(fieldParts[2], ".")
		if len(lastParts) == 1 {
			return val[0], nil
		}

		if len(lastParts) == 3 && lastParts[2] == "jwt" {
			// todo: decode jwt and set org id
		}
	case "body":
		result := gjson.Get(req.Body, fieldParts[2])
		if !result.Exists() {
			return "", fmt.Errorf("field %s not found", fieldName)
		}

		return result.String(), nil
	}

	return "", fmt.Errorf("invalid field %s", fieldName)
}
