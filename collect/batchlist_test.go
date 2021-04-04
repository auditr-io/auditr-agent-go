package collect

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/auditr-io/auditr-agent-go/test"
	"github.com/segmentio/ksuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestBatchListAdd(t *testing.T) {
	event := &Event{
		ID: ksuid.New().String(),
	}

	r := make(chan Response, DefaultPendingWorkCapacity*2)
	b := newBatchList(
		r,
		DefaultMaxEventsPerBatch,
		DefaultMaxConcurrentBatches,
	)
	b.Add(event)

	batchID := b.getBatchID(event.ID)
	assert.Contains(t, b.batches[batchID], event)
}

func TestReenqueue(t *testing.T) {
	events := make([]*Event, 3)
	for i := 0; i < len(events); i++ {
		events[i] = &Event{
			ID: ksuid.New().String(),
		}
	}

	r := make(chan Response, DefaultPendingWorkCapacity*2)
	b := newBatchList(
		r,
		DefaultMaxEventsPerBatch,
		DefaultMaxConcurrentBatches,
	)
	b.reenqueue(events)

	for _, event := range events {
		batchID := b.getOverflowBatchID(event.ID)
		assert.Contains(t, b.overflowBatches[batchID], event)
	}
}

type notifier struct {
	mock.Mock
}

func (n *notifier) Done() {
	n.Called()
}

func TestBatchListFire(t *testing.T) {
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
			}, {
				HTTPMethod: http.MethodGet,
				Path:       "/events/:id",
			},
		},
	}

	configResponse := func() (int, []byte) {
		statusCode := 200
		cfgJSON, _ := json.Marshal(cfg)

		return statusCode, cfgJSON
	}

	eventResponse := func() (int, []byte) {
		statusCode := 200

		return statusCode, []byte("")
	}

	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			var statusCode int
			var responseBody []byte
			switch req.URL.String() {
			case config.ConfigURL:
				statusCode, responseBody = configResponse()
			case config.EventsURL:
				statusCode, responseBody = eventResponse()
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
		// .Twice()

	config.Init(
		config.WithHTTPClient(func() *http.Client {
			return &http.Client{
				Transport: m,
			}
		}),
	)

	n := &notifier{}
	n.On("Done").Once()

	event := &Event{
		ID: ksuid.New().String(),
	}

	r := make(chan Response, DefaultPendingWorkCapacity*2)
	b := newBatchList(
		r,
		DefaultMaxEventsPerBatch,
		DefaultMaxConcurrentBatches,
	)
	b.Add(event)
	b.Fire(n)

	assert.True(t, m.AssertExpectations(t))
	assert.True(t, n.AssertExpectations(t))
}

func TestBatchListFire_ProcessesOverflow(t *testing.T) {
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
			}, {
				HTTPMethod: http.MethodGet,
				Path:       "/events/:id",
			},
		},
	}

	configResponse := func() (int, []byte) {
		statusCode := 200
		cfgJSON, _ := json.Marshal(cfg)

		return statusCode, cfgJSON
	}

	eventResponse := func() (int, []byte) {
		statusCode := 200

		return statusCode, []byte("")
	}

	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			var statusCode int
			var responseBody []byte
			switch req.URL.String() {
			case config.ConfigURL:
				statusCode, responseBody = configResponse()
			case config.EventsURL:
				statusCode, responseBody = eventResponse()
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
		// .Times(3)

	config.Init(
		config.WithHTTPClient(func() *http.Client {
			return &http.Client{
				Transport: m,
			}
		}),
	)

	n := &notifier{}
	n.On("Done").Once()

	eventID := ksuid.New().String()
	event := &Event{
		ID:      eventID,
		Request: "",
	}
	payloadExclReqContent, _ := json.Marshal(event)
	// This will cause the batch to overflow
	event.Request = randomString(maxEventBytes - len(payloadExclReqContent))

	r := make(chan Response, DefaultPendingWorkCapacity*2)
	b := newBatchList(
		r,
		DefaultMaxEventsPerBatch,
		DefaultMaxConcurrentBatches,
	)
	for i := 0; i < int(DefaultMaxEventsPerBatch); i++ {
		b.Add(event) // same event ID fills the same batch
	}

	b.Fire(n)

	assert.True(t, m.AssertExpectations(t))
	assert.True(t, n.AssertExpectations(t))
}

