package config

import (
	"errors"
	"fmt"
	"time"
)

const defaultAgentRunLoopTimeout = 150 * time.Second

type AgentRunConfig struct {
	LoopTimeout time.Duration
}

func LoadAgentRun(textProviderTimeout time.Duration) (AgentRunConfig, error) {
	if textProviderTimeout <= 0 {
		return AgentRunConfig{}, errors.New(
			"text provider timeout must be greater than zero",
		)
	}
	loopTimeout, err := durationOrDefault(
		"AGENT_RUN_LOOP_TIMEOUT",
		defaultAgentRunLoopTimeout,
	)
	if err != nil {
		return AgentRunConfig{}, err
	}
	if loopTimeout <= 0 {
		return AgentRunConfig{}, errors.New(
			"AGENT_RUN_LOOP_TIMEOUT must be greater than zero",
		)
	}
	if loopTimeout <= textProviderTimeout {
		return AgentRunConfig{}, fmt.Errorf(
			"AGENT_RUN_LOOP_TIMEOUT must be greater than the text provider timeout %s",
			textProviderTimeout,
		)
	}
	return AgentRunConfig{LoopTimeout: loopTimeout}, nil
}
