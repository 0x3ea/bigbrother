package prober

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/0x3ea/bigbrother/internal/config"
)

type Result struct {
	Success    bool
	Latency    time.Duration
	StatusCode int
	Error      error
	Timestamp  time.Time
	// TLS 证书的过期时间
	// 零值 = 非 TLS 或未拿到证书
	TLSCertNotAfter time.Time
}

type Prober interface {
	Type() string
	Probe(ctx context.Context, t config.Target) Result
}

func NewProber(t config.Target) (Prober, error) {
	u, err := url.Parse(t.URL)
	if err != nil {
		return nil, fmt.Errorf("parse target url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return &HTTPProber{}, nil
	case "https":
		return &HTTPSProber{}, nil
	default:
		return nil, fmt.Errorf("unsupported scheme %q: %s", u.Scheme, t.URL)
	}
}
