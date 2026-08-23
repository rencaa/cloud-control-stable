package handlers

import (
	"fmt"
	"testing"
	"time"
)

func resetLoginLimiterForTest(t *testing.T) {
	t.Helper()
	loginLimit.Lock()
	previous := loginLimit.entries
	loginLimit.entries = make(map[string]loginLimitEntry)
	loginLimit.Unlock()
	t.Cleanup(func() {
		loginLimit.Lock()
		loginLimit.entries = previous
		loginLimit.Unlock()
	})
}

func TestLoginLimitRejectsNewKeysAtCapacity(t *testing.T) {
	resetLoginLimiterForTest(t)
	loginLimit.Lock()
	for i := 0; i < loginLimitMaxEntries; i++ {
		loginLimit.entries[fmt.Sprintf("known-%d", i)] = loginLimitEntry{WindowStart: time.Now()}
	}
	loginLimit.Unlock()
	if allowLoginAttempt("attacker-random-username") {
		t.Fatal("new login key was accepted after the limiter reached capacity")
	}
	loginLimit.Lock()
	count := len(loginLimit.entries)
	_, created := loginLimit.entries["attacker-random-username"]
	loginLimit.Unlock()
	if count != loginLimitMaxEntries || created {
		t.Fatalf("limiter grew at capacity: count=%d created=%v", count, created)
	}
}

func TestLoginLimitPrunesExpiredKeysBeforeRejectingNewKey(t *testing.T) {
	resetLoginLimiterForTest(t)
	loginLimit.Lock()
	for i := 0; i < loginLimitMaxEntries-1; i++ {
		loginLimit.entries[fmt.Sprintf("active-%d", i)] = loginLimitEntry{WindowStart: time.Now()}
	}
	loginLimit.entries["expired"] = loginLimitEntry{WindowStart: time.Now().Add(-31 * time.Minute)}
	loginLimit.Unlock()
	if !allowLoginAttempt("new-user") {
		t.Fatal("new key was rejected even though an expired entry could be pruned")
	}
	loginLimit.Lock()
	_, expiredExists := loginLimit.entries["expired"]
	_, newExists := loginLimit.entries["new-user"]
	loginLimit.Unlock()
	if expiredExists || !newExists {
		t.Fatalf("prune result invalid: expired=%v new=%v", expiredExists, newExists)
	}
}
