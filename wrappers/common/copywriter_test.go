package common

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCopyWriter(t *testing.T) {
	expectedStatusCode := 200
	expectedHeaders := http.Header{
		"Content-Type": {"application/json"},
	}
	expectedBodyBytes := []byte(`{
		"hi": "you"
	}`)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		cw := NewCopyWriter(w)

		for k, v := range expectedHeaders {
			cw.Header().Add(k, v[0])
		}
		cw.WriteHeader(expectedStatusCode)
		cw.Write(expectedBodyBytes)

		res := cw.Response()

		assert.Equal(t, expectedStatusCode, res.StatusCode)
		assert.Equal(t, expectedHeaders, res.Header)

		body, _ := ioutil.ReadAll(res.Body)
		assert.Equal(t, expectedBodyBytes, body)
	})

	w := httptest.NewRecorder()
	r, _ := http.NewRequest("POST", "/", nil)
	mux.ServeHTTP(w, r)

	res := w.Result()
	assert.Equal(t, expectedStatusCode, res.StatusCode)
	assert.Equal(t, expectedHeaders, res.Header)

	body, _ := ioutil.ReadAll(res.Body)
	assert.Equal(t, expectedBodyBytes, body)
}
