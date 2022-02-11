package common

import (
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCopyWriter(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		cw := NewCopyWriter(w)

		expectedStatusCode := 200
		expectedHeaders := http.Header{
			"Content-Type": {"application/json"},
		}
		expectedBodyBytes := []byte(`{
			"hi": "you"
		}`)

		for k, v := range expectedHeaders {
			w.Header().Add(k, v[0])
		}
		w.WriteHeader(expectedStatusCode)
		w.Write(expectedBodyBytes)

		res := cw.Response()

		assert.Equal(t, expectedStatusCode, res.StatusCode)
		assert.Equal(t, expectedHeaders, res.Header)

		body, _ := ioutil.ReadAll(res.Body)
		assert.Equal(t, expectedBodyBytes, body)
	})
}
