package auditrgorilla

import (
	"net/http"

	"github.com/gorilla/mux"
)

func Middleware(handler http.Handler) http.Handler {
	wrappedHandler := func(w http.ResponseWriter, req *http.Request) {
		route := mux.CurrentRoute(req)
		if route != nil {
			// name := route.GetName()
		}

		handler.ServeHTTP(w, req)
	}

	return http.HandlerFunc(wrappedHandler)
}
