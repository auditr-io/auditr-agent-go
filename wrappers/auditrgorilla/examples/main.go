package main

import (
	"log"
	"net/http"
	"time"

	"github.com/auditr-io/auditr-agent-go/wrappers/auditrgorilla"
	"github.com/gorilla/mux"
)

// a sample implementation with gorilla/mux
// to run:
//   AUDITR_CONFIG_URL=https://config.auditr.io AUDITR_API_KEY=prik_xxx go run .
func main() {
	a, err := auditrgorilla.NewAgent()
	if err != nil {
		log.Fatal(err)
	}

	router := mux.NewRouter()
	router.Use(a.Middleware)
	router.HandleFunc("/hi/{name}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
			"hi": "homer"
		}`))
	})

	srv := &http.Server{
		Handler:      router,
		Addr:         "127.0.0.1:8000",
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}
