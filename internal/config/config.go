package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Target struct {
	Type      string `json:"type"`
	Target    string `json:"target"`
	IntervalS int64  `json:"interval_s"`
	TimeoutMS int64  `json:"timeout_ms"`
}

const TargetsEnvironment = "BIGBROTHER_TARGETS"

func validateTCPAddr(addr string) error {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("tcp target %q format illegal: %w", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("tcp target %q port illegal: %q", addr, portStr)
	}
	if host == "" {
		return fmt.Errorf("tcp target %q missing host", addr)
	}
	return nil
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("http/https target %q format illegal: %w", raw, err)
	}
	if u.Host == "" {
		return fmt.Errorf("http/https target %q missing host", raw)
	}
	return nil
}

func (t *Target) Validate() error {
	switch strings.ToLower(t.Type) {
	case "http", "https":
		if err := validateURL(t.Target); err != nil {
			return err
		}
	case "tcp":
		if err := validateTCPAddr(t.Target); err != nil {
			return err
		}
	default:
		return fmt.Errorf("target %q unknown prober type: %q (only support http/https/tcp)", t.Target, t.Type)
	}
	if t.IntervalS <= 0 {
		return fmt.Errorf("target %q must have positive interval_s", t.Target)
	}
	if t.TimeoutMS <= 0 {
		return fmt.Errorf("target %q must have positive TimeoutMS", t.Target)
	}
	return nil
}
func ParseTargets(data []byte) ([]Target, error) {
	var ts []Target
	if err := json.Unmarshal(data, &ts); err != nil {
		return nil, fmt.Errorf("parse targets failed: %w", err)
	}
	for _, t := range ts {
		if err := t.Validate(); err != nil {
			return nil, err
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
