package registry

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// 注册后调用方改自己手里的切片,不能污染注册表里的数据
func TestRegisterClonesInput(t *testing.T) {
	mr := NewMemory(15 * time.Second)

	n := Node{
		Name:         "node-1",
		Region:       "cn-north",
		Capabilities: []string{"http", "tcp"},
	}
	if err := mr.Register(n); err != nil {
		t.Fatalf("Register() err = %v", err)
	}

	n.Capabilities[0] = "tampered"

	got, ok, err := mr.Get("node-1")
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}
	if !ok {
		t.Fatal("Get() ok = false, register node should exist")
	}
	if got.Capabilities[0] != "http" {
		t.Fatalf("register table was contaminated: got.Capabilities = %v, want [http tcp]", got.Capabilities)
	}
}

// 空 name 必须被拒绝,且注册表里不能留下任何东西
func TestRegisterEmptyName(t *testing.T) {
	mr := NewMemory(15 * time.Second)

	err := mr.Register(Node{Region: "cn-north"})
	if err == nil {
		t.Fatal("Register() with empty name should return error, got nil")
	}

	_, ok, err := mr.Get("")
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}
	if ok {
		t.Fatal("node with empty name was stored in registry")
	}
}

// Touch 不存在的节点,应返回 ErrNodeNotFound
func TestTouchNodeNotFound(t *testing.T) {
	mr := NewMemory(15 * time.Second)

	err := mr.Touch("ghost")
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("Touch() on missing node should return ErrNodeNotFound, got %v", err)
	}
}

// Touch 之后 LastSeen 必须前进
func TestTouchUpdatesLastSeen(t *testing.T) {
	mr := NewMemory(15 * time.Second)

	if err := mr.Register(Node{Name: "node-1"}); err != nil {
		t.Fatalf("Register() err = %v", err)
	}
	before, _, err := mr.Get("node-1")
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	if err := mr.Touch("node-1"); err != nil {
		t.Fatalf("Touch() err = %v", err)
	}

	after, ok, err := mr.Get("node-1")
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}
	if !ok {
		t.Fatal("Get() ok = false, registered node should exist")
	}
	if !after.LastSeen.After(before.LastSeen) {
		t.Fatalf("Touch() did not advance LastSeen: before = %v, after = %v", before.LastSeen, after.LastSeen)
	}
}

// Get 不存在的节点:契约是 (零值, false, nil),"查不到"不是错误
func TestGetNodeNotFound(t *testing.T) {
	mr := NewMemory(15 * time.Second)

	got, ok, err := mr.Get("ghost")
	if err != nil {
		t.Fatalf("Get() on missing node should return nil error, got %v", err)
	}
	if ok {
		t.Fatal("Get() ok = true, missing node should not be found")
	}

	if !reflect.DeepEqual(got, Node{}) {
		t.Fatalf("Get() on missing node should return zero Node, got %+v", got)
	}
}

// 先 Register 再 Get,字段要原样带回来;LastSeen 由 Register 盖时间戳
func TestGetRoundtrip(t *testing.T) {
	mr := NewMemory(15 * time.Second)

	n := Node{
		Name:         "node-1",
		Region:       "cn-north",
		Capabilities: []string{"http", "tcp"},
	}
	if err := mr.Register(n); err != nil {
		t.Fatalf("Register() err = %v", err)
	}

	got, ok, err := mr.Get("node-1")
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}
	if !ok {
		t.Fatal("Get() ok = false, registered node should exist")
	}

	if got.LastSeen.IsZero() {
		t.Fatal("Register() should stamp LastSeen, got zero time")
	}

	want := n
	want.LastSeen = got.LastSeen
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Get() mismatch: got %+v, want %+v", got, want)
	}
}

// 改坏 Get 返回的节点,不能污染注册表里的数据
func TestGetReturnsClone(t *testing.T) {
	mr := NewMemory(15 * time.Second)

	n := Node{
		Name:         "node-1",
		Capabilities: []string{"http", "tcp"},
	}
	if err := mr.Register(n); err != nil {
		t.Fatalf("Register() err = %v", err)
	}

	got, _, err := mr.Get("node-1")
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}

	got.Capabilities[0] = "tampered"

	again, _, err := mr.Get("node-1")
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}
	if again.Capabilities[0] != "http" {
		t.Fatalf("registry was contaminated via Get(): got.Capabilities = %v, want [http tcp]", again.Capabilities)
	}
}

