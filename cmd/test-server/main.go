package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func main() {
	log.SetFlags(log.Flags() &^ (log.Ldate | log.Ltime))

	ctx := withGracefulShutdown(context.Background())

	mux := http.NewServeMux()
	mux.HandleFunc("/exit", exitHandler(ctx))
	mux.HandleFunc("/health", healthHandler())

	portStr := os.Getenv("PORT")
	if portStr == "" {
		portStr = "80"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		log.Fatalf("PORT is not a valid integer: %s", portStr)
	}

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		log.Printf("Listening on %s\n", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Shutdown error: %v", err)
	}

	log.Println("Exited cleanly")
	os.Exit(0)
}

func exitHandler(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("exiting\n"))
		wrapped, ok := ctx.(*cancelContext)
		if ok {
			wrapped.cancel()
		}
		log.Println("Exit requested")
	}
}

func healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}
}

// withGracefulShutdown returns a context that cancels on SIGTERM/SIGINT.
func withGracefulShutdown(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	wrapped := &cancelContext{Context: ctx, cancel: cancel}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		defer signal.Stop(sigCh)
		select {
		case s := <-sigCh:
			log.Printf("Received signal: %v", s)
			cancel()
		case <-ctx.Done():
		}
	}()

	return wrapped
}

// cancelContext wraps context with an exposed cancel for the exit handler.
type cancelContext struct {
	context.Context
	cancel context.CancelFunc
}
