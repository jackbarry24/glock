package core

import (
	"net/http"
	"testing"
	"time"
)

func TestNewLockTree(t *testing.T) {
	tree := NewLockTree()
	if tree == nil {
		t.Fatal("NewLockTree returned nil")
	}
	if tree.Root == nil {
		t.Fatal("tree root is nil")
	}
	if tree.Root.Children == nil {
		t.Fatal("tree root children map is nil")
	}
}

func TestNodeCreation(t *testing.T) {
	tree := NewLockTree()
	lock := &Lock{Name: "test-lock"}

	node := NewNode(tree.Root, lock)
	if node == nil {
		t.Fatal("NewNode returned nil")
	}
	if node.Name != "test-lock" {
		t.Fatalf("expected node name 'test-lock', got '%s'", node.Name)
	}
	if node.Parent != tree.Root {
		t.Fatal("node parent not set correctly")
	}
	if node.Lock != lock {
		t.Fatal("node lock not set correctly")
	}
}

func TestNodeAddChild(t *testing.T) {
	tree := NewLockTree()
	parentLock := &Lock{Name: "parent"}
	parentNode := NewNode(tree.Root, parentLock)

	childLock := &Lock{Name: "child"}
	childNode := parentNode.AddChild(childLock)

	if childNode.Parent != parentNode {
		t.Fatal("child parent not set correctly")
	}
	if parentNode.Children["child"] != childNode {
		t.Fatal("child not added to parent's children map")
	}
}

func TestNodeRemove(t *testing.T) {
	tree := NewLockTree()
	parentLock := &Lock{Name: "parent"}
	parentNode := NewNode(tree.Root, parentLock)

	childLock := &Lock{Name: "child"}
	childNode := parentNode.AddChild(childLock)

	grandchildLock := &Lock{Name: "grandchild"}
	grandchildNode := childNode.AddChild(grandchildLock)

	childNode.Remove()

	if parentNode.Children["child"] != nil {
		t.Fatal("child should be removed from parent")
	}
	if grandchildNode.Parent != parentNode {
		t.Fatal("grandchild should be reparented to parent")
	}
	if parentNode.Children["grandchild"] != grandchildNode {
		t.Fatal("grandchild should be in parent's children")
	}
}

func TestNodeChangeParent(t *testing.T) {
	tree := NewLockTree()
	parent1Lock := &Lock{Name: "parent1"}
	parent1Node := NewNode(tree.Root, parent1Lock)

	parent2Lock := &Lock{Name: "parent2"}
	parent2Node := NewNode(tree.Root, parent2Lock)

	childLock := &Lock{Name: "child"}
	childNode := parent1Node.AddChild(childLock)

	childNode.ChangeParent(parent2Node)

	if childNode.Parent != parent2Node {
		t.Fatal("child parent not changed")
	}
	if parent1Node.Children["child"] != nil {
		t.Fatal("child should be removed from old parent")
	}
	if parent2Node.Children["child"] != childNode {
		t.Fatal("child should be added to new parent")
	}
}

func TestIsParentHeld(t *testing.T) {
	tree := NewLockTree()
	parentLock := &Lock{
		Name:        "parent",
		OwnerID:     "owner1",
		AcquiredAt:  time.Now(),
		LastRefresh: time.Now(),
		TTL:         time.Minute,
		MaxTTL:      time.Hour,
		Available:   false,
	}
	parentLock.isHeld.Store(true) // Set atomic flag for manual lock creation
	parentNode := NewNode(tree.Root, parentLock)

	childLock := &Lock{Name: "child"}
	childNode := parentNode.AddChild(childLock)

	now := time.Now()
	if !childNode.IsParentHeld(now) {
		t.Fatal("expected parent to be held")
	}

	parentLock.Available = true
	parentLock.OwnerID = ""
	parentLock.isHeld.Store(false) // Clear atomic flag
	if childNode.IsParentHeld(now) {
		t.Fatal("expected parent not to be held")
	}
}

