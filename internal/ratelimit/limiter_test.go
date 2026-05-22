package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentConsumeNoRace(t *testing.T) {
	const capacity = 1000
	limiter := New(Config{Capacity: capacity, RefillPerSecond: 0})
	currentTime := time.Now()
	limiter.now = func() time.Time { return currentTime }

	const numGoRoutines = 50
	const maxAttempts = 100 // max attempts per goroutine
	
	var allowed int64
	var wg sync.WaitGroup
	for g := 0; g < numGoRoutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < maxAttempts; i++ {
				if limiter.Allow("same-key", 1) {
					atomic.AddInt64(&allowed, 1)
				}
			}
		}()

	}
	wg.Wait()
	if allowed != capacity {
		t.Fatalf("Allowed %d requests, want exactly %d - concurrency accounting is wrong", allowed, capacity)
	}
}

func TestRefill(t *testing.T) {
	limiter := New(Config{Capacity: 100, RefillPerSecond: 10})
	clock := time.Now()
	limiter.now = func() time.Time {return clock}

	if !limiter.Allow("key", 100) {
		t.Fatal("Should allow draining full bucket")
	}

	if limiter.Allow("key", 1) {
		t.Fatal("Bucket should be empty after draining")
	}

	clock = clock.Add(5 * time.Second)
	if !limiter.Allow("key", 50) {
		t.Fatal("Should have refilled 50 credits after 5 seconds")
	}

	if limiter.Allow("key", 1) {
		t.Fatal("Should be empty again after consuming 50 refilled credits")
	}
}

func TestPerKeyIsolation(t *testing.T) {
	limiter := New(Config{Capacity: 100, RefillPerSecond: 0})
	if !limiter.Allow("user1", 100) {
		t.Fatal("user1 should drain their own bucket")
	}

	if limiter.Allow("user1", 1) {
		t.Fatal("user1's bucket should be empty")
	}

	if !limiter.Allow("user2", 100) {
		t.Fatal("user2's bucket should be independent and full")
	}

}