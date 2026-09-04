package api

import (
	"net/http"
	"time"

	"github.com/0x3ea/bigbrother/internal/config"
	"github.com/0x3ea/bigbrother/internal/prober"
	"github.com/gin-gonic/gin"
)

type targetResp struct {
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	Target     string     `json:"target"`
	Status     string     `json:"status"` // up | down | pending
	LatencyMS  int64      `json:"latency_ms,omitempty"`
	StatusCode int        `json:"status_code,omitempty"`
	Error      string     `json:"error,omitempty"`
	At         *time.Time `json:"at,omitempty"`
}

func newTargetResp(cfg config.Target, r prober.Result) targetResp {
	status := "down"
	if r.Success {
		status = "up"
	}

	errMsg := ""
	if r.Error != nil {
		errMsg = r.Error.Error()
	}
	return targetResp{
		Name:       cfg.Name,
		Type:       cfg.Type,
		Target:     cfg.Target,
		Status:     status,
		LatencyMS:  r.Latency.Milliseconds(),
		StatusCode: r.StatusCode,
		At:         &r.Timestamp,
		Error:      errMsg,
	}
}

func pendingResp(cfg config.Target) targetResp { // 有这个目标,但还没数据
	return targetResp{Name: cfg.Name, Type: cfg.Type, Target: cfg.Target, Status: "pending"}
}

func (s *Server) status(c *gin.Context) {
	items := make([]targetResp, 0)
	for _, t := range s.targets {
		if r, ok := s.store.Latest(t.Name); ok {
			items = append(items, newTargetResp(t, r))
		} else {
			items = append(items, pendingResp(t))
		}
	}
	c.JSON(http.StatusOK, items)
}
