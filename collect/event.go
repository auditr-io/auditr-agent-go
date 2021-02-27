package collect

import "github.com/auditr-io/auditr-agent-go/config"

// Event is an audit event
// easyjson:json
type Event struct {
	ID          string        `json:"id"`
	Action      string        `json:"action"`
	Actor       *Actor        `json:"actor"`
	ActorID     string        `json:"actor_id"`
	RouteType   RouteType     `json:"route_type"`
	Route       *config.Route `json:"route"`
	Location    string        `json:"location"`
	RequestID   string        `json:"request_id"`
	RequestedAt int64         `json:"requested_at"`
	Request     interface{}   `json:"request"`
	Response    interface{}   `json:"response"`
	Error       interface{}   `json:"error"`
}

// Actor is the user originating the action
// easyjson:json
type Actor struct {
	ID       string `json:"actor_id"`
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
	Email    string `json:"email,omitempty"`
}

// EventList is a list of events
//easyjson:json
type EventList []*Event
