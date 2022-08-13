package cache

import (
	"context"
	"errors"
)

// EvictCallback is used to get a callback when a cache entry is evicted
type EvictCallback[K comparable, V any] func(context.Context, K, V)

// LRU implements a non-thread safe fixed size LRU cache
type LRU[K comparable, V any] struct {
	size      int
	evictList *List[entry[K, V]]
	items     map[K]*Element[entry[K, V]]
	onEvict   EvictCallback[K, V]
}

// entry is used to hold a value in the evictList
type entry[K comparable, V any] struct {
	key   K
	value V
}

// NewLRU constructs an LRU of the given size
func NewLRU[K comparable, V any](size int, onEvict EvictCallback[K, V]) (*LRU[K, V], error) {
	if size <= 0 {
		return nil, errors.New("must provide a positive size")
	}
	c := &LRU[K, V]{
		size:      size,
		evictList: NewList[entry[K, V]](),
		items:     make(map[K]*Element[entry[K, V]]),
		onEvict:   onEvict,
	}
	return c, nil
}

// Purge is used to completely clear the cache.
func (c *LRU[K, V]) Purge(ctx context.Context) {
	for k, v := range c.items {
		if c.onEvict != nil {
			c.onEvict(ctx, k, v.Value.value)
		}
		delete(c.items, k)
	}
	c.evictList.Init()
}

// Add adds a value to the cache.  Returns true if an eviction occurred.
func (c *LRU[K, V]) Add(ctx context.Context, key K, value V) (evicted bool) {
	// Check for existing item
	if ent, ok := c.items[key]; ok {
		c.evictList.MoveToFront(ent)
		ent.Value.value = value
		return false
	}

	// Add new item
	entry := c.evictList.PushFront(entry[K, V]{key, value})
	c.items[key] = entry

	evict := c.evictList.Len() > c.size
	// Verify size not exceeded
	if evict {
		c.removeOldest(ctx)
	}
	return evict
}

// Get looks up a key's value from the cache.
func (c *LRU[K, V]) Get(key K) (value *V, ok bool) {
	if ent, ok := c.items[key]; ok {
		c.evictList.MoveToFront(ent)
		if ent == nil {
			return nil, false
		}
		return &ent.Value.value, true
	}
	return
}

// Contains checks if a key is in the cache, without updating the recent-ness
// or deleting it for being stale.
func (c *LRU[K, V]) Contains(key K) (ok bool) {
	_, ok = c.items[key]
	return ok
}

// Peek returns the key value (or undefined if not found) without updating
// the "recently used"-ness of the key.
func (c *LRU[K, V]) Peek(key K) (value *V, ok bool) {
	if ent, ok := c.items[key]; ok {
		return &ent.Value.value, true
	}
	return nil, false
}

// Remove removes the provided key from the cache, returning if the
// key was contained.
func (c *LRU[K, V]) Remove(ctx context.Context, key K) (present bool) {
	if ent, ok := c.items[key]; ok {
		c.removeElement(ctx, ent)
		return true
	}
	return false
}

// RemoveOldest removes the oldest item from the cache.
func (c *LRU[K, V]) RemoveOldest(ctx context.Context) (key *K, value *V, ok bool) {
	ent := c.evictList.Back()
	if ent != nil {
		c.removeElement(ctx, ent)
		kv := ent.Value
		return &kv.key, &kv.value, true
	}
	return nil, nil, false
}

// GetOldest returns the oldest entry
func (c *LRU[K, V]) GetOldest() (key *K, value *V, ok bool) {
	ent := c.evictList.Back()
	if ent != nil {
		kv := ent.Value
		return &kv.key, &kv.value, true
	}
	return nil, nil, false
}

// Keys returns a slice of the keys in the cache, from oldest to newest.
func (c *LRU[K, V]) Keys() []K {
	keys := make([]K, len(c.items))
	i := 0
	for ent := c.evictList.Back(); ent != nil; ent = ent.Prev() {
		keys[i] = ent.Value.key
		i++
	}
	return keys
}

// Len returns the number of items in the cache.
func (c *LRU[K, V]) Len() int {
	return c.evictList.Len()
}

// Resize changes the cache size.
func (c *LRU[K, V]) Resize(ctx context.Context, size int) (evicted int) {
	diff := c.Len() - size
	if diff < 0 {
		diff = 0
	}
	for i := 0; i < diff; i++ {
		c.removeOldest(ctx)
	}
	c.size = size
	return diff
}

// removeOldest removes the oldest item from the cache.
func (c *LRU[K, V]) removeOldest(ctx context.Context) {
	ent := c.evictList.Back()
	if ent != nil {
		c.removeElement(ctx, ent)
	}
}

// removeElement is used to remove a given list element from the cache
func (c *LRU[K, V]) removeElement(ctx context.Context, e *Element[entry[K, V]]) {
	c.evictList.Remove(e)
	kv := e.Value
	delete(c.items, kv.key)
	if c.onEvict != nil {
		c.onEvict(ctx, kv.key, kv.value)
	}
}
