package prober_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/0x3ea/bigbrother/internal/config"
	"github.com/0x3ea/bigbrother/internal/prober"
)

func newTLSServer(t *testing.T, notAfter time.Time) *httptest.Server {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{
		Certificate: [][]byte{der}, PrivateKey: key,
	}}}
	srv.StartTLS()
	return srv
}

// http
// 能否正常工作(roundtrip) 200/204
// 是否正常失败 404/500/503
// 非法url
// 超时控制是否生效
// 拒绝连接(无服务)

// https
// 有效证书
// 临期证书
// 过期证书
// 是http

func TestProberHTTP_StatusCode(t *testing.T) {
	cases := []struct {
		name   string
		status int
		wantOK bool
	}{
		{"200_ok", 200, true},
		{"204_no_content", 204, true},
		{"404_not_found", 404, false},
		{"500_server_error", 500, false},
		{"503_unavailable", 503, false},
	}
	p := &prober.HTTPProber{}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(c.status) }))
			defer srv.Close()

			res := p.Probe(context.Background(), config.Target{
				URL: srv.URL, IntervalS: 1, TimeoutMS: 1000,
			})

			if res.Success != c.wantOK {
				t.Errorf("Success = %v, want %v", res.Success, c.wantOK)
			}

			if res.StatusCode != c.status {
				t.Errorf("StatusCode = %d, want %d", res.StatusCode, c.status)
			}
		})
	}

}

func TestHTTPProber_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) }))
	defer srv.Close()
	p := &prober.HTTPProber{}
	res := p.Probe(context.Background(), config.Target{
		URL: srv.URL, IntervalS: 1, TimeoutMS: 50,
	})

	if res.Success {
		t.Fatal("expected failure")
	}
	if !errors.Is(res.Error, context.DeadlineExceeded) {
		t.Errorf("error = %v, want context.DeadlineExceeded", res.Error)
	}
	// 超时的意义就是"及时止损":50ms 就该返回,而不是等 handler 睡完 300ms
	if res.Latency >= 300*time.Millisecond {
		t.Errorf("latency = %v, timeout should cut the request short", res.Latency)
	}
}

func TestHTTPProber_ConnectionRefused(t *testing.T) {
	// 先占一个空闲端口再立刻释放:拿到一个"语法合法、确定没人监听"的地址
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()

	p := &prober.HTTPProber{}

	res := p.Probe(context.Background(), config.Target{
		URL: "http://" + addr, IntervalS: 1, TimeoutMS: 1000,
	})
	if res.Success {
		t.Fatal("expected failure")
	}
	// 不只断言失败:确认是拨号阶段的错误,而不是别的什么
	var opErr *net.OpError
	if !errors.As(res.Error, &opErr) {
		t.Errorf("error = %v, want *net.OpError (dial failure)", res.Error)
	}
}

func TestHTTPProber_InvalidURL(t *testing.T) {
	cases := []string{
		"http://example.com/%zz",
		"http://[::1",
		"http://example.com:abc",
	}
	p := &prober.HTTPProber{}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			res := p.Probe(context.Background(), config.Target{
				URL: raw, IntervalS: 1, TimeoutMS: 1000,
			})
			if res.Success {
				t.Fatal("expected failure")
			}
			// 死在解析,不是死在发送
			var ue *url.Error
			if !errors.As(res.Error, &ue) || ue.Op != "parse" {
				t.Errorf("error = %v, want parse-stage *url.Error", res.Error)
			}
			// 该分支没碰网络,Latency 应为零 —— 和 client.Do 分支的输出差异
			if res.Latency != 0 {
				t.Errorf("latency = %v, want 0", res.Latency)
			}
		})
	}
}

func TestProberHTTPS_CertValidity(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name       string
		notAfter   time.Time
		wantOK     bool
		wantErrSub string // 空 = 不应有错误
	}{
		{"valid_90d", now.Add(90 * 24 * time.Hour), true, ""},
		{"above_threshold_15d", now.Add(15 * 24 * time.Hour), true, ""},
		{"near_expiry_13d", now.Add(13 * 24 * time.Hour), false, "expires in"},
		{"expired", now.Add(-time.Hour), false, "has expired"}, // 死在握手,不是策略分支
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newTLSServer(t, c.notAfter)
			defer srv.Close()
			p := &prober.HTTPSProber{Client: srv.Client()}
			res := p.Probe(context.Background(), config.Target{
				URL: srv.URL, IntervalS: 1, TimeoutMS: 1000,
			})
			if res.Success != c.wantOK {
				t.Fatalf("Success = %v, want %v (err = %v)", res.Success, c.wantOK, res.Error)
			}
			switch {
			case c.wantErrSub == "":
				if res.Error != nil {
					t.Errorf("unexpected error: %v", res.Error)
				}
				// 成功时必须带回了证书过期时间
				if res.TLSCertNotAfter.IsZero() {
					t.Error("TLSCertNotAfter is zero on success")
				}
			case !strings.Contains(res.Error.Error(), c.wantErrSub):
				t.Errorf("error = %v, want containing %q", res.Error, c.wantErrSub)
			}
		})
	}
}

func TestProberHTTPS_PlainHTTPTarget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	res := (&prober.HTTPSProber{}).Probe(context.Background(), config.Target{
		URL: srv.URL, IntervalS: 1, TimeoutMS: 1000,
	})
	if res.Success {
		t.Fatal("expected failure: target is plain http")
	}
	if !strings.Contains(res.Error.Error(), "no TLS certificate") {
		t.Errorf("error = %v, want type-mismatch failure", res.Error)
	}
}

func TestNewProber(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		wantType string // 空 = 期望报错
	}{
		{"http", "http://example.com", "http"},
		{"https", "https://example.com", "https"},
		{"ftp_unsupported", "ftp://example.com", ""},
		{"missing_scheme", "example.com", ""},
		{"bad_url", "http://[::1", ""},
		{"uppercase_scheme", "HTTP://example.com", "http"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := prober.NewProber(config.Target{URL: c.url, IntervalS: 1, TimeoutMS: 1000})
			if c.wantType == "" {
				if err == nil {
					t.Fatalf("want error, got prober %T", p)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := p.Type(); got != c.wantType {
				t.Errorf("Type() = %q, want %q", got, c.wantType)
			}
		})
	}
}
