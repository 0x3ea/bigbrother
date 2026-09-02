package store

import (
	"github.com/0x3ea/bigbrother/internal/config"
	"github.com/0x3ea/bigbrother/internal/prober"
)

// MaxPerTarget 每个目标在内存里最多保留多少条历史
const MaxPerTarget = 100

type Record struct {
	Type   string // http / https / tcp
	Target string
	prober.Result
}

type Store interface {
	Save(t config.Target, r prober.Result)
	Latest(target string) (Record, bool)
	History(target string, n int) []Record
}
