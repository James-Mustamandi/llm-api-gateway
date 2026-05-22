package ratelimit

import (
	"sync"
	"time"
)

type bucket struct {
	mutex		sync.Mutex
	credits 	float64 // current credits
	capacity	float64 // max credits
	refillRate	float64 // credits per second
	lastRefill	time.Time 
}

func newBucket(capacity, refillRate float64, now time.Time) *bucket {
	return &bucket {
		credits:	capacity,
		capacity:	capacity,
		refillRate: refillRate,
		lastRefill: now,
	}
}

func (bucket *bucket) refillLocked(now time.Time) {
	secondsSinceLastRefill := now.Sub(bucket.lastRefill).Seconds()
	if secondsSinceLastRefill <= 0 {
		return
	}
	bucket.credits += secondsSinceLastRefill * bucket.refillRate
	if bucket.credits > bucket.capacity {
		bucket.credits = bucket.capacity
	}
	bucket.lastRefill = now
}


func (bucket *bucket) tryConsume(cost float64, now time.Time) bool {
	bucket.mutex.Lock()
	defer bucket.mutex.Unlock()
	bucket.refillLocked(now)
	
	if bucket.credits < cost {
		return false
	}
	bucket.credits -= cost
	return true
}



// Unconditional consume (allows credits to go negative)
func (bucket *bucket) consume(cost float64, now time.Time) {
	bucket.mutex.Lock()
	defer bucket.mutex.Unlock()
	bucket.refillLocked(now)
	bucket.credits -= cost
}


func (bucket *bucket) creditsAvailable(now time.Time) float64 {
	bucket.mutex.Lock()
	defer bucket.mutex.Unlock()
	bucket.refillLocked(now)
	return bucket.credits
}