func TestSend(t *testing.T) {
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
			}, {
				HTTPMethod: http.MethodGet,
				Path:       "/events/:id",
			},
		},
	}

	configResponse := func() (int, []byte) {
		statusCode := 200
		cfgJSON, _ := json.Marshal(cfg)

		return statusCode, cfgJSON
	}

	eventResponse := func() (int, []byte) {
		statusCode := 200

		return statusCode, []byte("")
	}

	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			var statusCode int
			var responseBody []byte
			switch req.URL.String() {
			case config.ConfigURL:
				statusCode, responseBody = configResponse()
			case config.EventsURL:
				statusCode, responseBody = eventResponse()
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
		// .Twice()

	config.Init(
		config.WithHTTPClient(func() *http.Client {
			return &http.Client{
				Transport: m,
			}
		}),
	)

	events := make([]*Event, 3)
	for i := 0; i < len(events); i++ {
		events[i] = &Event{
			ID: ksuid.New().String(),
		}
	}

	r := make(chan Response, DefaultPendingWorkCapacity*2)
	b := newBatchList(
		r,
		DefaultMaxEventsPerBatch,
		DefaultMaxConcurrentBatches,
	)
	b.send(events)

	assert.True(t, m.AssertExpectations(t))
}

func TestSend_GetResponseOnError(t *testing.T) {
	expectedErr := fmt.Errorf("random error")

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
			}, {
				HTTPMethod: http.MethodGet,
				Path:       "/events/:id",
			},
		},
	}

	configResponse := func() (int, []byte) {
		statusCode := 200
		cfgJSON, _ := json.Marshal(cfg)

		return statusCode, cfgJSON
	}

	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			var statusCode int
			var responseBody []byte
			switch req.URL.String() {
			case config.ConfigURL:
				statusCode, responseBody = configResponse()
			case config.EventsURL:
				return nil, expectedErr
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
		// .Twice()

	config.Init(
		config.WithHTTPClient(func() *http.Client {
			return &http.Client{
				Transport: m,
			}
		}),
	)

	events := make([]*Event, 3)
	for i := 0; i < len(events); i++ {
		events[i] = &Event{
			ID: ksuid.New().String(),
		}
	}

	expectedErrRes := Response{
		Err: &url.Error{
			Op:  "Post",
			URL: config.EventsURL,
			Err: expectedErr,
		},
	}

	r := make(chan Response, DefaultPendingWorkCapacity*2)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		res := <-r
		assert.Equal(t, expectedErrRes, res)
	}()

	b := newBatchList(
		r,
		DefaultMaxEventsPerBatch,
		DefaultMaxConcurrentBatches,
	)
	b.send(events)

	assert.True(t, m.AssertExpectations(t))

	wg.Wait()
}

func TestSend_GetResponseOnNotOK(t *testing.T) {
	expectedEventStatusCode := 400
	expectedEventBody := []byte(`[
		{
			"id": "x",
			"error": "event is missing y"
		}
	]`)

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
			}, {
				HTTPMethod: http.MethodGet,
				Path:       "/events/:id",
			},
		},
	}

	configResponse := func() (int, []byte) {
		statusCode := 200
		cfgJSON, _ := json.Marshal(cfg)

		return statusCode, cfgJSON
	}

	eventResponse := func() (int, []byte) {
		return expectedEventStatusCode, expectedEventBody
	}

	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			var statusCode int
			var responseBody []byte
			switch req.URL.String() {
			case config.ConfigURL:
				statusCode, responseBody = configResponse()
			case config.EventsURL:
				statusCode, responseBody = eventResponse()
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
		// .Twice()

	config.Init(
		config.WithHTTPClient(func() *http.Client {
			return &http.Client{
				Transport: m,
			}
		}),
	)

	events := make([]*Event, 3)
	for i := 0; i < len(events); i++ {
		events[i] = &Event{
			ID: ksuid.New().String(),
		}
	}

	expectedErr := Response{
		Err: fmt.Errorf(
			"Error sending %s %s: status %d",
			http.MethodPost,
			config.EventsURL,
			expectedEventStatusCode,
		),
		StatusCode: expectedEventStatusCode,
		Body:       expectedEventBody,
	}

	r := make(chan Response, DefaultPendingWorkCapacity*2)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		res := <-r
		assert.Equal(t, expectedErr, res)
	}()

	b := newBatchList(
		r,
		DefaultMaxEventsPerBatch,
		DefaultMaxConcurrentBatches,
	)
	b.send(events)

	assert.True(t, m.AssertExpectations(t))

	wg.Wait()
}

