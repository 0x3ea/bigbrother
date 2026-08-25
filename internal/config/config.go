package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type Target struct {
	URL       string `json:"url"`
	IntervalS int64  `json:"interval_s"`
}

const TargetsEnvironment = "BIGBROTHER_TARGETS"

func ParseTargets(data []byte) ([]Target, error) {
	var ts []Target
	if err := json.Unmarshal(data, &ts); err != nil {
		return nil, fmt.Errorf("parse targets failed: %w", err)
	}
	for _, t := range ts {
		if t.URL == "" {
			return nil, fmt.Errorf("URL is empty")
		}
		if t.IntervalS <= 0 {
			return nil, fmt.Errorf("target %s must have positive interval_s", t.URL)
		}
	}
	return ts, nil
}

func LoadTargets() ([]Target, error) {
	path := os.Getenv(TargetsEnvironment)
	if path == "" {
		return nil, errors.New("configuration file path not specified in environment variable: " + TargetsEnvironment)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)

	}
	return ParseTargets(data)
}
