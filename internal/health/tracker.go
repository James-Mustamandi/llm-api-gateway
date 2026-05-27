package health

import (
	"sync"
	"time"
)

type Tracker struct {
	mutex		sync.Mutex
	breakers 	map[string]*Breaker
	threshold 	int
	timeout 	time.Duration
}

func NewTracker(failureThreshold int, openTimeout time.Duration) *Tracker {
	return &Tracker{
		breakers: make(map[string]*Breaker),
		threshold: failureThreshold,
		timeout: openTimeout,
	}
}

func (tracker *Tracker) breaker(provider string) *Breaker {
	tracker.mutex.Lock()
	defer tracker.mutex.Unlock()
	breaker, ok := tracker.breakers[provider]
	if !ok {
		breaker = NewBreaker(tracker.threshold, tracker.timeout)
		tracker.breakers[provider] = breaker
	}
	return breaker
}

func (tracker *Tracker) Allow(provider string) bool {
	return tracker.breaker(provider).Allow()
}

func (tracker *Tracker) RecordSuccess(provider string) {
	tracker.breaker(provider).RecordSuccess()
}

func (tracker *Tracker) RecordFailure(provider string) {
	tracker.breaker(provider).RecordFailure()
}

func (tracker *Tracker) Statuses() map[string]string {
	tracker.mutex.Lock()
	defer tracker.mutex.Unlock()
	statuses := make(map[string]string, len(tracker.breakers))
	for name, breaker := range tracker.breakers {
		statuses[name] = breaker.Status().String()
	}
	return statuses
}