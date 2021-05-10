package collect

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"reflect"
	"sync"
	"testing"

	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/auditr-io/auditr-agent-go/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestBatchListAdd(t *testing.T) {
	event := &EventRaw{}

	configurer, _ := config.NewConfigurer(
		config.WithConfigProvider(func() ([]byte, error) {
			return []byte(`{
				"base_url": "https://dev-api.auditr.io/v1",
				"events_path": "/events",
				"target": [
					{
						"method": "GET",
						"path": "/person/:id"
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
	)

	configurer.Refresh(context.Background())

	r := make(chan Response, DefaultPendingWorkCapacity*2)
	b := newBatchList(
		configurer.Configuration,
		r,
		DefaultMaxEventsPerBatch,
		DefaultMaxConcurrentBatches,
	)
	b.Add(event)

	// batchID := b.getBatchID(event.ID)
	eventFound := false
	for _, batch := range b.batches {
		for _, e := range batch {
			if reflect.DeepEqual(e, event) {
				eventFound = true
				break
			}
		}
	}

	assert.True(t, eventFound)
}

func TestReenqueue(t *testing.T) {
	events := make([]*EventRaw, 3)
	for i := 0; i < len(events); i++ {
		events[i] = &EventRaw{}
	}

	configurer, _ := config.NewConfigurer(
		config.WithConfigProvider(func() ([]byte, error) {
			return []byte(`{
				"base_url": "https://dev-api.auditr.io/v1",
				"events_path": "/events",
				"target": [
					{
						"method": "GET",
						"path": "/person/:id"
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
	)

	configurer.Refresh(context.Background())

	r := make(chan Response, DefaultPendingWorkCapacity*2)
	b := newBatchList(
		configurer.Configuration,
		r,
		DefaultMaxEventsPerBatch,
		DefaultMaxConcurrentBatches,
	)
	b.reenqueue(events)

	for _, event := range events {
		eventFound := false
		for _, batch := range b.overflowBatches {
			for _, e := range batch {
				if reflect.DeepEqual(e, event) {
					eventFound = true
					break
				}
			}
		}

		assert.True(t, eventFound)

		// batchID := b.getOverflowBatchID(event.ID)
		// assert.Contains(t, b.overflowBatches[batchID], event)
	}
}

type notifier struct {
	mock.Mock
}

func (n *notifier) Done() {
	n.Called()
}

func TestBatchListFire(t *testing.T) {
	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			r := ioutil.NopCloser(bytes.NewBuffer([]byte("")))

			return &http.Response{
				StatusCode: 200,
				Body:       r,
			}, nil
		},
	}

	m.
		On("RoundTrip", mock.AnythingOfType("*http.Request")).
		Return(mock.AnythingOfType("*http.Response"), nil).Once()

	configurer, _ := config.NewConfigurer(
		config.WithConfigProvider(func() ([]byte, error) {
			return []byte(`{
				"base_url": "https://dev-api.auditr.io/v1",
				"events_path": "/events",
				"target": [
					{
						"method": "GET",
						"path": "/person/:id"
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
		config.WithHTTPClient(func() *http.Client {
			return &http.Client{
				Transport: m,
			}
		}),
	)

	configurer.Refresh(context.Background())

	n := &notifier{}
	n.On("Done").Once()

	event := &EventRaw{}

	r := make(chan Response, DefaultPendingWorkCapacity*2)
	b := newBatchList(
		configurer.Configuration,
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
	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			r := ioutil.NopCloser(bytes.NewBuffer([]byte("")))

			return &http.Response{
				StatusCode: 200,
				Body:       r,
			}, nil
		},
	}

	m.
		On("RoundTrip", mock.AnythingOfType("*http.Request")).
		Return(mock.AnythingOfType("*http.Response"), nil).Twice()

	maxEventsPerBatch := 10
	maxConcurrentBatches := 1
	configurer, _ := config.NewConfigurer(
		config.WithConfigProvider(func() ([]byte, error) {
			return []byte(fmt.Sprintf(`{
				"base_url": "https://dev-api.auditr.io/v1",
				"events_path": "/events",
				"target": [
					{
						"method": "GET",
						"path": "/person/:id"
					}
				],
				"sample": [],
				"flush": true,
				"cache_duration": 2,
				"max_events_per_batch": %d,
				"max_concurrent_batches": %d,
				"pending_work_capacity": 20,
				"send_interval": 20,
				"block_on_send": false,
				"block_on_response": false
			}`, maxEventsPerBatch, maxConcurrentBatches)), nil
		}),
		config.WithHTTPClient(func() *http.Client {
			return &http.Client{
				Transport: m,
			}
		}),
	)

	configurer.Refresh(context.Background())

	n := &notifier{}
	n.On("Done").Once()

	event := &EventRaw{
		Request: "",
	}
	payloadExclReqContent, _ := json.Marshal(event)
	// This will cause the batch to overflow
	event.Request = randomString(maxEventBytes - len(payloadExclReqContent) + 1)

	r := make(chan Response, DefaultPendingWorkCapacity*2)
	b := newBatchList(
		configurer.Configuration,
		r,
		uint(maxEventsPerBatch),
		uint(maxConcurrentBatches),
	)
	l := int(maxBatchBytes / maxEventBytes)
	for i := 0; i <= l; i++ {
		b.Add(event)
	}

	b.Fire(n)

	assert.True(t, m.AssertExpectations(t))
	assert.True(t, n.AssertExpectations(t))
}

func TestSend(t *testing.T) {
	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			r := ioutil.NopCloser(bytes.NewBuffer([]byte("")))

			return &http.Response{
				StatusCode: 200,
				Body:       r,
			}, nil
		},
	}

	m.
		On("RoundTrip", mock.AnythingOfType("*http.Request")).
		Return(mock.AnythingOfType("*http.Response"), nil).Once()

	configurer, _ := config.NewConfigurer(
		config.WithConfigProvider(func() ([]byte, error) {
			return []byte(`{
				"base_url": "https://dev-api.auditr.io/v1",
				"events_path": "/events",
				"target": [
					{
						"method": "GET",
						"path": "/person/:id"
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
		config.WithHTTPClient(func() *http.Client {
			return &http.Client{
				Transport: m,
			}
		}),
	)

	configurer.Refresh(context.Background())

	events := make([]*EventRaw, 3)
	for i := 0; i < len(events); i++ {
		events[i] = &EventRaw{}
	}

	r := make(chan Response, DefaultPendingWorkCapacity*2)
	b := newBatchList(
		configurer.Configuration,
		r,
		DefaultMaxEventsPerBatch,
		DefaultMaxConcurrentBatches,
	)
	b.send(events)

	assert.True(t, m.AssertExpectations(t))
}

func TestSend_GetResponseOnError(t *testing.T) {
	expectedErr := fmt.Errorf("random error")

	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			return nil, expectedErr
		},
	}

	m.
		On("RoundTrip", mock.AnythingOfType("*http.Request")).
		Return(mock.AnythingOfType("*http.Response"), nil).Twice()

	configurer, _ := config.NewConfigurer(
		config.WithConfigProvider(func() ([]byte, error) {
			return []byte(`{
				"base_url": "https://dev-api.auditr.io/v1",
				"events_path": "/events",
				"target": [
					{
						"method": "GET",
						"path": "/person/:id"
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
		config.WithHTTPClient(func() *http.Client {
			return &http.Client{
				Transport: m,
			}
		}),
	)

	configurer.Refresh(context.Background())

	events := make([]*EventRaw, 3)
	for i := 0; i < len(events); i++ {
		events[i] = &EventRaw{}
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
		configurer.Configuration,
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

	m := &test.MockTransport{
		Fn: func(m *test.MockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			r := ioutil.NopCloser(bytes.NewBuffer(expectedEventBody))

			return &http.Response{
				StatusCode: expectedEventStatusCode,
				Body:       r,
			}, nil
		},
	}

	m.
		On("RoundTrip", mock.AnythingOfType("*http.Request")).
		Return(mock.AnythingOfType("*http.Response"), nil).Once()

	configurer, _ := config.NewConfigurer(
		config.WithConfigProvider(func() ([]byte, error) {
			return []byte(`{
				"base_url": "https://dev-api.auditr.io/v1",
				"events_path": "/events",
				"target": [
					{
						"method": "GET",
						"path": "/person/:id"
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
		config.WithHTTPClient(func() *http.Client {
			return &http.Client{
				Transport: m,
			}
		}),
	)

	configurer.Refresh(context.Background())

	events := make([]*EventRaw, 3)
	for i := 0; i < len(events); i++ {
		events[i] = &EventRaw{}
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
		configurer.Configuration,
		r,
		DefaultMaxEventsPerBatch,
		DefaultMaxConcurrentBatches,
	)
	b.send(events)

	assert.True(t, m.AssertExpectations(t))

	wg.Wait()
}

func TestEncodeJSON(t *testing.T) {
	events := make([]*EventRaw, 3)
	for i := 0; i < len(events); i++ {
		events[i] = &EventRaw{}
	}

	configurer, _ := config.NewConfigurer(
		config.WithConfigProvider(func() ([]byte, error) {
			return []byte(`{
				"base_url": "https://dev-api.auditr.io/v1",
				"events_path": "/events",
				"target": [
					{
						"method": "GET",
						"path": "/person/:id"
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
	)

	configurer.Refresh(context.Background())

	r := make(chan Response, DefaultPendingWorkCapacity*2)
	b := newBatchList(
		configurer.Configuration,
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

	events := []*EventRaw{
		{
			Request: unmarshallable{
				Fn: func() {},
			},
		},
	}
	_, expectedErr := json.Marshal(events[0])

	configurer, _ := config.NewConfigurer(
		config.WithConfigProvider(func() ([]byte, error) {
			return []byte(`{
				"base_url": "https://dev-api.auditr.io/v1",
				"events_path": "/events",
				"target": [
					{
						"method": "GET",
						"path": "/person/:id"
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
	)

	configurer.Refresh(context.Background())

	r := make(chan Response, DefaultPendingWorkCapacity*2)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		res := <-r
		assert.Equal(t, expectedErr, res.Err)
	}()

	b := newBatchList(
		configurer.Configuration,
		r,
		DefaultMaxEventsPerBatch,
		DefaultMaxConcurrentBatches,
	)
	b.encodeJSON(events)

	wg.Wait()
}

func TestEncodeJSON_FailsOnOversizedEvent(t *testing.T) {
	t.Skip()
	event := &EventRaw{
		Request: "",
	}
	payloadExclReqContent, _ := json.Marshal(event)
	event.Request = randomString(maxEventBytes - len(payloadExclReqContent) + 1)

	events := []*EventRaw{
		event,
	}
	expectedErr := fmt.Errorf("Event exceeds max size of %d bytes", maxEventBytes)

	configurer, _ := config.NewConfigurer(
		config.WithConfigProvider(func() ([]byte, error) {
			return []byte(`{
				"base_url": "https://dev-api.auditr.io/v1",
				"events_path": "/events",
				"target": [
					{
						"method": "GET",
						"path": "/person/:id"
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
	)

	configurer.Refresh(context.Background())

	r := make(chan Response, DefaultPendingWorkCapacity*2)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		res := <-r
		assert.Equal(t, expectedErr, res.Err)
	}()

	b := newBatchList(
		configurer.Configuration,
		r,
		DefaultMaxEventsPerBatch,
		DefaultMaxConcurrentBatches,
	)
	b.encodeJSON(events)

	wg.Wait()
}

func TestEncodeJSON_ReenqueuesOnOversizedBatch(t *testing.T) {
	event := &EventRaw{
		Request: "",
	}
	payloadExclReqContent, _ := json.Marshal(event)
	event.Request = randomString(maxEventBytes - len(payloadExclReqContent) + 1)

	maxEventsPerBatch := 10
	maxConcurrentBatches := 1
	configurer, _ := config.NewConfigurer(
		config.WithConfigProvider(func() ([]byte, error) {
			return []byte(fmt.Sprintf(`{
				"base_url": "https://dev-api.auditr.io/v1",
				"events_path": "/events",
				"target": [
					{
						"method": "GET",
						"path": "/person/:id"
					}
				],
				"sample": [],
				"flush": true,
				"cache_duration": 2,
				"max_events_per_batch": %d,
				"max_concurrent_batches": %d,
				"pending_work_capacity": 20,
				"send_interval": 20,
				"block_on_send": false,
				"block_on_response": false
			}`, maxEventsPerBatch, maxConcurrentBatches)), nil
		}),
	)

	configurer.Refresh(context.Background())

	r := make(chan Response, DefaultPendingWorkCapacity*2)

	b := newBatchList(
		configurer.Configuration,
		r,
		uint(maxEventsPerBatch),
		uint(maxConcurrentBatches),
	)

	l := int(maxBatchBytes / maxEventBytes)
	events := make([]*EventRaw, l+1)
	for i := range events {
		events[i] = event
	}
	b.encodeJSON(events)

	overflowEvents := 0
	for _, batch := range b.overflowBatches {
		for _, e := range batch {
			if reflect.DeepEqual(e, event) {
				overflowEvents++
			}
		}
	}

	assert.Equal(t, 1, overflowEvents)
}

func randomString(length int) string {
	b := make([]byte, length/2)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
