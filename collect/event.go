package collect

// Event is an audit event
type Event struct {
	ID            string      `json:"id"`
	Action        string      `json:"action"`
	ActorID       string      `json:"actor_id"`
	ActorName     string      `json:"actor_name"`
	ActorUsername string      `json:"actor_username"`
	ActorEmail    string      `json:"actor_email"`
	RouteType     RouteType   `json:"route_type"`
	HTTPMethod    string      `json:"http_method"`
	RoutePath     string      `json:"route_path"`
	Location      string      `json:"location"`
	RequestID     string      `json:"request_id"`
	RequestedAt   int64       `json:"requested_at"`
	Request       interface{} `json:"request"`
	Response      interface{} `json:"response"`
	Error         interface{} `json:"error"`
}

// RouteType describes the type of route; either target or sample
type RouteType string

const (
	// RouteTypeTarget is a route that is targeted
	RouteTypeTarget RouteType = "target"

	// RouteTypeSample is a route that is sample
	RouteTypeSample RouteType = "sample"
)
