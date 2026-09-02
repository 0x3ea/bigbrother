package store

import (
	"sync"

	"github.com/0x3ea/bigbrother/internal/config"
	"github.com/0x3ea/bigbrother/internal/prober"
)

var _ Store = (*MemoryStore)(nil)

// MemoryStore 进程内实现:map[target][]Record,每目标固定窗口
type MemoryStore struct {
	mu   sync.RWMutex
	hist map[string][]Record
}

// NewMemory 返回就绪可用的 MemoryStore
func NewMemory() *MemoryStore {
	return &MemoryStore{
		hist: make(map[string][]Record),
	}
}

// Save 追加一条结果,达到 MaxPerTarget 时滑掉最老的
func (ms *MemoryStore) Save(tgt config.Target, r prober.Result) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	record := Record{
		Type:   tgt.Type,
		Target: tgt.Target,
		Result: r,
	}
	h := ms.hist[tgt.Target]
	if len(h) >= MaxPerTarget {
		h = h[1:]
	}

	ms.hist[tgt.Target] = append(h, record)
}

// Latest 返回最新一条;还没有任何结果时 ok=false
func (ms *MemoryStore) Latest(target string) (Record, bool) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	h := ms.hist[target]
	if len(h) == 0 {
		return Record{}, false
	}
	return h[len(h)-1], true
}

// History 返回最近 n 条,按时间升序;n<=0 返回 nil
func (ms *MemoryStore) History(target string, n int) []Record {
	if n <= 0 {
		return nil
	}

	ms.mu.RLock()
	defer ms.mu.RUnlock()

	h := ms.hist[target]
	start := max(0, len(h)-n)

	out := make([]Record, len(h)-start)
	copy(out, h[start:])
	return out
}
