package observer

import "sync"

var shutdownOnce sync.Once

// Shutdown closes every attached observer in reverse (LIFO) order, once.
// The observer no longer listens to OS signals: gofi's Service.Run (or the
// application, via signal.NotifyContext) decides when to call it.
func Shutdown() {
	shutdownOnce.Do(func() { Instance().notify() })
}
