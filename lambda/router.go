package lambda

import (
	"sync"

	"github.com/auditr-io/auditr-agent-go/config"
)

type Router struct {
	paramsPool sync.Pool
	maxParams  uint16
	target     map[string]*node
	sampled    map[string]*node
}

func newRouter(
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

func (r *Router) getParams() *Params {
	ps, _ := r.paramsPool.Get().(*Params)
	*ps = (*ps)[0:0] // reset slice
	return ps
}

func (r *Router) putParams(ps *Params) {
	if ps != nil {
		r.paramsPool.Put(ps)
	}
}

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

func newHandler(path string) Handle {
	return func() string {
		return path
	}
}
