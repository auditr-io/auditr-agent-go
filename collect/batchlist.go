package collect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/facebookgo/muster"
	"github.com/hashicorp/terraform/helper/hashcode"
)

const (
	// max bytes allowed per event
	maxEventBytes int = 25000 // 25kb

	// number of batches to hold events exceeding maxBatchBytes
	// Overflow exceeding this will not be processed.
	maxOverflowBatches int = 10
)

// Response is the result of processing an event
type Response struct {
	Err        error
	StatusCode int
	Body       []byte
}

// UnmarshalJSON deserializes response from processing an event
func (r *Response) UnmarshalJSON(b []byte) error {
	res := struct {
		Error  string
		Status int
	}{}
	if err := json.Unmarshal(b, &res); err != nil {
		return err
	}

	r.StatusCode = res.Status
	if res.Error != "" {
		r.Err = errors.New(res.Error)
	}

	return nil
}

// batchList is a list of batches.
// This batch handling implementation is shamelessly borrowed from
// Honeycomb's libhoney.
type batchList struct {
	maxEventsPerBatch    uint
	maxConcurrentBatches uint

	// max bytes allowed per batch
	maxBatchBytes int

	// batches of events
	batches map[int][]*Event

	// holds batches exceeding maxBatchSize
	overflowBatches map[int][]*Event

	responses       chan Response
	blockOnResponse bool

	client *http.Client
}

// newBatchList creates a new batch list
func newBatchList(
	responses chan Response,
	maxEventsPerBatch uint,
	maxConcurrentBatches uint,
) *batchList {
	b := &batchList{
		batches:              map[int][]*Event{},
		overflowBatches:      map[int][]*Event{},
		responses:            responses,
		maxEventsPerBatch:    maxEventsPerBatch,
		maxConcurrentBatches: maxConcurrentBatches,
		maxBatchBytes:        int(maxEventsPerBatch) * maxEventBytes,
		client:               config.GetEventsClient(),
	}

	return b
}

// Add adds an event to a batch
func (b *batchList) Add(event interface{}) {
	e := event.(*Event)
	batchID := b.getBatchID(e.ID)
	b.batches[batchID] = append(b.batches[batchID], e)
}

// Fire informs muster the batch is done
func (b *batchList) Fire(notifier muster.Notifier) {
	defer notifier.Done()

	for _, events := range b.batches {
		b.send(events)
	}

	// Batches exceeding maxBatchBytes will overflow. Process
	// overflow batches until complete.
	overflowProcessed := 0
	for len(b.overflowBatches) > 0 {
		if overflowProcessed > maxOverflowBatches {
			// Should never happen because once the batch is processing
			// the overflows dwindle and you can't add more to the batch.
			break
		}

		overflowProcessed++

		// Get the current snapshot of batch IDs in overflow. This could change
		// as we process the overflow batches.
		batchIDs := make([]int, len(b.overflowBatches))
		i := 0
		for batchID := range b.overflowBatches {
			batchIDs[i] = batchID
			i++
		}

		// Send the overflow batch.
		for _, batchID := range batchIDs {
			events := b.overflowBatches[batchID]

			// Remove the current overflow batch from the list before sending
			// so if there are more overflow events with the same batch ID as
			// a result of this send, we'll process them in the next round.
			delete(b.overflowBatches, batchID)
			b.send(events)
		}
	}
}

// getBatchID determines the batchID given an item ID
func (b *batchList) getBatchID(id string) int {
	h := hashcode.String(id)
	return h % int(b.maxConcurrentBatches)
}

// getOverflowBatchID determines the batchID given an item ID
func (b *batchList) getOverflowBatchID(id string) int {
	h := hashcode.String(id)
	return h % int(maxOverflowBatches)
}

// enqueueResponseForEvents enqueues a response for each event in the event list
func (b *batchList) enqueueResponseForEvents(res Response, events []*Event) {
	for _, event := range events {
		if event != nil {
			b.enqueueResponse(res)
		}
	}
}

