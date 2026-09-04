package store

import (
	"sync"

	"github.com/0x3ea/bigbrother/internal/prober"
)

var _ Store = (*MemoryStore)(nil)

// MemoryStore 进程内实现:map[name][]Result,每目标固定窗口
type MemoryStore struct {
	mu   sync.RWMutex
	hist map[string][]prober.Result
}

// NewMemory 返回就绪可用的 MemoryStore
func NewMemory() *MemoryStore {
	return &MemoryStore{
		hist: make(map[string][]prober.Result),
	}
}

// Save 追加一条结果,达到 MaxPerTarget 时滑掉最老的
func (ms *MemoryStore) Save(name string, r prober.Result) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	h := ms.hist[name]
	if len(h) >= MaxPerTarget {
		h = h[1:]
	}

	ms.hist[name] = append(h, r)
}

// Latest 返回最新一条;还没有任何结果时 ok=false
func (ms *MemoryStore) Latest(name string) (prober.Result, bool) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	h := ms.hist[name]
	if len(h) == 0 {
		return prober.Result{}, false
	}
	return h[len(h)-1], true
}

// History 返回最近 n 条,按时间升序;n<=0 返回 nil
func (ms *MemoryStore) History(name string, n int) []prober.Result {
	if n <= 0 {
		return nil
	}

	ms.mu.RLock()
	defer ms.mu.RUnlock()

	h := ms.hist[name]
	start := max(0, len(h)-n)

	out := make([]prober.Result, len(h)-start)
	copy(out, h[start:])
	return out
}
