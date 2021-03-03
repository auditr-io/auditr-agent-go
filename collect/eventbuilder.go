package collect

import (
	"encoding/json"

	"github.com/auditr-io/auditr-agent-go/config"
)

// EventBuilder builds an event from the given parameters
type EventBuilder interface {
	// Build builds an event from the given parameters
	Build(
		routeType RouteType,
		route *config.Route,
		request interface{},
		response json.RawMessage,
		errorValue json.RawMessage,
	) (*Event, error)
}