func TestIsParentHeldWithNoParentLock(t *testing.T) {
	tree := NewLockTree()
	lock := &Lock{Name: "root-lock"}
	node := NewNode(tree.Root, lock)

	now := time.Now()
	if node.IsParentHeld(now) {
		t.Fatal("root node should not have held parent")
	}
}

func TestIsAnyParentHeld(t *testing.T) {
	tree := NewLockTree()

	grandparentLock := &Lock{
		Name:        "grandparent",
		OwnerID:     "owner1",
		AcquiredAt:  time.Now(),
		LastRefresh: time.Now(),
		TTL:         time.Minute,
		MaxTTL:      time.Hour,
		Available:   false,
	}
	grandparentLock.isHeld.Store(true) // Set atomic flag for manual lock creation
	grandparentNode := NewNode(tree.Root, grandparentLock)

	parentLock := &Lock{Name: "parent"}
	parentNode := grandparentNode.AddChild(parentLock)

	childLock := &Lock{Name: "child"}
	childNode := parentNode.AddChild(childLock)

	now := time.Now()
	if !childNode.IsAnyParentHeld(now) {
		t.Fatal("expected ancestor (grandparent) to be held")
	}

	grandparentLock.Available = true
	grandparentLock.OwnerID = ""
	grandparentLock.isHeld.Store(false) // Clear atomic flag
	if childNode.IsAnyParentHeld(now) {
		t.Fatal("expected no ancestors to be held")
	}
}

func TestIsAnyParentHeldMultipleLevels(t *testing.T) {
	tree := NewLockTree()

	level1Lock := &Lock{Name: "level1"}
	level1Node := NewNode(tree.Root, level1Lock)

	level2Lock := &Lock{
		Name:        "level2",
		OwnerID:     "owner2",
		AcquiredAt:  time.Now(),
		LastRefresh: time.Now(),
		TTL:         time.Minute,
		MaxTTL:      time.Hour,
		Available:   false,
	}
	level2Lock.isHeld.Store(true) // Set atomic flag for manual lock creation
	level2Node := level1Node.AddChild(level2Lock)

	level3Lock := &Lock{Name: "level3"}
	level3Node := level2Node.AddChild(level3Lock)

	level4Lock := &Lock{Name: "level4"}
	level4Node := level3Node.AddChild(level4Lock)

	now := time.Now()
	if !level4Node.IsAnyParentHeld(now) {
		t.Fatal("expected level2 ancestor to be held")
	}
}

