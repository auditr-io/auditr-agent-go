package auditr

import (
	"log"

	"github.com/auditr-io/auditr-agent-go/agent"
)

var agentInstance *agent.Agent

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
	agentInstance = agent.New()
}
