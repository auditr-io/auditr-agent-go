package agent

import (
	"log"
	"os"
	"strings"

	"github.com/auditr-io/auditr-agent-go/lambda"
)

var agentInstance *lambda.Agent

// Audit wraps the handler function so the agent can intercept
// and record events
func Audit(handler interface{}) interface{} {
	if agentInstance == nil {
		log.Println("auditr.go: agentInstance is nil")
		return handler
	}

	return agentInstance.Wrap(handler)
}

// init initializes the auditr agent
func init() {
	if strings.HasSuffix(os.Args[0], ".test") {
		// Skip init if testing
		return
	}

	agentInstance, _ = lambda.New()
}
