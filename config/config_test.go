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

func TestTargetRoutes(t *testing.T) {
	assert.NotEmpty(t, TargetRoutes, "TargetRoutes is empty")
}

func TestSampledRoutes(t *testing.T) {
	assert.NotEmpty(t, SampledRoutes, "TargetRoutes is empty")
}
