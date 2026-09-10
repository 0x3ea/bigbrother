package store

import (
	"sync"

	"github.com/0x3ea/bigbrother/internal/prober"
)

// MaxPerTarget 每个目标在内存里最多保留多少条历史
const MaxPerTarget = 100

var _ Store = (*MemoryStore)(nil)

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

// Save 实现 Store.Save;内存写不会失败,窗口由 MaxPerTarget 控制
func (ms *MemoryStore) Save(name string, r prober.Result) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	h := ms.hist[name]
	if len(h) >= MaxPerTarget {
		h = h[1:]
	}

	ms.hist[name] = append(h, r)
	return nil
}

// Latest 实现 Store.Latest
func (ms *MemoryStore) Latest(name string) (prober.Result, bool, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	h := ms.hist[name]
	if len(h) == 0 {
		return prober.Result{}, false, nil
	}
	return h[len(h)-1], true, nil
}

// History 实现 Store.History
func (ms *MemoryStore) History(name string, n int) ([]prober.Result, error) {
	if n <= 0 {
		return nil, nil
	}

	ms.mu.RLock()
	defer ms.mu.RUnlock()

	h := ms.hist[name]
	start := max(0, len(h)-n)

	out := make([]prober.Result, len(h)-start)
	copy(out, h[start:])
	return out, nil
}
