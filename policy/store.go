package policy

import "sync"

// We hold the policy behind RWMutex so configuration updates can safely go through without a restart.
type Store struct {
	mu sync.RWMutex
	p  *Policy
}

// NewStore wraps p.
func NewStore(p *Policy) *Store { return &Store{p: p} }

// Current returns the policy in force. s must be non-nil.
func (s *Store) Current() *Policy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.p
}

// Replace swaps in a freshly loaded policy.
func (s *Store) Replace(p *Policy) {
	s.mu.Lock()
	s.p = p
	s.mu.Unlock()
}
