package auth

import (
	"sync"
	"testing"
)

const testProberName = "test"

// Issue 生成的key 合法(Verify 通过)
func TestIssueValid(t *testing.T) {
	st := NewMemoryKeyStore()
	key := st.Issue(testProberName)
	if !st.Verify(testProberName, key) {
		t.Fatal("Issue return invalid key")
	}
}

// 每次Issue生成的key不一样
func TestIssueDifferent(t *testing.T) {
	st := NewMemoryKeyStore()
	set := make(map[string]struct{})

	for range 100 {
		key := st.Issue(testProberName)
		set[key] = struct{}{}
	}
	if len(set) != 100 {
		t.Fatal("Issue return same key")
	}
}

// Verify对非法输入的处理 (空key/空name/未注册name/长度不足)
func TestVerifyWithIllegalInput(t *testing.T) {
	st := NewMemoryKeyStore()
	key := st.Issue(testProberName)

	tests := []struct {
		name       string
		proberName string
		key        string
		want       bool
	}{
		{"empty_name", "", key, false},
		{"empty_key", testProberName, "", false},
		{"empty_both", "", "", false},
		{"unregistered_name", "nobody", key, false},
		{"key_truncated", testProberName, key[:len(key)-1], false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := st.Verify(tt.proberName, tt.key); got != tt.want {
				t.Fatalf("Verify(%q, key=%q) = %v, want %v",
					tt.proberName, tt.key, got, tt.want)
			}
		})
	}
}

// 同名重发,旧key失效
func TestMultiRegister(t *testing.T) {
	st := NewMemoryKeyStore()
	oldKey := st.Issue(testProberName)
	st.Issue(testProberName)
	if st.Verify(testProberName, oldKey) {
		t.Fatal("old key not expired in multiRegister")
	}
}

// Verify: 合法key最后一位被篡改,必须拒绝
func TestVerify_WrongKey(t *testing.T) {
	st := NewMemoryKeyStore()
	key := st.Issue(testProberName)

	last := key[len(key)-1]
	flipped := byte('0')
	if last == '0' {
		flipped = '1'
	}
	wrong := key[:len(key)-1] + string(flipped)

	if st.Verify(testProberName, wrong) {
		t.Fatal("Verify accepted a tampered key")
	}
}

// 并发Issue不能触发data race(锁修复后)
func TestIssueConcurrent(t *testing.T) {
	st := NewMemoryKeyStore()
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() { defer wg.Done(); st.Issue(testProberName) }()
	}
	wg.Wait()
	if !st.Verify(testProberName, st.Issue(testProberName)) {
		t.Fatal("key issued after concurrent writes failed Verify")
	}
}

// key不能以其他proberName通过Verify
func TestVerify_KeyOfOtherProber(t *testing.T) {
	st := NewMemoryKeyStore()
	st.Issue(testProberName)

	key1 := st.Issue("prober-a")
	if st.Verify(testProberName, key1) {
		t.Fatal("other prober key can pass Verify")
	}
}
