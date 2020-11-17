package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBaseUrl(t *testing.T) {
	assert.NotEmpty(t, BaseUrl, "BaseUrl is empty")
}

func TestEventsUrl(t *testing.T) {
	assert.NotEmpty(t, EventsUrl, "EventsUrl is empty")
}
