package ratelimit

import (
	"sync"
	"time"
)

type Limiter struct {
	mutex 		sync.Mutex
	buckets		map[string]*bucket
	capacity	float64
	refillRate	float64
	now			func() time.Time 
}

type Config struct {
	Capacity		float64
	RefillPerSecond	float64
}

func New(config Config) *Limiter {
	return &Limiter{
		buckets:	make(map[string]*bucket),
		capacity: 	config.Capacity,
		refillRate: config.RefillPerSecond,
		now:		time.Now,
	}
}

func (limiter *Limiter) getBucket(key string) *bucket {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()
	bucket, ok := limiter.buckets[key]
	if !ok {
		bucket = newBucket(limiter.capacity, limiter.refillRate, limiter.now())
		limiter.buckets[key] = bucket
	}
	return bucket
}

func (limiter *Limiter) Allow(key string, cost float64) bool {
	bucket := limiter.getBucket(key)
	return bucket.tryConsume(cost, limiter.now())
}

func (limiter *Limiter) Charge(key string, cost float64) {
	bucket := limiter.getBucket(key)
	bucket.consume(cost, limiter.now())
}

func (limiter *Limiter) creditsAvailable(key string) float64{
	bucket := limiter.getBucket(key)
	return bucket.creditsAvailable(limiter.now())
}