// enqueueResponse writes the response to the response channel
func (b *batchList) enqueueResponse(res Response) {
	if writeToChannel(b.responses, res, b.blockOnResponse) {
		// no-op
	}
}

// writeToChannel writes a response to a given channel.
// If channel is full and:
// 		* block is true, this will block
// 		* block is false, this will drop the response
// Returns true if event was dropped, false otherwise.
func writeToChannel(responses chan Response, res Response, block bool) bool {
	if block {
		responses <- res
	} else {
		select {
		case responses <- res:
		default:
			return true
		}
	}

	return false
}

// reenqueue reenqueues events for processing
func (b *batchList) reenqueue(events []*Event) {
	for _, e := range events {
		batchID := b.getOverflowBatchID(e.ID)
		b.overflowBatches[batchID] = append(b.overflowBatches[batchID], e)
	}
}

type httpError interface {
	Timeout() bool
}

// send sends a batch of events to the target URL
func (b *batchList) send(events []*Event) {
	if len(events) == 0 {
		// should never happen, but just in case
		return
	}

	eventsJSON, numEncoded := b.encodeJSON(events)
	if numEncoded == 0 {
		// nothing encoded
		return
	}

	ctx := context.Background()
	method := http.MethodPost
	var req *http.Request
	var res *http.Response
	var err error

	// retry once in case of timeouts
	for n := 0; n < 2; n++ {
		eventsReader := ioutil.NopCloser(bytes.NewReader(eventsJSON))

		req, err = http.NewRequestWithContext(
			ctx,
			method,
			config.EventsURL,
			eventsReader,
		)
		if err != nil {
			b.enqueueResponseForEvents(Response{Err: err}, events)
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", fmt.Sprintf("auditr-agent-go/%s", version))

		res, err = b.client.Do(req)
		if err != nil {
			continue
		}
		// if httpErr, ok := err.(httpError); ok && httpErr.Timeout() {
		// 	continue
		// }
		break
	}

	if err != nil {
		b.enqueueResponseForEvents(Response{Err: err}, events)
		return
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		errRes := Response{
			Err: fmt.Errorf(
				"Error sending %s %s: status %d",
				method,
				config.EventsURL,
				res.StatusCode,
			),
			StatusCode: res.StatusCode,
		}

		if res.StatusCode == http.StatusBadRequest {
			log.Printf("eventsJSON: %s", string(eventsJSON))
		}

		// todo: retry on 5xx
		body, err := ioutil.ReadAll(res.Body)
		if err != nil {
			errRes.Err = err
		} else {
			errRes.Body = body
		}

		b.enqueueResponseForEvents(errRes, events)
		return
	}

	var batchResponses []Response
	err = json.NewDecoder(res.Body).Decode(&batchResponses)
	if err != nil {
		b.enqueueResponseForEvents(Response{Err: err}, events)
		return
	}

	// i := 0
	for _, eventRes := range batchResponses {
		// Find index of matching event for this response
		// for i < len(events) && events[i] == nil {
		// 	i++
		// }

		// if i >= len(events) {
		// 	break
		// }

		b.enqueueResponse(eventRes)
		// i++
	}
}

// encodeJSON encodes a batch of events to JSON
func (b *batchList) encodeJSON(events []*Event) ([]byte, int) {
	buf := bytes.Buffer{}
	buf.WriteByte('[')
	numEncoded := 0
	for i, e := range events {
		if i > 0 {
			buf.WriteByte(',')
		}

		payload, err := json.Marshal(e)
		if err != nil {
			b.enqueueResponse(Response{
				Err: err,
			})
			events[i] = nil
			continue
		}

		if len(payload) > maxEventBytes {
			b.enqueueResponse(Response{
				Err: fmt.Errorf("Event exceeds max size of %d bytes", maxEventBytes),
			})
			events[i] = nil
			continue
		}

		if buf.Len()+len(payload)+1 > b.maxBatchBytes {
			b.reenqueue(events[i:])
			break
		}

		buf.Write(payload)
		numEncoded++
	}

	buf.WriteByte(']')
	return buf.Bytes(), numEncoded
}
