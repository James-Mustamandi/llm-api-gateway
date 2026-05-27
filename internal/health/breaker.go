package health

import (
	"sync"
	"time"
)

type State int

const (
	StateClosed State = iota	// normal
	StateOpen					// traffic blocked				
	StateHalfOpen				// testing 1 trial request allowed
)

func (state State) String() string {
	switch state {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

type Breaker struct {
	mutex 				sync.Mutex
	state 				State
	consecutiveFails	int
	openedAt 			time.Time

	failureThreshold 	int
	openTimeout 		time.Duration
	now 				func() time.Time
}

func NewBreaker(failureThreshold int, openTimeout time.Duration) *Breaker {
	return &Breaker {
		state:				StateClosed,
		failureThreshold: 	failureThreshold,
		openTimeout: 		openTimeout,
		now: 				time.Now,
	}
}

func (breaker *Breaker) Allow() bool {
	breaker.mutex.Lock()
	defer breaker.mutex.Unlock()

	switch breaker.state {
	case StateClosed:
		return true
	case StateOpen:
		if breaker.now().Sub(breaker.openedAt) >= breaker.openTimeout {
			breaker.state = StateHalfOpen
			return true
		}
		return false
	case StateHalfOpen:
		return false
	default:
		return false
	}
}

func (breaker *Breaker) RecordSuccess() {
	breaker.mutex.Lock()
	defer breaker.mutex.Unlock()
	breaker.consecutiveFails = 0
	breaker.state = StateClosed
}


func (breaker *Breaker) RecordFailure() {
	breaker.mutex.Lock()
	defer breaker.mutex.Unlock()

	switch breaker.state {
	case StateHalfOpen:
		breaker.state = StateOpen
		breaker.openedAt = breaker.now()
	case StateClosed:
		breaker.consecutiveFails++
		if breaker.consecutiveFails >= breaker.failureThreshold {
			breaker.state = StateOpen
			breaker.openedAt = breaker.now()
		}
	case StateOpen:
	}
}

func (breaker *Breaker) Status() State {
	breaker.mutex.Lock()
	defer breaker.mutex.Unlock()
	if breaker.state == StateOpen && breaker.now().Sub(breaker.openedAt) >= breaker.openTimeout {
		return StateHalfOpen
	}
	return breaker.state
}