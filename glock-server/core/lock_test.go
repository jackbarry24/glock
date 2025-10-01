package core

import (
	"testing"
	"time"
)

func TestIsAvailable_NoOwner(t *testing.T) {
	l := &Lock{OwnerID: ""}
	if !l.IsAvailable(time.Now()) {
		t.Fatalf("expected available when no owner")
	}
}

func TestIsAvailable_TTLExpired(t *testing.T) {
	l := &Lock{
		OwnerID:     "owner",
		LastRefresh: time.Now().Add(-2 * time.Second),
		TTL:         time.Second,
	}
	if !l.IsAvailable(time.Now()) {
		t.Fatalf("expected available when TTL expired")
	}
}

func TestIsAvailable_MaxTTLExpired(t *testing.T) {
	l := &Lock{
		OwnerID:    "owner",
		AcquiredAt: time.Now().Add(-2 * time.Second),
		MaxTTL:     time.Second,
	}
	if !l.IsAvailable(time.Now()) {
		t.Fatalf("expected available when MaxTTL expired")
	}
}

func TestIsAvailable_HeldAndNotExpired(t *testing.T) {
	l := &Lock{
		OwnerID:     "owner",
		LastRefresh: time.Now(),
		AcquiredAt:  time.Now(),
		TTL:         time.Minute,
		MaxTTL:      time.Hour,
		Available:   false,
	}
	if l.IsAvailable(time.Now()) {
		t.Fatalf("expected not available when held and not expired")
	}
}

func TestGetOwner_WithOwner(t *testing.T) {
	l := &Lock{
		Owner:   "alice",
		OwnerID: "user123",
	}
	owner := l.GetOwner()
	if owner != "alice" {
		t.Fatalf("expected 'alice', got '%s'", owner)
	}
}

func TestGetOwner_WithOwnerIDOnly(t *testing.T) {
	l := &Lock{
		Owner:   "",
		OwnerID: "user123",
	}
	owner := l.GetOwner()
	if owner != "user123" {
		t.Fatalf("expected 'user123', got '%s'", owner)
	}
}

func TestGetOwner_NoOwner(t *testing.T) {
	l := &Lock{
		Owner:   "",
		OwnerID: "",
	}
	owner := l.GetOwner()
	if owner != "none" {
		t.Fatalf("expected 'none', got '%s'", owner)
	}
}
