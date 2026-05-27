package health

import (
	"testing"
	"time"
)

func TestTripsAfterThreshol(t *testing.T) {
	breaker := NewBreaker(3, 30 * time.Second)
	clock := time.Now()
	breaker.now = func() time.Time { return clock }	

	if !breaker.Allow() {
		t.Fatal("fresh breaker should be closed/allow")
	}

	breaker.RecordFailure()
	breaker.RecordFailure()

	if !breaker.Allow() {
		t.Fatal("2 failures < threshold 3; should still allow")
	}

	breaker.RecordFailure()
	if breaker.Allow() {
		t.Fatal("breaker should be OPEN after 3 failures and deny")
	}

}

func TestSuccessFailures(t *testing.T) {
	breaker := NewBreaker(3, 30 * time.Second)
	breaker.RecordFailure()
	breaker.RecordFailure()
	breaker.RecordSuccess()
	breaker.RecordFailure()
	breaker.RecordFailure()
	if !breaker.Allow() {
		t.Fatal("success should have reset the failure count; 2 fails < 3 should still allow")
	}
}

func TestHalfOpenTrialAndRecovery(t *testing.T) {
	breaker := NewBreaker(1, 30 * time.Second)
	clock := time.Now()
	breaker.now = func() time.Time { return clock }

	breaker.RecordFailure()
	if breaker.Allow() {
		t.Fatal("should be open immediately after tripping")
	}
 
	clock = clock.Add(31 * time.Second)
	if !breaker.Allow() {
		t.Fatal("after open timeout one trial should be allowed (half-open)")
	}

	if breaker.Allow() {
		t.Fatal("only one half-open trial should be allowed, second must be denied")
	}

	breaker.RecordSuccess()

	if !breaker.Allow() {
		t.Fatal("after succcessful trial, breaker should be closed and allow")
	}
}

func TestHalfOpenTrialFailureReopens(t *testing.T) {
	breaker := NewBreaker(1, 30*time.Second)
	clock := time.Now()
	breaker.now = func() time.Time { return clock }

	breaker.RecordFailure()
	clock = clock.Add(31 * time.Second)
	breaker.Allow()
	breaker.RecordFailure()

	if breaker.Allow() {
		t.Fatal("failed trial should reopen the breaker and deny")
	}

	clock = clock.Add(31 * time.Second)
	if !breaker.Allow() {
		t.Fatal("after reopen + timeout, a new trial should be allowed")
	}
}