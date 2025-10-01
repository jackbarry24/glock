package core

import (
	"sync/atomic"
	"time"
)

type LockTree struct {
	Root *Node
}

func NewLockTree() *LockTree {
	return &LockTree{
		Root: &Node{
			Children: make(map[string]*Node),
		},
	}
}

type Node struct {
	Name                 string
	Parent               *Node
	Children             map[string]*Node
	Lock                 *Lock
	heldDescendantsCount atomic.Int32 // Count of held locks in subtree
}

func NewNode(parent *Node, lock *Lock) *Node {
	n := &Node{
		Name:     lock.Name,
		Parent:   parent,
		Children: make(map[string]*Node),
		Lock:     lock,
	}
	if parent != nil {
		parent.Children[lock.Name] = n
	}
	return n
}

func (n *Node) AddChild(lock *Lock) *Node {
	child := NewNode(n, lock)
	n.Children[lock.Name] = child
	return child
}

func (n *Node) Remove() {
	if n.Parent != nil {
		for _, child := range n.Children {
			child.Parent = n.Parent
			n.Parent.Children[child.Name] = child
		}
		delete(n.Parent.Children, n.Name)
	}
}

func (n *Node) ChangeParent(newParent *Node) {
	if n.Parent != nil {
		delete(n.Parent.Children, n.Name)
	}
	n.Parent = newParent
	newParent.Children[n.Name] = n
}

// IsAnyParentHeld walks up the tree and checks if any parent lock is held.
// Uses atomic flag for race-free checking, with opportunistic expiration detection.
// If we can acquire a parent's lock, we check for expiration and clear the flag if needed.
func (n *Node) IsAnyParentHeld(now time.Time) bool {
	current := n.Parent
	for current != nil && current.Lock != nil {
		if current.Lock.isHeld.Load() {
			// Opportunistically try to clear expired locks without blocking
			if current.Lock.mu.TryLock() {
				if current.Lock.IsAvailable(now) {
					current.Lock.isHeld.Store(false)
					current.Lock.mu.Unlock()
					// Continue checking other parents
					current = current.Parent
					continue
				}
				current.Lock.mu.Unlock()
				// Lock is held and not expired
				return true
			}
			// Couldn't acquire lock - conservatively assume it's held
			return true
		}
		current = current.Parent
	}
	return false
}

// IsParentHeld checks if the immediate parent lock is held.
// Uses atomic flag for race-free checking.
func (n *Node) IsParentHeld(now time.Time) bool {
	if n.Parent != nil && n.Parent.Lock != nil {
		return n.Parent.Lock.isHeld.Load()
	}
	return false
}

// HasHeldDescendants checks if any descendant lock is currently held
func (n *Node) HasHeldDescendants() bool {
	return n.heldDescendantsCount.Load() > 0
}

// IncrementAncestorCounts increments the held descendants count for all ancestors
func (n *Node) IncrementAncestorCounts() {
	current := n.Parent
	for current != nil {
		current.heldDescendantsCount.Add(1)
		current = current.Parent
	}
}

// DecrementAncestorCounts decrements the held descendants count for all ancestors
func (n *Node) DecrementAncestorCounts() {
	current := n.Parent
	for current != nil {
		current.heldDescendantsCount.Add(-1)
		current = current.Parent
	}
}

// WouldCreateCycle checks if setting newParent as the parent of this node would create a cycle.
// This is done by walking up from newParent and checking if we reach this node.
func (n *Node) WouldCreateCycle(newParent *Node) bool {
	if newParent == nil {
		return false
	}

	// Walk up from newParent to see if we reach this node
	current := newParent
	for current != nil {
		if current == n {
			return true
		}
		current = current.Parent
	}
	return false
}

// GetAllDescendants returns all descendant nodes recursively using DFS.
// Pre-allocates slice for efficiency assuming average branching factor.
func (n *Node) GetAllDescendants() []*Node {
	if len(n.Children) == 0 {
		return nil
	}

	// Pre-allocate with estimated size (branching factor * depth estimate)
	result := make([]*Node, 0, len(n.Children)*4)
	n.collectDescendants(&result)
	return result
}

// collectDescendants is a helper for recursive DFS traversal
func (n *Node) collectDescendants(result *[]*Node) {
	for _, child := range n.Children {
		*result = append(*result, child)
		if len(child.Children) > 0 {
			child.collectDescendants(result)
		}
	}
}

// GetAncestors returns all ancestor nodes from parent to root.
// Returns nil if node has no parent.
func (n *Node) GetAncestors() []*Node {
	if n.Parent == nil {
		return nil
	}

	// Pre-allocate assuming typical depth (most hierarchies are shallow)
	result := make([]*Node, 0, 8)
	current := n.Parent
	for current != nil {
		result = append(result, current)
		current = current.Parent
	}
	return result
}

// GetImmediateFamily returns parent and all direct children in a single slice.
// Optimized for the common case of checking related nodes.
func (n *Node) GetImmediateFamily() []*Node {
	familySize := len(n.Children)
	if n.Parent != nil {
		familySize++
	}

	if familySize == 0 {
		return nil
	}

	result := make([]*Node, 0, familySize)
	if n.Parent != nil {
		result = append(result, n.Parent)
	}
	for _, child := range n.Children {
		result = append(result, child)
	}
	return result
}
