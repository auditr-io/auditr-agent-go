package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/auditr-io/auditr-agent-go/lambda"
	"github.com/auditr-io/auditr-agent-go/lambda/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type Handler interface {
	Invoke(ctx context.Context, payload []byte) ([]byte, error)
}

// mockTransport is a mock Transport client
type mockTransport struct {
	mock.Mock
	http.Transport
	fn func(m *mockTransport, req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if m.fn != nil {
		return m.fn(m, req)
	}

	return m.successRoundTripResponse()
}

func (m *mockTransport) successRoundTripResponse() (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
	}, nil
}

func TestAudit(t *testing.T) {
	id := "xyz"
	req := events.APIGatewayProxyRequest{
		HTTPMethod:     http.MethodPut,
		Resource:       "/events/{id}",
		Path:           fmt.Sprintf("/events/%s", id),
		PathParameters: map[string]string{"id": id},
	}

	body := fmt.Sprintf(`{"id": %s}`, id)
	expected := events.APIGatewayProxyResponse{
		StatusCode: 200,
		Body:       body,
	}

	type fnType = func(context.Context, events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error)

	handler := func(r events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
		return expected, nil
	}

	configResponse := func() (int, []byte) {
		cfg := struct {
			BaseURL      string         `json:"base_url"`
			EventsPath   string         `json:"events_path"`
			TargetRoutes []config.Route `json:"target"`
			SampleRoutes []config.Route `json:"sample"`
		}{
			BaseURL:    "https://dev-api.auditr.io/v1",
			EventsPath: "/events",
			TargetRoutes: []config.Route{
				{
					HTTPMethod: http.MethodPost,
					Path:       "/events",
				}, {
					HTTPMethod: http.MethodPut,
					Path:       "/events/:id",
				},
			},
			SampleRoutes: []config.Route{
				{
					HTTPMethod: http.MethodGet,
					Path:       "/events",
				},
			},
		}
		responseBody, _ := json.Marshal(cfg)
		statusCode := 200

		return statusCode, responseBody
	}

	m := &mockTransport{
		fn: func(m *mockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			var statusCode int
			var responseBody []byte
			switch req.URL.String() {
			case config.ConfigURL:
				statusCode, responseBody = configResponse()
			}

			r := ioutil.NopCloser(bytes.NewBuffer(responseBody))

			return &http.Response{
				StatusCode: statusCode,
				Body:       r,
			}, nil
		},
	}

	m.
		On("RoundTrip", mock.AnythingOfType("*http.Request")).
		Return(mock.AnythingOfType("*http.Response"), nil)

	mockClient := func(ctx context.Context) *http.Client {
		return &http.Client{
			Transport: m,
		}
	}

	agentInstance, _ = lambda.NewAgent(
		config.WithHTTPClient(mockClient),
	)
	wrappedHandler := Audit(handler).(Handler)
	reqBytes, _ := json.Marshal(req)
	resBytes, _ := wrappedHandler.Invoke(context.Background(), reqBytes)

	var actual events.APIGatewayProxyResponse
	err := json.Unmarshal(resBytes, &actual)
	assert.NoError(t, err)
	assert.Equal(t, expected, actual)
}
