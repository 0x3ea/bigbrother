package registry

import "time"

// Node 探针所在的物理节点
type Node struct {
	Name         string
	Region       string
	Capabilities []string
	// 由Registry维护,传入值被忽略
	// TODO 考虑把字段分离,不暴露给调用方
	LastSeen time.Time
}

// Registry 节点维护接口
type Registry interface {
	Register(n Node) error
	Touch(name string) error
	Get(name string) (Node, bool, error)
	List() ([]Node, error)
	// Online 判断节点是否在线,ttl<=0 表示永不过期;节点不存在返回 ErrNodeNotFound
	Online(name string) (bool, error)
	// TODO 添加Remove方法
}
