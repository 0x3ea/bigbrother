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
	"io"
	"log"
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
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
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
				Target: srv.URL, IntervalS: 1, TimeoutMS: 1000,
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

func TestProberHTTP_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) }))
	defer srv.Close()
	p := &prober.HTTPProber{}
	res := p.Probe(context.Background(), config.Target{
		Target: srv.URL, IntervalS: 1, TimeoutMS: 50,
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

func TestProberHTTP_ConnectionRefused(t *testing.T) {
	// 先占一个空闲端口再立刻释放:拿到一个"语法合法、确定没人监听"的地址
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()

	p := &prober.HTTPProber{}

	res := p.Probe(context.Background(), config.Target{
		Target: "http://" + addr, IntervalS: 1, TimeoutMS: 1000,
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

func TestProberHTTP_InvalidURL(t *testing.T) {
	cases := []string{
		"http://example.com/%zz",
		"http://[::1",
		"http://example.com:abc",
	}
	p := &prober.HTTPProber{}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			res := p.Probe(context.Background(), config.Target{
				Target: raw, IntervalS: 1, TimeoutMS: 1000,
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

// https
// 有效证书
// 临期证书
// 过期证书
// 是http

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
				Target: srv.URL, IntervalS: 1, TimeoutMS: 1000,
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
		Target: srv.URL, IntervalS: 1, TimeoutMS: 1000,
	})
	if res.Success {
		t.Fatal("expected failure: target is plain http")
	}
	if !strings.Contains(res.Error.Error(), "no TLS certificate") {
		t.Errorf("error = %v, want type-mismatch failure", res.Error)
	}
}

// tcp

// newTCPListener 起一个真实监听,Accept 到连接就关掉
func newTCPListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener 已关闭,退出
			}
			conn.Close()
		}
	}()
	return ln.Addr().String()
}

// 连接正常/失败
func TestProberTCP(t *testing.T) {
	cases := []struct {
		name        string
		setup       func(t *testing.T) string
		timeout     time.Duration
		wantOK      bool
		wantTimeout bool
	}{
		{
			name:        "port_open",
			setup:       newTCPListener,
			timeout:     time.Second,
			wantOK:      true,
			wantTimeout: false,
		},
		{
			name: "port_close",
			setup: func(t *testing.T) string {
				ln, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				addr := ln.Addr().String()
				ln.Close() // 先占后放,保证拿到没人监听的端口
				return addr
			},
			timeout:     time.Second,
			wantOK:      false,
			wantTimeout: false,
		},
		{
			name:        "address_unreachable",
			setup:       func(t *testing.T) string { return "192.0.2.1:80" },
			timeout:     200 * time.Millisecond,
			wantOK:      false,
			wantTimeout: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := prober.TCPProber{}
			res := p.Probe(context.Background(), config.Target{
				Target:    tc.setup(t),
				IntervalS: 10,
				TimeoutMS: tc.timeout.Milliseconds(),
			})

			if res.Success != tc.wantOK {
				t.Fatalf("Success = %v, expect %v, err = %v", res.Success, tc.wantOK, res.Error)
			}

			if res.Latency <= 0 {
				t.Errorf("latency = %v, want > 0", res.Latency)
			}
			if res.Latency > tc.timeout+500*time.Millisecond {
				t.Errorf("latency %v much longer than timeout %v", res.Latency, tc.timeout)
			}
			if tc.wantTimeout {
				// ctx 的 timer 和 fd 的写截止同时到点,谁先响决定错误类型:
				// ctx 先响 → context.DeadlineExceeded;fd 先响 → *net.OpError(i/o timeout)。
				// http.Client 会统一映射成前者,裸 DialContext 不会 —— 两种都得认。
				var ne net.Error
				if !errors.Is(res.Error, context.DeadlineExceeded) &&
					!(errors.As(res.Error, &ne) && ne.Timeout()) {
					t.Fatalf("error = %v, want timeout error", res.Error)
				}
			}
		})
	}
}
