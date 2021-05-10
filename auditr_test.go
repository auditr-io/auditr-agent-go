package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"testing"

	"github.com/auditr-io/auditr-agent-go/collect"
	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/auditr-io/auditr-agent-go/lambda"
	"github.com/auditr-io/auditr-agent-go/lambda/events"
	"github.com/auditr-io/auditr-agent-go/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type Handler interface {
	Invoke(ctx context.Context, payload []byte) ([]byte, error)
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

	handler := func(r events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
		return expected, nil
	}

	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			reqBody, err := ioutil.ReadAll(req.Body)
			assert.NoError(t, err)

			var eventBatch []*collect.EventRaw
			err = json.Unmarshal(reqBody, &eventBatch)
			assert.NoError(t, err)
			event := eventBatch[0]
			assert.Equal(t, collect.RouteTypeTarget, event.Route.Type)

			r := ioutil.NopCloser(bytes.NewBuffer([]byte(`[
				{
					"status": 200
				}
			]`)))

			return &http.Response{
				StatusCode: 200,
				Body:       r,
			}, nil
		},
	}

	m.
		On("RoundTrip", mock.AnythingOfType("*http.Request")).
		Return(mock.AnythingOfType("*http.Response"), nil).Once()

	mockClient := func() *http.Client {
		return &http.Client{
			Transport: m,
		}
	}

	configurer, err := config.NewConfigurer(
		config.WithConfigProvider(func() ([]byte, error) {
			return []byte(`{
				"base_url": "https://dev-api.auditr.io/v1",
				"events_path": "/events",
				"target": [
					{
						"method": "PUT",
						"path": "/events/:id"
					}
				],
				"sample": [],
				"flush": false,
				"cache_duration": 2,
				"max_events_per_batch": 10,
				"max_concurrent_batches": 10,
				"pending_work_capacity": 20,
				"send_interval": 20,
				"block_on_send": false,
				"block_on_response": true
			}`), nil
		}),
		config.WithHTTPClient(mockClient),
	)

	configurer.Refresh(context.Background())

	agentInstance, err = lambda.NewAgentWithConfiguration(configurer.Configuration)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		res := <-agentInstance.Responses()

		expectedResponse := collect.Response{
			StatusCode: 200,
		}
		assert.Equal(t, expectedResponse, res)
	}()

	wrappedHandler := Audit(handler).(Handler)
	reqBytes, _ := json.Marshal(req)
	resBytes, _ := wrappedHandler.Invoke(context.Background(), reqBytes)

	var actual events.APIGatewayProxyResponse
	err = json.Unmarshal(resBytes, &actual)
	assert.NoError(t, err)
	assert.Equal(t, expected, actual)

	wg.Wait()

	m.AssertExpectations(t)
}
