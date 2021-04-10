package config

import (
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewHTTPClient_ReusesTransport(t *testing.T) {
	var wg sync.WaitGroup
	expectedClients := 10
	wg.Add(expectedClients)

	clients := make([]*http.Client, expectedClients)
	for i := 0; i < expectedClients; i++ {
		go func(n int) {
			defer wg.Done()
			client, err := NewHTTPClient("https://auditr.io")
			assert.NoError(t, err)
			clients[n] = client
		}(i)
	}

	wg.Wait()
	assert.Equal(t, expectedClients, len(clients))
	for i, c := range clients {
		assert.NotNil(t, clients[i])
		assert.Equal(t, clients[0].Transport, c.Transport)
	}
}
