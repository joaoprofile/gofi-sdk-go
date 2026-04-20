package session

import "sync"

// ResetSingleton resets the Session singleton for testing.
// Call this at the start and end of each test that calls New().
func ResetSingleton() {
	once = sync.Once{}
	instance = nil
}
