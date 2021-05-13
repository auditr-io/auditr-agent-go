package collect

// Event is an audit event
// type Event struct {
// 	ID            string      `json:"id"`
// 	Action        string      `json:"action"`
// 	ActorID       string      `json:"actor_id"`
// 	ActorName     string      `json:"actor_name"`
// 	ActorUsername string      `json:"actor_username"`
// 	ActorEmail    string      `json:"actor_email"`
// 	RouteType     RouteType   `json:"route_type"`
// 	HTTPMethod    string      `json:"http_method"`
// 	RoutePath     string      `json:"route_path"`
// 	Location      string      `json:"location"`
// 	RequestID     string      `json:"request_id"`
// 	RequestedAt   int64       `json:"requested_at"`
// 	Request       interface{} `json:"request"`
// 	Response      interface{} `json:"response"`
// 	Error         interface{} `json:"error"`
// }

// Event is a raw audit event
// A raw audit event is a set of minimal fields required for an audit event.
// The event will later be enriched based on these fields.
type EventRaw struct {
	Organization *EventOrganization `json:"organization"`
	Agent        *EventAgent        `json:"agent,omitempty"`
	Route        *EventRoute        `json:"route"`
	User         *EventUser         `json:"user,omitempty"`
	Client       *EventClient       `json:"client"`
	RequestedAt  int64              `json:"requested_at"`
	Request      interface{}        `json:"request"`
	Response     interface{}        `json:"response"`
	Error        interface{}        `json:"error,omitempty"`
}

// RouteType describes the type of route; either target or sample
type RouteType string

const (
	// RouteTypeTarget is a route that is targeted
	RouteTypeTarget RouteType = "target"

	// RouteTypeSample is a route that is sample
	RouteTypeSample RouteType = "sample"
)

// EventRoute is the route where the event occurred
// todo: drop "Event"?
type EventRoute struct {
	Type   RouteType `json:"type"`
	Method string    `json:"method"`
	Path   string    `json:"path"`
}

// EventOrganization is the organization of the client
// https://github.com/elastic/ecs/blob/1.9/code/go/ecs/organization.go
type EventOrganization struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// EventAgent is the agent sending the event
// https://github.com/elastic/ecs/blob/1.9/code/go/ecs/agent.go
type EventAgent struct {
	Name    string `json:"name,omitempty"`
	Type    string `json:"type,omitempty"`
	Version string `json:"version,omitempty"`
}

// EventUser is the user who triggered the event
// https://github.com/elastic/ecs/blob/1.9/code/go/ecs/user.go
type EventUser struct {
	ID       string `json:"id,omitempty"`
	Email    string `json:"email,omitempty"`
	FullName string `json:"full_name,omitempty"`
	Name     string `json:"name,omitempty"`
	Domain   string `json:"domain,omitempty"`
}

// EventClient is the client originating the event
// https://github.com/elastic/ecs/blob/1.9/code/go/ecs/client.go
type EventClient struct {
	// Address is the raw address; can be IP, domain, unix socket.
	Address string `json:"address,omitempty"`
	Bytes   int64  `json:"bytes,omitempty"`
	Domain  string `json:"domain,omitempty"`
	IP      string `json:"ip,omitempty"`
	Port    int    `json:"port,omitempty"`
}
