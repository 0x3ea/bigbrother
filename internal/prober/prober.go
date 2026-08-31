package prober

import (
	"context"
	"fmt"
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
	switch strings.ToLower(t.Type) {
	case "http":
		return &HTTPProber{}, nil
	case "https":
		return &HTTPSProber{}, nil
	case "tcp":
		return &TCPProber{}, nil
	default:
		return nil, fmt.Errorf("unsupported prober type %q: %s", t.Type, t.Target)
	}
}
