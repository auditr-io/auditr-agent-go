package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBaseURL(t *testing.T) {
	assert.NotEmpty(t, BaseURL, "BaseURL is empty")
}

func TestEventsURL(t *testing.T) {
	assert.NotEmpty(t, EventsURL, "EventsURL is empty")
}

func TestTargetRoutes(t *testing.T) {
	assert.NotEmpty(t, TargetRoutes, "TargetRoutes is empty")
}

func TestSampledRoutes(t *testing.T) {
	assert.NotEmpty(t, SampledRoutes, "TargetRoutes is empty")
}
