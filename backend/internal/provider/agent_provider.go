package provider

import (
	"errors"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// AgentProvider is the provider explicitly selected for a client. An absent
// selection preserves the client's existing routing behavior.
func AgentProvider(agent string) string {
	return heldSettings().AgentProviders[strings.ToLower(strings.TrimSpace(agent))]
}

func SetAgentProvider(agent, id string) error {
	agent = strings.ToLower(strings.TrimSpace(agent))
	if agent == "" {
		return errors.New("no agent")
	}
	s := settings.Load()
	if s.AgentProviders[agent] == id {
		return nil
	}
	if id == "" {
		delete(s.AgentProviders, agent)
	} else {
		if s.AgentProviders == nil {
			s.AgentProviders = map[string]string{}
		}
		s.AgentProviders[agent] = id
	}
	if err := settings.Save(s); err != nil {
		return err
	}
	catalog.Touched()
	return nil
}
