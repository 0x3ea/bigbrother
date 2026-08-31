package prober

import (
	"context"
	"net"
	"time"

	"github.com/0x3ea/bigbrother/internal/config"
)

var _ Prober = (*TCPProber)(nil)

type TCPProber struct{}

func (p *TCPProber) Type() string {
	return "tcp"
}
func (p *TCPProber) Probe(ctx context.Context, t config.Target) Result {
	ctx, cancel := context.WithTimeout(ctx,
		time.Duration(t.TimeoutMS)*time.Millisecond)
	defer cancel()
	start := time.Now()
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", t.Target)
	latency := time.Since(start)

	if err != nil {
		return Result{
			Success:   false,
			Latency:   latency,
			Error:     err,
			Timestamp: time.Now(),
		}
	}
	defer conn.Close()
	return Result{
		Success:   true,
		Latency:   latency,
		Timestamp: time.Now(),
	}
}
