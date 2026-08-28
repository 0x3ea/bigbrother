package prober

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/0x3ea/bigbrother/internal/config"
)

var defaultClient = http.DefaultClient

var _ Prober = (*HTTPProber)(nil)

type HTTPProber struct {
	Client *http.Client
}

func (p *HTTPProber) Type() string {
	return "http"
}

func (p *HTTPProber) Probe(ctx context.Context, t config.Target) Result {
	return probeHTTP(ctx, t, p.Client)
}

func probeHTTP(ctx context.Context, t config.Target, client *http.Client) Result {
	if client == nil {
		client = defaultClient
	}

	ctx, cancel := context.WithTimeout(ctx,
		time.Duration(t.TimeoutMS)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		return Result{
			Success:   false,
			Error:     err,
			Timestamp: time.Now(),
		}
	}

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return Result{
			Success:   false,
			Latency:   elapsed,
			Error:     err,
			Timestamp: time.Now(),
		}
	}
	defer resp.Body.Close()
	// 尝试复用(只有body被读到EOF,transport 才会认为该连接可复用)
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))

	res := Result{
		Success:    resp.StatusCode >= 200 && resp.StatusCode < 400,
		Latency:    elapsed,
		StatusCode: resp.StatusCode,
		Timestamp:  time.Now(),
	}

	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		res.TLSCertNotAfter = resp.TLS.PeerCertificates[0].NotAfter
	}

	return res
}
