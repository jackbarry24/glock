package core

import (
	"testing"

	"github.com/google/uuid"
)

func TestIsValidOwner(t *testing.T) {
	tests := []struct {
		name  string
		uid   string
		valid bool
	}{
		{"valid uuid", "550e8400-e29b-41d4-a716-446655440000", true},
		{"empty string", "", false},
		{"invalid format", "not-a-uuid", false},
		{"partial uuid", "550e8400-e29b", false},
		{"uppercase uuid", "550E8400-E29B-41D4-A716-446655440000", true},
		{"random string", "random", false},
		{"numbers only", "12345678", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidOwner(tt.uid)
			if result != tt.valid {
				t.Errorf("IsValidOwner(%q) = %v, want %v", tt.uid, result, tt.valid)
			}
		})
	}
}

func TestFnv32(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		stable bool
	}{
		{"empty string", "", true},
		{"simple string", "test", true},
		{"lock id", "lock-123", true},
		{"unicode", "测试", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := fnv32(tt.input)
			hash2 := fnv32(tt.input)

			if hash1 != hash2 {
				t.Errorf("fnv32(%q) not stable: %d != %d", tt.input, hash1, hash2)
			}

			if tt.input != "" {
				emptyHash := fnv32("")
				if hash1 == emptyHash {
					t.Errorf("fnv32(%q) should not equal empty hash", tt.input)
				}
			}
		})
	}
}

func TestFnv32Distribution(t *testing.T) {
	const numShards = 32
	counts := make(map[uint32]int)

	for i := 0; i < 1000; i++ {
		lockID := uuid.New().String()
		hash := fnv32(lockID)
		shard := hash % numShards
		counts[shard]++
	}

	if len(counts) < numShards/2 {
		t.Errorf("Poor distribution: only %d/%d shards used", len(counts), numShards)
	}

	for shard, count := range counts {
		if count == 0 {
			t.Errorf("Shard %d never used", shard)
		}
	}
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if id1 == id2 {
		t.Error("generateID() produced duplicate IDs")
	}

	if !IsValidOwner(id1) {
		t.Errorf("generateID() produced invalid UUID: %s", id1)
	}

	if !IsValidOwner(id2) {
		t.Errorf("generateID() produced invalid UUID: %s", id2)
	}
}

func TestGenerateIDConcurrent(t *testing.T) {
	const numGoroutines = 100
	ids := make(chan string, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			ids <- generateID()
		}()
	}

	seen := make(map[string]bool)
	for i := 0; i < numGoroutines; i++ {
		id := <-ids
		if seen[id] {
			t.Errorf("Duplicate ID generated: %s", id)
		}
		seen[id] = true

		if !IsValidOwner(id) {
			t.Errorf("Invalid UUID generated: %s", id)
		}
	}
}
