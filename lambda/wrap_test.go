package lambda

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"testing"

	"github.com/auditr-io/auditr-agent-go/config"
	awslambda "github.com/aws/aws-lambda-go/lambda"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type Person struct {
	Name string
	Age  int
}

type recordHook struct {
	mock.Mock
}

func (h *recordHook) AfterExecution(
	ctx context.Context,
	payload []byte,
	returnValue interface{},
	err error,
) {
	returnInterface := returnValue.(*interface{})
	p := (*returnInterface).(*Person)
	h.Called(ctx, payload, p, err)
}

func createAgent() *Agent {
	configResponse := func() (int, []byte) {
		cfg := struct {
			BaseURL       string   `json:"base_url"`
			EventsPath    string   `json:"events_path"`
			TargetRoutes  []string `json:"target"`
			SampledRoutes []string `json:"sampled"`
		}{
			BaseURL:       "https://dev-api.auditr.io/v1",
			EventsPath:    "/events",
			TargetRoutes:  []string{"POST /events", "PUT /events/:id"},
			SampledRoutes: []string{"GET /events", "GET /events/:id"},
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

	a, err := New(
		WithHTTPClient(mockClient),
	)

	if err != nil {
		log.Fatal("Error creating agent")
	}

	return a
}

func TestWrap_ReturnsInvokableHandler(t *testing.T) {
	handler := func(ctx context.Context, e *Person) (*Person, error) {
		return e, nil
	}

	a := createAgent()
	assert.NotNil(t, a)

	wrappedHandler := a.Wrap(handler)
	assert.IsType(t, *new(lambdaHandler), wrappedHandler)
	assert.Implements(t, new(awslambda.Handler), wrappedHandler)

	pIn := &Person{
		Name: "x",
		Age:  10,
	}
	payload, err := json.Marshal(pIn)
	assert.NoError(t, err)

	awslambdaHandler := wrappedHandler.(awslambda.Handler)
	resBytes, err := awslambdaHandler.Invoke(
		context.Background(),
		payload,
	)
	assert.NoError(t, err)

	var pOut *Person
	err = json.Unmarshal(resBytes, &pOut)
	assert.Equal(t, pIn, pOut)
}

func TestWrap_RunsPostHook(t *testing.T) {
	handler := func(ctx context.Context, p *Person) (*Person, error) {
		return p, nil
	}

	a := createAgent()
	assert.NotNil(t, a)

	record := &recordHook{}
	a.RegisterPostHook(record)

	wrappedHandler := a.Wrap(handler)
	assert.IsType(t, *new(lambdaHandler), wrappedHandler)
	assert.Implements(t, new(awslambda.Handler), wrappedHandler)

	pIn := &Person{
		Name: "x",
		Age:  10,
	}
	payload, err := json.Marshal(pIn)
	assert.NoError(t, err)

	ctx := context.Background()
	record.
		On("AfterExecution", ctx, payload, pIn, nil).
		Once()

	awslambdaHandler := wrappedHandler.(awslambda.Handler)
	resBytes, err := awslambdaHandler.Invoke(
		ctx,
		payload,
	)
	assert.NoError(t, err)

	var pOut *Person
	err = json.Unmarshal(resBytes, &pOut)
	assert.Equal(t, pIn, pOut)
}
