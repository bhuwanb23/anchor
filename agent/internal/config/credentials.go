package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func SaveCredentials(configPath, agentID, agentSecret, serverID string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	delete(raw, "registration_token")
	delete(raw, "agent_token")

	raw["agent_id"] = agentID
	raw["agent_secret"] = agentSecret
	raw["server_id"] = serverID

	out, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, out, 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}
