package prober

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/0x3ea/bigbrother/internal/config"
)

// 证书剩余有效期低于该天数时,探测判定为失败(临期告警)
const certExpireWarnDays = 14

var _ Prober = (*HTTPSProber)(nil)

type HTTPSProber struct {
	Client *http.Client
}

func (p *HTTPSProber) Type() string {
	return "https"
}

func (p *HTTPSProber) Probe(ctx context.Context, t config.Target) Result {
	res := probeHTTP(ctx, t, p.Client)
	if !res.Success {
		return res
	}
	if res.TLSCertNotAfter.IsZero() {
		// 请求成功但没拿到证书,说明目标实际是 http,类型配错了
		res.Success = false
		res.Error = fmt.Errorf("no TLS certificate presented by %s, check target type", t.Target)
		return res
	}
	if daysLeft := time.Until(res.TLSCertNotAfter).Hours() / 24; daysLeft < certExpireWarnDays {
		res.Success = false
		res.Error = fmt.Errorf("certificate expires in %.1f days (< %d days threshold)",
			daysLeft, certExpireWarnDays)
	}
	return res
}
