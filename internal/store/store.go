package store

import (
	"github.com/0x3ea/bigbrother/internal/prober"
)

// MaxPerTarget 每个目标在内存里最多保留多少条历史
const MaxPerTarget = 100

type Store interface {
	Save(name string, r prober.Result)
	Latest(name string) (prober.Result, bool)
	History(name string, n int) []prober.Result
}
