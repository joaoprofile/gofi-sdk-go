package observer

import (
	"log"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type Observer interface {
	Close()
}

type subject interface {
	attach(observer Observer)
	notify()
}

type instanceObserver struct {
	mu        sync.Mutex
	observers []Observer
}

var (
	instanceOnce sync.Once
	instance     *instanceObserver

	WAIT_GROUP_TIMEOUT_SECONDS = 90
	once                       sync.Once
	singleInstance             *sync.WaitGroup
)

func Instance() *instanceObserver {
	instanceOnce.Do(func() {
		instance = &instanceObserver{
			observers: make([]Observer, 0),
		}
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGKILL, os.Interrupt)
		go func() {
			sig := <-ch
			log.Printf("notify shutdown: %+v", sig)
			instance.notify()
		}()
	})
	return instance
}

func Attach(o Observer) {
	Instance().attach(o)
}

func (s *instanceObserver) attach(observer Observer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observers = append(s.observers, observer)
}

func (s *instanceObserver) notify() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, observer := range s.observers {
		observer.Close()
	}
}

func GetWaitGroup() *sync.WaitGroup {
	once.Do(func() {
		slog.Debug("observer: creating WaitGroup singleton")
		singleInstance = &sync.WaitGroup{}
	})
	slog.Debug("observer: returning WaitGroup singleton")
	return singleInstance
}

func WaitRunningTimeout() bool {
	timeout := WAIT_GROUP_TIMEOUT_SECONDS
	c := make(chan struct{})

	go func() {
		defer close(c)
		GetWaitGroup().Wait()
	}()

	select {
	case <-c:
		return false
	case <-time.After(time.Duration(timeout) * time.Second):
		return true
	}
}
