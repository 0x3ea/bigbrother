package store

import (
	"github.com/0x3ea/bigbrother/internal/prober"
)

type Store interface {
	// Save 落一条结果;失败返回 error,由调用方决定怎么处理
	Save(name string, r prober.Result) error
	// Latest 最新一条;尚无数据 ok=false(常态),读失败 err!=nil
	Latest(name string) (prober.Result, bool, error)
	// History 最近 n 条,时间升序;n<=0 返回 (nil, nil)
	History(name string, n int) ([]prober.Result, error)
}
