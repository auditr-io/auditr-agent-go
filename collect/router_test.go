package collect

import (
	"net/http"
	"testing"

	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/stretchr/testify/assert"
)

func TestSampleRoute_RouteFoundAfterSampling(t *testing.T) {
	r := NewRouter(
		[]config.Route{},
		[]config.Route{},
	)

	sampledRoute := r.SampleRoute(http.MethodGet, "/person/xyz", "/person/{id}")
	foundRoute, err := r.FindRoute(RouteTypeSampled, http.MethodGet, "/person/xyz")
	assert.NoError(t, err)
	assert.Equal(t, sampledRoute, foundRoute)
}
