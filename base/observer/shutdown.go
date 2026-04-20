package observer

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func SetupShutdown(lc *Registry, timeout time.Duration) {
	go func() {
		waitForSignal()
		log.Print("signal received, initiating shutdown...")

		if err := lc.CloseWithTimeout(timeout); err != nil {
			log.Fatalf("error closing resources: %v", err)
		}

		log.Print("shutdown completed")
		os.Exit(0)
	}()
}

func waitForSignal() {
	ch := make(chan os.Signal, 1)

	signal.Notify(
		ch,
		syscall.SIGINT,
		syscall.SIGTERM,
	)

	<-ch
}
