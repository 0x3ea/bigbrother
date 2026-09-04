package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/0x3ea/bigbrother/internal/api"
	"github.com/0x3ea/bigbrother/internal/config"
	"github.com/0x3ea/bigbrother/internal/prober"
	"github.com/0x3ea/bigbrother/internal/store"
)

const listenAddr = ":8080"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	targets, err := config.LoadTargets()
	if err != nil {
		log.Fatal(err)
	}

	st := store.NewMemory()

	var wg sync.WaitGroup
	for _, t := range targets {
		p, err := prober.NewProber(t) // NewProber 在这里第一次有了调用者
		if err != nil {
			log.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			runTarget(ctx, st, p, t)
		}()
	}

	// HTTP 层:用 http.Server 包住 Engine,而不是 r.Run()——为了拿到 Shutdown 的把手
	srv := api.New(st, targets)
	hs := &http.Server{Addr: listenAddr, Handler: srv.Engine()}
	go func() {
		if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http server exit: %v", err)
		}
	}()
	log.Printf("bigbrother started, %d targets, listening on %s", len(targets), listenAddr)

	wg.Wait() // Ctrl+C → ctx 取消 → 各探测 goroutine 收尾

	// 探测先停写,再关 HTTP:等在途请求处理完(最多 3 秒)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := hs.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
}

// runTarget 按 t.IntervalMS 周期探测一个目标,直到 ctx 取消
func runTarget(ctx context.Context, st store.Store, p prober.Prober, t config.Target) {
	interval := time.Duration(t.IntervalMS) * time.Millisecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		res := p.Probe(ctx, t)
		st.Save(t.Name, res) // 探测 → 存储;API 从这里读
		logResult(p, t, res)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func logResult(p prober.Prober, t config.Target, res prober.Result) {
	if res.Success {
		log.Printf("[%s] name=%s target=%s OK latency=%v", p.Type(), t.Name, t.Target, res.Latency)
		return
	}
	log.Printf("[%s] name=%s target=%s FAIL latency=%v err=%v status=%d", p.Type(), t.Name, t.Target, res.Latency, res.Error, res.StatusCode)
}
