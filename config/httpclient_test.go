package config

import (
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewHTTPClient_ReusesTransport(t *testing.T) {
	var wg sync.WaitGroup
	var clients []*http.Client
	expectedClients := 10
	wg.Add(expectedClients)

	for i := 0; i < expectedClients; i++ {
		go func() {
			defer wg.Done()
			client, err := NewHTTPClient("https://auditr.io")
			if err != nil {
				assert.Equal(t, "protocol https already registered", err.Error())
			}
			clients = append(clients, client)
		}()
	}

	wg.Wait()
	assert.Equal(t, expectedClients, len(clients))
	for _, c := range clients {
		assert.Equal(t, clients[0].Transport, c.Transport)
	}
}
