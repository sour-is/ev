package cache_test

import (
	"context"
	"testing"

	"github.com/matryer/is"
	"go.sour.is/ev/pkg/cache"
)

func TestCache(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	c, err := cache.NewCache[string, int](1)
	is.NoErr(err)

	evicted := c.Add(ctx, "one", 1)
	is.True(!evicted)

	is.True(c.Contains("one"))
	_, ok := c.Peek("one")
	is.True(ok)

	ok, evicted = c.ContainsOrAdd(ctx, "two", 2)
	is.True(!ok)
	is.True(evicted)

	is.True(!c.Contains("one"))
	is.True(c.Contains("two"))

	is.Equal(c.Len(), 1)
	is.Equal(c.Keys(), []string{"two"})

	v, ok := c.Get("two")
	is.True(ok)
	is.Equal(*v, 2)

	evictCount := c.Resize(ctx, 100)
	is.True(evictCount == 0)

	c.Add(ctx, "one", 1)

	prev, ok, evicted := c.PeekOrAdd(ctx, "three", 3)
	is.True(!ok)
	is.True(!evicted)
	is.Equal(prev, nil)

	key, value, ok := c.GetOldest()
	is.True(ok)
	is.Equal(*key, "two")
	is.Equal(*value, 2)

	key, value, ok = c.RemoveOldest(ctx)
	is.True(ok)
	is.Equal(*key, "two")
	is.Equal(*value, 2)

	c.Remove(ctx, "one")

	c.Purge(ctx)
	is.True(!c.Contains("three"))
}

func TestCacheWithEvict(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	evictions := 0

	c, err := cache.NewWithEvict(1, func(ctx context.Context, s string, i int) { evictions++ })
	is.NoErr(err)

	key, value, ok := c.GetOldest()
	is.True(!ok)
	is.Equal(key, nil)
	is.Equal(value, nil)

	key, value, ok = c.RemoveOldest(ctx)
	is.True(!ok)
	is.Equal(key, nil)
	is.Equal(value, nil)

	evicted := c.Add(ctx, "one", 1)
	is.True(!evicted)

	is.True(c.Contains("one"))
	_, ok = c.Peek("one")
	is.True(ok)

	ok, evicted = c.ContainsOrAdd(ctx, "two", 2)
	is.True(!ok)
	is.True(evicted)

	is.True(!c.Contains("one"))
	is.True(c.Contains("two"))

	is.Equal(c.Len(), 1)
	is.Equal(c.Keys(), []string{"two"})

	v, ok := c.Get("two")
	is.True(ok)
	is.Equal(*v, 2)

	evictCount := c.Resize(ctx, 100)
	is.True(evictCount == 0)

	c.Add(ctx, "one", 1)

	prev, ok, evicted := c.PeekOrAdd(ctx, "three", 3)
	is.True(!ok)
	is.True(!evicted)
	is.Equal(prev, nil)

	key, value, ok = c.GetOldest()
	is.True(ok)
	is.Equal(*key, "two")
	is.Equal(*value, 2)

	key, value, ok = c.RemoveOldest(ctx)
	is.True(ok)
	is.Equal(*key, "two")
	is.Equal(*value, 2)

	c.Resize(ctx, 1)

	c.Purge(ctx)
	is.True(!c.Contains("three"))

	is.Equal(evictions, 4)
}