func TestParentChildAcquireBlocking(t *testing.T) {
	g := newServer(10)

	_, _, _ = g.CreateLock(&CreateRequest{Name: "parent", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.CreateLock(&CreateRequest{Name: "child", Parent: "parent", TTL: "1s", MaxTTL: "1m"})

	result, code, err := g.AcquireLock(&AcquireRequest{
		Name:    "parent",
		Owner:   "owner1",
		OwnerID: "11111111-1111-1111-1111-111111111111",
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("parent acquire failed: code=%d err=%v", code, err)
	}
	parentLock := result.(*Lock)

	_, code, err = g.AcquireLock(&AcquireRequest{
		Name:    "child",
		Owner:   "owner2",
		OwnerID: "22222222-2222-2222-2222-222222222222",
	})
	if err == nil || code != http.StatusConflict {
		t.Fatalf("expected conflict when acquiring child with parent held, got code=%d err=%v", code, err)
	}
	// When queue is disabled (default), hierarchy constraints result in "lock is held by another owner"
	if err.Error() != "lock is held by another owner" {
		t.Fatalf("unexpected error message: %s", err.Error())
	}

	_, code, err = g.ReleaseLock(&ReleaseRequest{
		Name:    "parent",
		OwnerID: "11111111-1111-1111-1111-111111111111",
		Token:   parentLock.Token,
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("parent release failed: code=%d err=%v", code, err)
	}

	_, code, err = g.AcquireLock(&AcquireRequest{
		Name:    "child",
		Owner:   "owner2",
		OwnerID: "22222222-2222-2222-2222-222222222222",
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("child acquire should succeed after parent release, got code=%d err=%v", code, err)
	}
}

func TestParentChildMultiLevel(t *testing.T) {
	g := newServer(10)

	_, _, _ = g.CreateLock(&CreateRequest{Name: "grandparent", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.CreateLock(&CreateRequest{Name: "parent", Parent: "grandparent", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.CreateLock(&CreateRequest{Name: "child", Parent: "parent", TTL: "1s", MaxTTL: "1m"})

	result, code, err := g.AcquireLock(&AcquireRequest{
		Name:    "grandparent",
		Owner:   "owner1",
		OwnerID: "11111111-1111-1111-1111-111111111111",
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("grandparent acquire failed: code=%d err=%v", code, err)
	}
	grandparentLock := result.(*Lock)

	_, code, err = g.AcquireLock(&AcquireRequest{
		Name:    "child",
		Owner:   "owner2",
		OwnerID: "22222222-2222-2222-2222-222222222222",
	})
	if err == nil || code != http.StatusConflict {
		t.Fatalf("expected conflict when acquiring child with grandparent held, got code=%d", code)
	}

	g.ReleaseLock(&ReleaseRequest{
		Name:    "grandparent",
		OwnerID: "11111111-1111-1111-1111-111111111111",
		Token:   grandparentLock.Token,
	})

	_, code, err = g.AcquireLock(&AcquireRequest{
		Name:    "child",
		Owner:   "owner2",
		OwnerID: "22222222-2222-2222-2222-222222222222",
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("child acquire should succeed after grandparent release, got code=%d err=%v", code, err)
	}
}

func TestDeleteWithParentHeld(t *testing.T) {
	g := newServer(10)

	_, _, _ = g.CreateLock(&CreateRequest{Name: "parent", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.CreateLock(&CreateRequest{Name: "child", Parent: "parent", TTL: "1s", MaxTTL: "1m"})

	result, _, _ := g.AcquireLock(&AcquireRequest{
		Name:    "parent",
		Owner:   "owner1",
		OwnerID: "11111111-1111-1111-1111-111111111111",
	})
	parentLock := result.(*Lock)

	_, code, err := g.DeleteLock("child")
	if err == nil || code != http.StatusConflict {
		t.Fatalf("expected conflict when deleting child with parent held, got code=%d err=%v", code, err)
	}
	if err.Error() != "cannot delete lock: parent lock is currently held" {
		t.Fatalf("unexpected error message: %s", err.Error())
	}

	g.ReleaseLock(&ReleaseRequest{
		Name:    "parent",
		OwnerID: "11111111-1111-1111-1111-111111111111",
		Token:   parentLock.Token,
	})

	ok, code, err := g.DeleteLock("child")
	if err != nil || code != http.StatusOK || !ok {
		t.Fatalf("child delete should succeed after parent release, got code=%d err=%v ok=%v", code, err, ok)
	}
}

func TestUpdateParentWithAncestorHeld(t *testing.T) {
	g := newServer(10)

	_, _, _ = g.CreateLock(&CreateRequest{Name: "parent1", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.CreateLock(&CreateRequest{Name: "parent2", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.CreateLock(&CreateRequest{Name: "grandparent", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.CreateLock(&CreateRequest{Name: "child", Parent: "parent1", TTL: "1s", MaxTTL: "1m"})

	result, _, _ := g.AcquireLock(&AcquireRequest{
		Name:    "grandparent",
		Owner:   "owner1",
		OwnerID: "11111111-1111-1111-1111-111111111111",
	})
	grandparentLock := result.(*Lock)

	_, _, _ = g.UpdateLock(&UpdateRequest{
		Name:   "parent2",
		Parent: "grandparent",
		TTL:    "2s",
		MaxTTL: "2m",
	})

	_, code, err := g.UpdateLock(&UpdateRequest{
		Name:   "child",
		Parent: "parent2",
		TTL:    "2s",
		MaxTTL: "2m",
	})
	if err == nil || code != http.StatusConflict {
		t.Fatalf("expected conflict when changing parent to one with held ancestor, got code=%d err=%v", code, err)
	}
	if err.Error() != "cannot change parent: an ancestor lock is currently held" {
		t.Fatalf("unexpected error message: %s", err.Error())
	}

	g.ReleaseLock(&ReleaseRequest{
		Name:    "grandparent",
		OwnerID: "11111111-1111-1111-1111-111111111111",
		Token:   grandparentLock.Token,
	})

	_, code, err = g.UpdateLock(&UpdateRequest{
		Name:   "child",
		Parent: "parent2",
		TTL:    "2s",
		MaxTTL: "2m",
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("update should succeed after grandparent release, got code=%d err=%v", code, err)
	}
}

func TestChildCanBeAcquiredIndependently(t *testing.T) {
	g := newServer(10)

	_, _, _ = g.CreateLock(&CreateRequest{Name: "parent", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.CreateLock(&CreateRequest{Name: "child", Parent: "parent", TTL: "1s", MaxTTL: "1m"})

	result, code, err := g.AcquireLock(&AcquireRequest{
		Name:    "child",
		Owner:   "owner1",
		OwnerID: "11111111-1111-1111-1111-111111111111",
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("child acquire should succeed when parent not held, got code=%d err=%v", code, err)
	}
	childLock := result.(*Lock)

	// With bidirectional blocking, parent should NOT be acquirable when child is held
	_, code, err = g.AcquireLock(&AcquireRequest{
		Name:    "parent",
		Owner:   "owner2",
		OwnerID: "22222222-2222-2222-2222-222222222222",
	})
	if err == nil || code != http.StatusConflict {
		t.Fatalf("parent acquire should fail when child is held (bidirectional blocking), got code=%d err=%v", code, err)
	}

	// Release child, now parent should be acquirable
	g.ReleaseLock(&ReleaseRequest{
		Name:    "child",
		OwnerID: "11111111-1111-1111-1111-111111111111",
		Token:   childLock.Token,
	})

	result, code, err = g.AcquireLock(&AcquireRequest{
		Name:    "parent",
		Owner:   "owner2",
		OwnerID: "22222222-2222-2222-2222-222222222222",
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("parent acquire should succeed after child is released, got code=%d err=%v", code, err)
	}
	parentLock := result.(*Lock)

	g.ReleaseLock(&ReleaseRequest{
		Name:    "parent",
		OwnerID: "22222222-2222-2222-2222-222222222222",
		Token:   parentLock.Token,
	})
}

func TestParentChildWithTTLExpiration(t *testing.T) {
	g := newServer(10)

	_, _, _ = g.CreateLock(&CreateRequest{Name: "parent", TTL: "50ms", MaxTTL: "1s"})
	_, _, _ = g.CreateLock(&CreateRequest{Name: "child", Parent: "parent", TTL: "1s", MaxTTL: "1m"})

	result, _, _ := g.AcquireLock(&AcquireRequest{
		Name:    "parent",
		Owner:   "owner1",
		OwnerID: "11111111-1111-1111-1111-111111111111",
	})
	parentLock := result.(*Lock)

	_, code, err := g.AcquireLock(&AcquireRequest{
		Name:    "child",
		Owner:   "owner2",
		OwnerID: "22222222-2222-2222-2222-222222222222",
	})
	if err == nil || code != http.StatusConflict {
		t.Fatalf("expected conflict when parent held, got code=%d", code)
	}

	time.Sleep(100 * time.Millisecond)

	_, code, err = g.AcquireLock(&AcquireRequest{
		Name:    "child",
		Owner:   "owner2",
		OwnerID: "22222222-2222-2222-2222-222222222222",
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("child acquire should succeed after parent TTL expired, got code=%d err=%v", code, err)
	}

	_, code, err = g.RefreshLock(&RefreshRequest{
		Name:    "parent",
		OwnerID: "11111111-1111-1111-1111-111111111111",
		Token:   parentLock.Token,
	})
	if err == nil || code != http.StatusConflict {
		t.Fatalf("parent refresh should fail after TTL expiration, got code=%d", code)
	}
}

func TestCreateChildWithNonExistentParent(t *testing.T) {
	g := newServer(10)

	_, code, err := g.CreateLock(&CreateRequest{
		Name:   "child",
		Parent: "non-existent-parent",
		TTL:    "1s",
		MaxTTL: "1m",
	})
	if err == nil || code != http.StatusBadRequest {
		t.Fatalf("expected bad request when creating child with non-existent parent, got code=%d err=%v", code, err)
	}
}

func TestUpdateToNonExistentParent(t *testing.T) {
	g := newServer(10)

	_, _, _ = g.CreateLock(&CreateRequest{Name: "child", TTL: "1s", MaxTTL: "1m"})

	_, code, err := g.UpdateLock(&UpdateRequest{
		Name:   "child",
		Parent: "non-existent-parent",
		TTL:    "2s",
		MaxTTL: "2m",
	})
	if err == nil || code != http.StatusBadRequest {
		t.Fatalf("expected bad request when updating to non-existent parent, got code=%d err=%v", code, err)
	}
}

func TestComplexHierarchy(t *testing.T) {
	g := newServer(20)

	_, _, _ = g.CreateLock(&CreateRequest{Name: "root", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.CreateLock(&CreateRequest{Name: "branch1", Parent: "root", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.CreateLock(&CreateRequest{Name: "branch2", Parent: "root", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.CreateLock(&CreateRequest{Name: "leaf1-1", Parent: "branch1", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.CreateLock(&CreateRequest{Name: "leaf1-2", Parent: "branch1", TTL: "1s", MaxTTL: "1m"})
	_, _, _ = g.CreateLock(&CreateRequest{Name: "leaf2-1", Parent: "branch2", TTL: "1s", MaxTTL: "1m"})

	result, code, err := g.AcquireLock(&AcquireRequest{
		Name:    "root",
		Owner:   "owner1",
		OwnerID: "11111111-1111-1111-1111-111111111111",
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("root acquire failed")
	}
	rootLock := result.(*Lock)

	_, code, _ = g.AcquireLock(&AcquireRequest{
		Name:    "leaf1-1",
		Owner:   "owner2",
		OwnerID: "22222222-2222-2222-2222-222222222222",
	})
	if code == http.StatusOK {
		t.Fatal("leaf1-1 should not be acquirable when root is held")
	}

	_, code, _ = g.AcquireLock(&AcquireRequest{
		Name:    "leaf2-1",
		Owner:   "owner3",
		OwnerID: "33333333-3333-3333-3333-333333333333",
	})
	if code == http.StatusOK {
		t.Fatal("leaf2-1 should not be acquirable when root is held")
	}

	g.ReleaseLock(&ReleaseRequest{
		Name:    "root",
		OwnerID: "11111111-1111-1111-1111-111111111111",
		Token:   rootLock.Token,
	})

	_, code, err = g.AcquireLock(&AcquireRequest{
		Name:    "leaf1-1",
		Owner:   "owner2",
		OwnerID: "22222222-2222-2222-2222-222222222222",
	})
	if err != nil || code != http.StatusOK {
		t.Fatal("leaf1-1 should be acquirable after root release")
	}

	_, code, err = g.AcquireLock(&AcquireRequest{
		Name:    "leaf2-1",
		Owner:   "owner3",
		OwnerID: "33333333-3333-3333-3333-333333333333",
	})
	if err != nil || code != http.StatusOK {
		t.Fatal("leaf2-1 should be acquirable after root release")
	}
}

func TestParentChildQueueingWhenAncestorHeld(t *testing.T) {
	g := newServer(10)

	// Create parent with FIFO queue enabled
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "parent",
		TTL:          "1s",
		MaxTTL:       "1m",
		QueueType:    QueueFIFO,
		QueueTimeout: "1m",
	})
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "child",
		Parent:       "parent",
		TTL:          "1s",
		MaxTTL:       "1m",
		QueueType:    QueueFIFO,
		QueueTimeout: "1m",
	})

	// Acquire parent
	result, code, err := g.AcquireLock(&AcquireRequest{
		Name:    "parent",
		Owner:   "owner1",
		OwnerID: "11111111-1111-1111-1111-111111111111",
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("parent acquire failed: code=%d err=%v", code, err)
	}
	parentLock := result.(*Lock)

	// Try to acquire child - should queue because parent is held
	result, code, err = g.AcquireLock(&AcquireRequest{
		Name:    "child",
		Owner:   "owner2",
		OwnerID: "22222222-2222-2222-2222-222222222222",
	})
	if code != http.StatusAccepted {
		t.Fatalf("expected child to be queued (202), got code=%d err=%v", code, err)
	}
	queueResp := result.(*QueueResponse)
	if queueResp.Position != 1 {
		t.Fatalf("expected position 1, got %d", queueResp.Position)
	}

	// Release parent - child should automatically get lock from queue
	_, code, err = g.ReleaseLock(&ReleaseRequest{
		Name:    "parent",
		OwnerID: "11111111-1111-1111-1111-111111111111",
		Token:   parentLock.Token,
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("parent release failed: code=%d err=%v", code, err)
	}

	// Check that child was granted from queue
	nodeVal, _ := g.Locks.Load("child")
	childLock := nodeVal.(*Node).Lock
	childLock.mu.Lock()
	if childLock.OwnerID != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("child should have been granted from queue, but owner is: %s", childLock.OwnerID)
	}
	childLock.mu.Unlock()
}

func TestParentChildQueueingWhenDescendantHeld(t *testing.T) {
	g := newServer(10)

	// Create parent with FIFO queue enabled
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "parent",
		TTL:          "1s",
		MaxTTL:       "1m",
		QueueType:    QueueFIFO,
		QueueTimeout: "1m",
	})
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "child",
		Parent:       "parent",
		TTL:          "1s",
		MaxTTL:       "1m",
		QueueType:    QueueFIFO,
		QueueTimeout: "1m",
	})

	// Acquire child first
	result, code, err := g.AcquireLock(&AcquireRequest{
		Name:    "child",
		Owner:   "owner1",
		OwnerID: "11111111-1111-1111-1111-111111111111",
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("child acquire failed: code=%d err=%v", code, err)
	}
	childLock := result.(*Lock)

	// Try to acquire parent - should queue because child is held
	result, code, err = g.AcquireLock(&AcquireRequest{
		Name:    "parent",
		Owner:   "owner2",
		OwnerID: "22222222-2222-2222-2222-222222222222",
	})
	if code != http.StatusAccepted {
		t.Fatalf("expected parent to be queued (202), got code=%d err=%v", code, err)
	}
	queueResp := result.(*QueueResponse)
	if queueResp.Position != 1 {
		t.Fatalf("expected position 1, got %d", queueResp.Position)
	}

	// Release child - parent should automatically get lock from queue
	_, code, err = g.ReleaseLock(&ReleaseRequest{
		Name:    "child",
		OwnerID: "11111111-1111-1111-1111-111111111111",
		Token:   childLock.Token,
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("child release failed: code=%d err=%v", code, err)
	}

	// Check that parent was granted from queue
	nodeVal, _ := g.Locks.Load("parent")
	parentLock := nodeVal.(*Node).Lock
	parentLock.mu.Lock()
	if parentLock.OwnerID != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("parent should have been granted from queue, but owner is: %s", parentLock.OwnerID)
	}
	parentLock.mu.Unlock()
}

func TestParentChildMultiLevelQueueing(t *testing.T) {
	g := newServer(10)

	// Create 3-level hierarchy all with queues
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "root",
		TTL:          "1s",
		MaxTTL:       "1m",
		QueueType:    QueueFIFO,
		QueueTimeout: "1m",
	})
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "mid",
		Parent:       "root",
		TTL:          "1s",
		MaxTTL:       "1m",
		QueueType:    QueueFIFO,
		QueueTimeout: "1m",
	})
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:         "leaf",
		Parent:       "mid",
		TTL:          "1s",
		MaxTTL:       "1m",
		QueueType:    QueueFIFO,
		QueueTimeout: "1m",
	})

	// Acquire root
	result, code, err := g.AcquireLock(&AcquireRequest{
		Name:    "root",
		Owner:   "owner1",
		OwnerID: "11111111-1111-1111-1111-111111111111",
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("root acquire failed: code=%d err=%v", code, err)
	}
	rootLock := result.(*Lock)

	// Try to acquire mid - should queue
	_, code, _ = g.AcquireLock(&AcquireRequest{
		Name:    "mid",
		Owner:   "owner2",
		OwnerID: "22222222-2222-2222-2222-222222222222",
	})
	if code != http.StatusAccepted {
		t.Fatalf("expected mid to be queued, got code=%d", code)
	}

	// Try to acquire leaf - should also queue
	_, code, _ = g.AcquireLock(&AcquireRequest{
		Name:    "leaf",
		Owner:   "owner3",
		OwnerID: "33333333-3333-3333-3333-333333333333",
	})
	if code != http.StatusAccepted {
		t.Fatalf("expected leaf to be queued, got code=%d", code)
	}

	// Release root
	_, code, err = g.ReleaseLock(&ReleaseRequest{
		Name:    "root",
		OwnerID: "11111111-1111-1111-1111-111111111111",
		Token:   rootLock.Token,
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("root release failed: code=%d err=%v", code, err)
	}

	// Mid should now be granted
	nodeVal, _ := g.Locks.Load("mid")
	midLock := nodeVal.(*Node).Lock
	midLock.mu.Lock()
	midOwner := midLock.OwnerID
	midToken := midLock.Token
	midLock.mu.Unlock()

	if midOwner != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("mid should have been granted from queue, but owner is: %s", midOwner)
	}

	// Leaf should still be queued (mid is now held)
	nodeVal, _ = g.Locks.Load("leaf")
	leafLock := nodeVal.(*Node).Lock
	leafLock.mu.Lock()
	if leafLock.OwnerID == "33333333-3333-3333-3333-333333333333" {
		t.Fatal("leaf should NOT have been granted yet (mid is held)")
	}
	leafLock.mu.Unlock()

	// Release mid
	_, code, err = g.ReleaseLock(&ReleaseRequest{
		Name:    "mid",
		OwnerID: "22222222-2222-2222-2222-222222222222",
		Token:   midToken,
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("mid release failed: code=%d err=%v", code, err)
	}

	// Now leaf should be granted
	leafLock.mu.Lock()
	if leafLock.OwnerID != "33333333-3333-3333-3333-333333333333" {
		t.Fatalf("leaf should have been granted from queue, but owner is: %s", leafLock.OwnerID)
	}
	leafLock.mu.Unlock()
}

func TestQueueingDisabledWithHierarchy(t *testing.T) {
	g := newServer(10)

	// Create parent and child with NO queue
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:      "parent",
		TTL:       "1s",
		MaxTTL:    "1m",
		QueueType: QueueNone,
	})
	_, _, _ = g.CreateLock(&CreateRequest{
		Name:      "child",
		Parent:    "parent",
		TTL:       "1s",
		MaxTTL:    "1m",
		QueueType: QueueNone,
	})

	// Acquire parent
	_, code, err := g.AcquireLock(&AcquireRequest{
		Name:    "parent",
		Owner:   "owner1",
		OwnerID: "11111111-1111-1111-1111-111111111111",
	})
	if err != nil || code != http.StatusOK {
		t.Fatalf("parent acquire failed: code=%d err=%v", code, err)
	}

	// Try to acquire child - should fail immediately (no queue)
	_, code, err = g.AcquireLock(&AcquireRequest{
		Name:    "child",
		Owner:   "owner2",
		OwnerID: "22222222-2222-2222-2222-222222222222",
	})
	if code != http.StatusConflict {
		t.Fatalf("expected conflict (409) when queue disabled and parent held, got code=%d", code)
	}
	if err == nil || err.Error() != "lock is held by another owner" {
		t.Fatalf("expected 'lock is held by another owner' error, got: %v", err)
	}
}
