package observer

import (
	"log"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type MockObserver struct {
	closed bool
}

func (m *MockObserver) Close() {
	m.closed = true
}

func TestSingletonInstance(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(2)

	var instance1, instance2 *instanceObserver

	// Goroutine 1
	go func() {
		defer wg.Done()
		instance1 = Instance()
	}()

	// Goroutine 2
	go func() {
		defer wg.Done()
		instance2 = Instance()
	}()

	wg.Wait()

	if instance1 != instance2 {
		t.Errorf("Expected both instances to be the same, got %p and %p", instance1, instance2)
	}
}

func TestAttachAndNotify(t *testing.T) {
	// Reset the singleton instance for testing purposes
	instanceOnce = sync.Once{}
	instance = nil

	instance := Instance()

	mockObserver1 := &MockObserver{}
	mockObserver2 := &MockObserver{}

	Attach(mockObserver1)
	Attach(mockObserver2)

	if len(instance.observers) != 2 {
		t.Errorf("Expected 2 observers, got %d", len(instance.observers))
	}

	instance.notify()

	if !mockObserver1.closed {
		t.Errorf("Expected mockObserver1 to be closed")
	}

	if !mockObserver2.closed {
		t.Errorf("Expected mockObserver2 to be closed")
	}
}

// orderObserver records the global close order so a test can assert that
// notify() shuts resources down in reverse (LIFO) registration order.
type orderObserver struct {
	name  string
	order *[]string
}

func (o *orderObserver) Close() { *o.order = append(*o.order, o.name) }

// TestNotifyClosesInReverseOrder guards the shutdown invariant: a Kafka consumer
// attaches AFTER the DB it uses (consumers are built post-Build), so it must close
// FIRST — otherwise in-flight handlers hit a closed pool ("sql: database is closed").
func TestNotifyClosesInReverseOrder(t *testing.T) {
	instanceOnce = sync.Once{}
	instance = nil
	Instance()

	var order []string
	Attach(&orderObserver{name: "db", order: &order})       // registered during Build
	Attach(&orderObserver{name: "cache", order: &order})    // registered during Build
	Attach(&orderObserver{name: "consumer", order: &order}) // registered after Build

	instance.notify()

	assert.Equal(t, []string{"consumer", "cache", "db"}, order,
		"consumers (registered last) must drain before the DB/cache they depend on")
}

type observerTest struct {
	closed bool
}

func (o *observerTest) Close() {
	o.closed = true
	log.Println("close observer")
}

func TestSignalHandling(t *testing.T) {
	o := &observerTest{closed: false}
	Instance()
	Attach(o)

	assert.False(t, o.closed)
	instance.notify()
	assert.True(t, o.closed)
}

func TestGetWaitGroupSingleton(t *testing.T) {
	// Reset to get a clean slate for this test
	once = sync.Once{}
	singleInstance = nil

	wg1 := GetWaitGroup()
	wg2 := GetWaitGroup()

	if wg1 == nil {
		t.Fatal("GetWaitGroup() returned nil")
	}
	if wg1 != wg2 {
		t.Errorf("GetWaitGroup() must return the same instance each call: got %p vs %p", wg1, wg2)
	}
}

func TestWaitRunningTimeoutNoTimeout(t *testing.T) {
	once = sync.Once{}
	singleInstance = nil

	// An empty WaitGroup completes immediately — must not time out.
	timedOut := WaitRunningTimeout()
	if timedOut {
		t.Error("Expected WaitRunningTimeout()=false for an empty WaitGroup")
	}
}

func TestWaitRunningTimeoutExpires(t *testing.T) {
	once = sync.Once{}
	singleInstance = nil

	wg := GetWaitGroup()
	wg.Add(1) // never decremented — forces timeout

	original := WAIT_GROUP_TIMEOUT_SECONDS
	WAIT_GROUP_TIMEOUT_SECONDS = 1
	defer func() {
		WAIT_GROUP_TIMEOUT_SECONDS = original
		wg.Done() // unblock after test completes
	}()

	timedOut := WaitRunningTimeout()
	if !timedOut {
		t.Error("Expected WaitRunningTimeout()=true when WaitGroup is never released")
	}
}

func TestWaitForSignal(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		waitForSignal()
	}()

	// Give the goroutine time to register the signal handler.
	time.Sleep(20 * time.Millisecond)

	// Send SIGTERM to ourselves; waitForSignal must unblock.
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("Kill(SIGTERM) failed: %v", err)
	}

	select {
	case <-done:
		// waitForSignal returned as expected
	case <-time.After(2 * time.Second):
		t.Error("waitForSignal did not return after SIGTERM")
	}
}

// TestSetupShutdownLaunches must run AFTER TestWaitForSignal (i.e. after the
// SIGTERM has been consumed) so the goroutine spawned by SetupShutdown blocks
// safely on waitForSignal without ever reaching os.Exit(0).
func TestSetupShutdownLaunches(t *testing.T) {
	r := New()
	SetupShutdown(r, 100*time.Millisecond)
	// Give the goroutine time to start and reach waitForSignal.
	time.Sleep(20 * time.Millisecond)
	// The goroutine is now blocked inside waitForSignal; test ends cleanly.
}
