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

func (n *Node) IsAnyParentHeld(now time.Time) bool {
	current := n.Parent
	for current != nil && current.Lock != nil {
		if !current.Lock.IsAvailable(now) {
			return true
		}
		current = current.Parent
	}
	return false
}

func (n *Node) IsParentHeld(now time.Time) bool {
	if n.Parent != nil && n.Parent.Lock != nil {
		return !n.Parent.Lock.IsAvailable(now)
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
