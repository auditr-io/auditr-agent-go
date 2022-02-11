package auditrgorilla

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
)

type hiHandler struct {
	ServeFn func(h hiHandler, w http.ResponseWriter, r *http.Request)
}

func (h hiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.ServeFn(h, w, r)
}

func TestMiddleware(t *testing.T) {
	wantResBodyBytes := []byte(`{
		"id": 123,
		"name": "homer"
	}`)
	wantResStatusCode := 200

	router := mux.NewRouter()
	router.Use(Middleware)
	router.Handle("/hi/{id}", hiHandler{
		ServeFn: func(h hiHandler, w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(wantResStatusCode)
			w.Write(wantResBodyBytes)
		},
	})

	reqBodyBytes := []byte(`{
		"name": "homer"
	}`)
	req, _ := http.NewRequest("POST", "/hi/123", bytes.NewBuffer(reqBodyBytes))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	result := rec.Result()
	assert.Equal(t, wantResStatusCode, result.StatusCode)

	resBodyBytes, _ := ioutil.ReadAll(result.Body)
	assert.Equal(t, wantResBodyBytes, resBodyBytes)
}
