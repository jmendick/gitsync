package auth

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

type stateStore struct {
	states map[string]time.Time
	mu     sync.RWMutex
}

func newStateStore() *stateStore {
	return &stateStore{
		states: make(map[string]time.Time),
	}
}

func (s *stateStore) Generate() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	state := base64.URLEncoding.EncodeToString(b)

	s.mu.Lock()
	s.states[state] = time.Now()
	s.mu.Unlock()

	// Clean up old states
	go s.cleanup()

	return state, nil
}

func (s *stateStore) Validate(state string) bool {
	s.mu.RLock()
	timestamp, exists := s.states[state]
	s.mu.RUnlock()

	if !exists {
		return false
	}

	// Remove used state
	s.mu.Lock()
	delete(s.states, state)
	s.mu.Unlock()

	// State tokens expire after 10 minutes
	return time.Since(timestamp) < 10*time.Minute
}

func (s *stateStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for state, timestamp := range s.states {
		if time.Since(timestamp) > 10*time.Minute {
			delete(s.states, state)
		}
	}
}
