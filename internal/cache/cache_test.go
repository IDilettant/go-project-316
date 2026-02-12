package cache

import (
	"fmt"
	"sync"
	"testing"
)

func TestCacheGetSet(t *testing.T) {
	t.Parallel()

	cache := New[int]()

	if _, ok := cache.Get("missing"); ok {
		t.Fatalf("expected missing key")
	}

	cache.Set("a", 10)
	
	got, ok := cache.Get("a")
	if !ok {
		t.Fatalf("expected existing key")
	}
	
	if got != 10 {
		t.Fatalf("value = %d; want %d", got, 10)
	}
}

func TestCacheConcurrentSet(t *testing.T) {
	t.Parallel()

	cache := New[string]()
	var wg sync.WaitGroup

	for i := range 50 {
		wg.Go(func() {
			key := fmt.Sprintf("k-%d", i)
			value := fmt.Sprintf("v-%d", i)
			cache.Set(key, value)
		})
	}

	wg.Wait()

	for i := range 50 {
		key := fmt.Sprintf("k-%d", i)
		want := fmt.Sprintf("v-%d", i)
		
		got, ok := cache.Get(key)
		if !ok {
			t.Fatalf("missing key %q", key)
		}
		
		if got != want {
			t.Fatalf("value for %q = %q; want %q", key, got, want)
		}
	}
}