// 改坏 List 返回的节点,不能污染注册表里的数据
func TestListReturnsClone(t *testing.T) {
	mr := NewMemory(15 * time.Second)

	for _, name := range []string{"node-1", "node-2"} {
		if err := mr.Register(Node{Name: name, Capabilities: []string{"http", "tcp"}}); err != nil {
			t.Fatalf("Register() err = %v", err)
		}
	}

	nodes, err := mr.List()
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}

	nodes[0].Capabilities[0] = "tampered"

	again, err := mr.List()
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}
	for _, n := range again {
		if n.Capabilities[0] != "http" {
			t.Fatalf("registry was contaminated via List(): %s.Capabilities = %v, want [http tcp]", n.Name, n.Capabilities)
		}
	}
}

// TTL 到期后 Online 判离线
func TestOnlineTimeout(t *testing.T) {
	mr := NewMemory(50 * time.Millisecond)

	if err := mr.Register(Node{Name: "node-1"}); err != nil {
		t.Fatalf("Register() err = %v", err)
	}

	online, err := mr.Online("node-1")
	if err != nil {
		t.Fatalf("Online() err = %v", err)
	}
	if !online {
		t.Fatal("node should be online right after Register")
	}

	time.Sleep(120 * time.Millisecond)

	online, err = mr.Online("node-1")
	if err != nil {
		t.Fatalf("Online() err = %v", err)
	}
	if online {
		t.Fatal("node should be offline after ttl")
	}
}

// TTL=0 表示永不过期,Online 恒为 true
func TestZeroTtlNeverExpires(t *testing.T) {
	mr := NewMemory(0)

	if err := mr.Register(Node{Name: "node-1"}); err != nil {
		t.Fatalf("Register() err = %v", err)
	}

	time.Sleep(120 * time.Millisecond)

	online, err := mr.Online("node-1")
	if err != nil {
		t.Fatalf("Online() err = %v", err)
	}
	if !online {
		t.Fatal("node with ttl=0 should always be online")
	}
}

// nil Capabilities 原样保留,clone 不能把 nil 变成空切片
func TestNilCapabilitiesPreserved(t *testing.T) {
	mr := NewMemory(15 * time.Second)

	if err := mr.Register(Node{Name: "node-1"}); err != nil {
		t.Fatalf("Register() err = %v", err)
	}

	got, ok, err := mr.Get("node-1")
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}
	if !ok {
		t.Fatal("Get() ok = false, registered node should exist")
	}
	if got.Capabilities != nil {
		t.Fatalf("nil Capabilities should be preserved, got %v", got.Capabilities)
	}
}

// 过期节点心跳一次就复活——Touch 存在的意义
func TestTouchRevivesExpiredNode(t *testing.T) {
	mr := NewMemory(50 * time.Millisecond)

	if err := mr.Register(Node{Name: "node-1"}); err != nil {
		t.Fatalf("Register() err = %v", err)
	}

	time.Sleep(120 * time.Millisecond)

	online, err := mr.Online("node-1")
	if err != nil {
		t.Fatalf("Online() err = %v", err)
	}
	if online {
		t.Fatal("node should be expired before Touch")
	}

	if err := mr.Touch("node-1"); err != nil {
		t.Fatalf("Touch() err = %v", err)
	}

	online, err = mr.Online("node-1")
	if err != nil {
		t.Fatalf("Online() err = %v", err)
	}
	if !online {
		t.Fatal("node should be revived after Touch")
	}
}

// Online 一个不存在的名字,返回 ErrNodeNotFound
func TestOnlineNodeNotFound(t *testing.T) {
	mr := NewMemory(15 * time.Second)

	online, err := mr.Online("ghost")
	if online {
		t.Fatal("Online() on missing node should return false")
	}
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("Online() on missing node should return ErrNodeNotFound, got %v", err)
	}
}
