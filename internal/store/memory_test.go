package store_test

import (
	"sync"
	"testing"
	"time"

	"github.com/0x3ea/bigbrother/internal/config"
	"github.com/0x3ea/bigbrother/internal/prober"
	"github.com/0x3ea/bigbrother/internal/store"
)

func newTarget() config.Target {
	return config.Target{
		Type:      "https",
		Target:    "https://www.zhihu.com/",
		IntervalS: 30,
		TimeoutMS: 5000,
	}
}

func newResult() prober.Result {
	return prober.Result{
		Success:         true,
		Latency:         time.Second,
		StatusCode:      200,
		Error:           nil,
		Timestamp:       time.Now(),
		TLSCertNotAfter: time.Now().Add(time.Hour * 300),
	}
}

func saveSequence(t *testing.T, s store.Store, target config.Target, n int) prober.Result {
	t.Helper()
	result := newResult()
	for range n {
		result.Timestamp = result.Timestamp.Add(time.Minute)
		result.TLSCertNotAfter = result.TLSCertNotAfter.Add(-time.Minute)
		s.Save(target, result)
	}
	return result
}

// Latest: target不存在
func TestLatest_TargetNotExist(t *testing.T) {
	s := store.NewMemory()
	_, ok := s.Latest("baidu.com")
	if ok {
		t.Fatalf("Latest() ok = true, want false")
	}
}

// Latest: 多条返回最新
func TestLatest_ReturnLatest(t *testing.T) {
	s := store.NewMemory()
	target := newTarget()
	last := saveSequence(t, s, target, store.MaxPerTarget)
	record, ok := s.Latest(target.Target)
	if !ok {
		t.Fatalf("Latest() ok = false, want true")
	}
	if record.Result != last {
		t.Fatalf("Latest() = %+v, want latest %+v", record.Result, last)
	}
}

// History: target不存在
func TestHistory_TargetNotExist(t *testing.T) {
	s := store.NewMemory()
	hist := s.History("https://www.zhihu.com/", 10)
	if len(hist) != 0 {
		t.Fatalf("History() len = %d, want 0", len(hist))
	}
}

// History: 历史数量<n
func TestHistory_CountNotEnough(t *testing.T) {
	s := store.NewMemory()
	target := newTarget()
	num := store.MaxPerTarget / 2
	saveSequence(t, s, target, num)
	hist := s.History(target.Target, num+5)
	if hist == nil {
		t.Fatalf("History(n=%d) = nil, want non-nil", num+5)
	}
	if len(hist) != num {
		t.Fatalf("History(n=%d) len = %d, want %d", num+5, len(hist), num)
	}
}

// History: 返回的是升序
func TestHistory_ReturnInAscending(t *testing.T) {
	s := store.NewMemory()
	target := newTarget()
	saveSequence(t, s, target, store.MaxPerTarget)
	hist := s.History(target.Target, store.MaxPerTarget)
	if hist == nil {
		t.Fatalf("History(n=%d) = nil, want non-nil", store.MaxPerTarget)
	}
	for i := 1; i < store.MaxPerTarget; i++ {
		if hist[i-1].Timestamp.After(hist[i].Timestamp) {
			t.Fatalf("History not ascending: hist[%d] = %v is after hist[%d] = %v",
				i-1, hist[i-1].Timestamp, i, hist[i].Timestamp)
		}
	}
}

// History: 返回的是副本
func TestHistory_ReturnIsCopy(t *testing.T) {
	s := store.NewMemory()
	target := newTarget()
	result := newResult()
	saveSequence(t, s, target, store.MaxPerTarget)
	hist := s.History(target.Target, store.MaxPerTarget)
	if hist == nil {
		t.Fatalf("History(n=%d) = nil, want non-nil", store.MaxPerTarget)
	}

	result.Timestamp = time.Time{}
	hist[store.MaxPerTarget-1].Result = result

	hist = s.History(target.Target, store.MaxPerTarget)
	if hist == nil {
		t.Fatalf("History(n=%d) = nil, want non-nil", store.MaxPerTarget)
	}

	if hist[store.MaxPerTarget-1].Result == result {
		t.Fatalf("History returned a view, not a copy: hist[%d].Result = %+v",
			store.MaxPerTarget-1, hist[store.MaxPerTarget-1].Result)
	}
}

// History: n<=0
func TestHistory_IllegalParam(t *testing.T) {
	s := store.NewMemory()
	target := newTarget()
	saveSequence(t, s, target, store.MaxPerTarget)
	hist := s.History(target.Target, 0)
	if hist != nil {
		t.Fatalf("History(n=0) len = %d, want nil", len(hist))
	}

	hist = s.History(target.Target, -1)
	if hist != nil {
		t.Fatalf("History(n=-1) len = %d, want nil", len(hist))
	}
}

// Save: 到达上限是否自动去掉最早记录
func TestSave_EraseOldest(t *testing.T) {
	s := store.NewMemory()
	target := newTarget()
	result := newResult()

	for i := 0; i <= store.MaxPerTarget; i++ {
		result.Latency = time.Duration(i) * time.Millisecond
		result.Timestamp = result.Timestamp.Add(time.Minute)
		result.TLSCertNotAfter = result.TLSCertNotAfter.Add(-time.Minute)
		s.Save(target, result)
	}
	hist := s.History(target.Target, store.MaxPerTarget)
	if hist == nil {
		t.Fatalf("History(n=%d) = nil, want non-nil", store.MaxPerTarget)
	}

	if len(hist) != store.MaxPerTarget {
		t.Fatalf("History() len = %d, want %d", len(hist), store.MaxPerTarget)
	}

	for i, record := range hist {
		if record.Latency == 0 {
			t.Fatalf("oldest record survived: hist[%d].Latency = %v, want evicted", i, record.Latency)
		}
	}
}

// 并发读写:多探测 goroutine 写 + 展示层(gin)并发读
// 不变量靠断言,竞态靠 make test 里的 -race
func TestSave_ConcurrentReadWrite(t *testing.T) {
	s := store.NewMemory()
	target := newTarget()

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		wg.Go(func() {
			defer wg.Done()
			for range 1000 {
				s.Save(target, newResult()) // 探测 goroutine 的写
				s.Latest(target.Target)     // HTTP handler 的读
				s.History(target.Target, 10)
			}
		})
	}
	wg.Wait()

	// 不变量:任意交错之后,窗口依然守得住
	hist := s.History(target.Target, store.MaxPerTarget+1)
	if len(hist) != store.MaxPerTarget {
		t.Fatalf("History() len = %d, want %d", len(hist), store.MaxPerTarget)
	}
}
