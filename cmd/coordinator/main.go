package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/0x3ea/bigbrother/internal/config"
	"github.com/0x3ea/bigbrother/internal/prober"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	targets, err := config.LoadTargets()
	if err != nil {
		log.Fatal(err)
	}

	var wg sync.WaitGroup
	for _, t := range targets {
		p, err := prober.NewProber(t) // NewProber 在这里第一次有了调用者
		if err != nil {
			log.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			runTarget(ctx, p, t)
		}()
	}

	log.Printf("bigbrother started, %d targets", len(targets))
	wg.Wait() // Ctrl+C → ctx 取消 → 各 goroutine 收尾退出

}

// runTarget 按 t.IntervalS 周期探测一个目标,直到 ctx 取消
func runTarget(ctx context.Context, p prober.Prober, t config.Target) {
	interval := time.Duration(t.IntervalS) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		res := p.Probe(ctx, t) // 启动立刻探一次,不等第一个 tick
		logResult(p, t, res)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// logResult 目前只是打印;阶段 0 后半接存储时,把它换成 store.Save(...)
func logResult(p prober.Prober, t config.Target, res prober.Result) {
	if res.Success {
		log.Printf("[%s] %s OK latency=%v", p.Type(), t.Target, res.Latency)
		return
	}
	log.Printf("[%s] %s FAIL latency=%v err=%v status=%d", p.Type(), t.Target, res.Latency, res.Error, res.StatusCode)
}