func TestEncodeJSON(t *testing.T) {
	events := make([]*Event, 3)
	for i := 0; i < len(events); i++ {
		events[i] = &Event{
			ID: ksuid.New().String(),
		}
	}

	r := make(chan Response, DefaultPendingWorkCapacity*2)
	b := newBatchList(
		r,
		DefaultMaxEventsPerBatch,
		DefaultMaxConcurrentBatches,
	)
	eventsJSON, numEncoded := b.encodeJSON(events)
	assert.Equal(t, len(events), numEncoded)

	expectedJSON, _ := json.Marshal(events)
	assert.Equal(t, expectedJSON, eventsJSON)
}

func TestEncodeJSON_FailsOnInvalidEvent(t *testing.T) {
	type unmarshallable struct {
		Fn func()
	}

	events := []*Event{
		{
			ID: ksuid.New().String(),
			Request: unmarshallable{
				Fn: func() {},
			},
		},
	}
	_, expectedErr := json.Marshal(events[0])

	r := make(chan Response, DefaultPendingWorkCapacity*2)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		res := <-r
		assert.Equal(t, expectedErr, res.Err)
	}()

	b := newBatchList(
		r,
		DefaultMaxEventsPerBatch,
		DefaultMaxConcurrentBatches,
	)
	b.encodeJSON(events)

	wg.Wait()
}

func TestEncodeJSON_FailsOnOversizedEvent(t *testing.T) {
	eventID := ksuid.New().String()
	event := &Event{
		ID:      eventID,
		Request: "",
	}
	payloadExclReqContent, _ := json.Marshal(event)
	event.Request = randomString(maxEventBytes - len(payloadExclReqContent) + 1)

	events := []*Event{
		event,
	}
	expectedErr := fmt.Errorf("Event exceeds max size of %d bytes", maxEventBytes)

	r := make(chan Response, DefaultPendingWorkCapacity*2)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		res := <-r
		assert.Equal(t, expectedErr, res.Err)
	}()

	b := newBatchList(
		r,
		DefaultMaxEventsPerBatch,
		DefaultMaxConcurrentBatches,
	)
	b.encodeJSON(events)

	wg.Wait()
}

func TestEncodeJSON_ReenqueuesOnOversizedBatch(t *testing.T) {
	eventID := ksuid.New().String()
	event := &Event{
		ID:      eventID,
		Request: "",
	}
	payloadExclReqContent, _ := json.Marshal(event)
	event.Request = randomString(maxEventBytes - len(payloadExclReqContent))

	events := make([]*Event, DefaultMaxEventsPerBatch)
	for i := range events {
		events[i] = event // same event ID fills the same batch
	}

	r := make(chan Response, DefaultPendingWorkCapacity*2)

	b := newBatchList(
		r,
		DefaultMaxEventsPerBatch,
		DefaultMaxConcurrentBatches,
	)
	b.encodeJSON(events)

	batchID := b.getOverflowBatchID(eventID)
	assert.Equal(t, 1, len(b.overflowBatches[batchID]))
}

func randomString(length int) string {
	b := make([]byte, length/2)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
