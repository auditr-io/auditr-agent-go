package collect

import (
	"fmt"
	"strings"
	"sync"

	"github.com/auditr-io/auditr-agent-go/config"
)

// RouteType describes the type of route; either target or sampled
type RouteType string

const (
	// RouteTypeTarget is a route that is targeted
	RouteTypeTarget RouteType = "target"

	// RouteTypeSampled is a route that is sampled
	RouteTypeSampled RouteType = "sampled"
)

// Param is a single URL parameter, consisting of a key and a value.
type Param struct {
	Key   string
	Value string
}

// Params is a Param-slice, as returned by the router.
// The slice is ordered, the first URL parameter is also the first slice value.
// It is therefore safe to read values by the index.
type Params []Param

// MatchedRoutePathParam is the Param name under which the path of the matched
// route is stored, if Router.SaveMatchedRoutePath is set.
var MatchedRoutePathParam = "$matchedRoutePath"

// ByName returns the value of the first Param which key matches the given name.
// If no matching Param is found, an empty string is returned.
func (ps Params) ByName(name string) string {
	for _, p := range ps {
		if p.Key == name {
			return p.Value
		}
	}
	return ""
}

// MatchedRoutePath retrieves the path of the matched route.
// Router.SaveMatchedRoutePath must have been enabled when the respective
// handler was added, otherwise this function always returns an empty string.
func (ps Params) MatchedRoutePath() string {
	return ps.ByName(MatchedRoutePathParam)
}

// Handle is a function that can be registered to a route to handle HTTP
// requests. Like http.HandlerFunc, but has a third parameter for the values of
// wildcards (path variables).
type Handle func() string

// newHandler creates a handler that simply returns the matching route
func newHandler(path string) Handle {
	return func() string {
		return path
	}
}

// Router matches the incoming request to a route that is targeted or sampled
type Router struct {
	paramsPool sync.Pool
	maxParams  uint16
	target     map[string]*node
	sampled    map[string]*node
}

// NewRouter creates a new router
func NewRouter(
	targetRoutes []config.Route,
	sampledRoutes []config.Route,
) *Router {
	r := &Router{
		target:  make(map[string]*node),
		sampled: make(map[string]*node),
	}

	r.addRoutes(r.target, targetRoutes)
	r.addRoutes(r.sampled, sampledRoutes)

	return r
}

// addRoutes adds routes to a tree of nodes
func (r *Router) addRoutes(tree map[string]*node, routes []config.Route) {
	for _, route := range routes {
		varsCount := uint16(0)
		root := tree[route.HTTPMethod]
		if root == nil {
			root = new(node)
			tree[route.HTTPMethod] = root
		}

		root.addRoute(route.Path, newHandler(route.Path))

		// Update maxParams
		if paramsCount := countParams(route.Path); paramsCount+varsCount > r.maxParams {
			r.maxParams = paramsCount + varsCount
		}

		// Lazy-init paramsPool alloc func
		if r.paramsPool.New == nil && r.maxParams > 0 {
			r.paramsPool.New = func() interface{} {
				ps := make(Params, 0, r.maxParams)
				return &ps
			}
		}
	}
}

// getParams provides a ready-to-use params store from a pre-allocated pool
func (r *Router) getParams() *Params {
	ps, _ := r.paramsPool.Get().(*Params)
	*ps = (*ps)[0:0] // reset slice
	return ps
}

// putParams adds params to the pool
func (r *Router) putParams(ps *Params) {
	if ps != nil {
		r.paramsPool.Put(ps)
	}
}

// FindRoute finds the matching route for a given method and path pair.
// Returns a config.Route containing the matching method and path
// The matching path is not the same as the input path argument.
// Example:
// 		input path 		: "/events/123"
// 		matching path 	: "/events/:id"
func (r *Router) FindRoute(
	routeType RouteType,
	method string,
	path string,
) (*config.Route, error) {
	var tree map[string]*node
	switch routeType {
	case RouteTypeTarget:
		tree = r.target
	case RouteTypeSampled:
		tree = r.sampled
	default:
		return nil, fmt.Errorf("routeType must be RouteTypeTarget or RouteTypeSampled")
	}

	if method == "" {
		return nil, fmt.Errorf("method cannot be empty")
	}

	method = strings.ToUpper(method)

	root, ok := tree[method]
	if ok {
		handler, ps, _ := root.getValue(path, r.getParams)
		if handler != nil {
			if ps != nil {
				r.putParams(ps)
			}

			matchingPath := handler()

			return &config.Route{
				HTTPMethod: method,
				Path:       matchingPath,
			}, nil
		}
	}

	return nil, nil
}

// SampleRoute adds a new route to sampled routes
func (r *Router) SampleRoute(
	method string,
	path string,
	resource string,
) *config.Route {
	method = strings.ToUpper(method)

	root, ok := r.sampled[method]
	if !ok {
		root = new(node)
		r.sampled[method] = root
	}

	var route *config.Route
	if resource != "{proxy+}" {
		r := strings.NewReplacer("{", ":", "}", "")
		route = &config.Route{
			HTTPMethod: method,
			Path:       r.Replace(resource),
		}
	}

	root.addRoute(route.Path, newHandler(route.Path))

	return route
}
