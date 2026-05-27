package metrics

import (
	"sync"
	"sync/atomic"
)

type Counters struct {
	requestsTotal 		atomic.Int64
	rateLimited   		atomic.Int64
	allProvidersFailed 	atomic.Int64
	tokensTotal 		atomic.Int64

	mutex 				sync.Mutex
	providerSuccess 	map[string]int64
	providerFailure 	map[string]int64
}

func New() *Counters {
	return &Counters{
		providerSuccess: make(map[string]int64),
		providerFailure: make(map[string]int64),
	}
}

func (counter *Counters) IncRequests() {
	counter.requestsTotal.Add(1)
}

func (counter *Counters) IncRateLimited() {
	counter.rateLimited.Add(1)
}

func (counter *Counters) IncAllFailed() {
	counter.allProvidersFailed.Add(1)
}

func (counter *Counters) AddTokens(n int64) {
	counter.tokensTotal.Add(n)
}

func (counter *Counters) IncProviderSuccess(name string) {
	counter.mutex.Lock()
	counter.providerSuccess[name]++
	counter.mutex.Unlock()
}

func (counter *Counters) IncProviderFailure(name string) {
	counter.mutex.Lock()
	counter.providerFailure[name]++
	counter.mutex.Unlock()
}

type Snapshot struct {
	RequestsTotal		int64
	RateLimited 		int64
	AllProvidersFailed 	int64
	TokensTotal			int64
	ProviderSuccess 	map[string]int64
	ProviderFailure 	map[string]int64
}

func (counter *Counters) Snapshot() Snapshot {
	counter.mutex.Lock()
	providerSuccess := make(map[string]int64, len(counter.providerSuccess))
	for metric, value := range counter.providerSuccess {
		providerSuccess[metric] = value
	}

	providerFailure := make(map[string]int64, len(counter.providerFailure))
	for metric, value := range counter.providerFailure {
		providerFailure[metric] = value
	}
	counter.mutex.Unlock()
	return Snapshot{
		RequestsTotal: 		counter.requestsTotal.Load(),
		RateLimited: 		counter.rateLimited.Load(),
		AllProvidersFailed: counter.allProvidersFailed.Load(),
		TokensTotal: 		counter.tokensTotal.Load(),
		ProviderSuccess: 	providerSuccess,
		ProviderFailure: 	providerFailure,
	}
}
