package collector

import "github.com/auditr-io/auditr-agent-go/config"

// Event is an audit event
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
