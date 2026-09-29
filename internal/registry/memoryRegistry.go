package registry

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrNodeNotFound 节点不存在
var ErrNodeNotFound = errors.New("registry: node not found")

var _ Registry = (*MemoryRegistry)(nil)

// MemoryRegistry 内存中维护已注册的节点
type MemoryRegistry struct {
	ttl   time.Duration
	mu    sync.RWMutex
	nodes map[string]Node
}

// NewMemory 创建MemoryRegistry
func NewMemory(ttl time.Duration) *MemoryRegistry {
	return &MemoryRegistry{nodes: map[string]Node{}, ttl: ttl}
}

// Register 注册 Node,重复注册 = 覆盖 + 重新计时
func (mr *MemoryRegistry) Register(n Node) error {
	if n.Name == "" {
		return fmt.Errorf("registry: node name is empty")
	}

	n.LastSeen = time.Now()

	mr.mu.Lock()
	defer mr.mu.Unlock()
	mr.nodes[n.Name] = cloneNode(n)

	return nil
}

// Touch 心跳机制,只更新LastSeen
func (mr *MemoryRegistry) Touch(name string) error {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	n, ok := mr.nodes[name]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNodeNotFound, name)
	}
	n.LastSeen = time.Now()
	mr.nodes[name] = n
	return nil
}

// Get 获取指定节点的信息
func (mr *MemoryRegistry) Get(name string) (Node, bool, error) {
	mr.mu.RLock()
	defer mr.mu.RUnlock()
	n, ok := mr.nodes[name]
	if !ok {
		return Node{}, false, nil
	}
	return cloneNode(n), true, nil
}

// List 获取当前所有已注册节点的信息
func (mr *MemoryRegistry) List() ([]Node, error) {
	mr.mu.RLock()
	defer mr.mu.RUnlock()

	nodes := make([]Node, 0, len(mr.nodes))
	for _, n := range mr.nodes {
		nodes = append(nodes, cloneNode(n))
	}
	return nodes, nil
}

// Online 判断节点是否在线,ttl<=0 表示永不过期
func (mr *MemoryRegistry) Online(name string) (bool, error) {
	mr.mu.RLock()
	defer mr.mu.RUnlock()

	n, ok := mr.nodes[name]
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrNodeNotFound, name)
	}
	return mr.ttl <= 0 || time.Since(n.LastSeen) <= mr.ttl, nil
}

func cloneNode(n Node) Node {
	if n.Capabilities == nil {
		return n
	}
	caps := make([]string, len(n.Capabilities))
	copy(caps, n.Capabilities)
	n.Capabilities = caps
	return n
}
