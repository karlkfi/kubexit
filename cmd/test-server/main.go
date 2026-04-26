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

	ctx, wrapped := withGracefulShutdown(context.Background(), 0)

	mux := http.NewServeMux()
	mux.HandleFunc("/exit", exitHandler(ctx, 0))
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
	os.Exit(wrapped.exitCode)
}

func exitHandler(ctx context.Context, exitCode int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		codeStr := r.URL.Query().Get("exit_code")
		if codeStr != "" {
			if parsed, err := strconv.Atoi(codeStr); err == nil {
				exitCode = parsed
			}
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("exiting with code %d\n", exitCode)))
		wrapped, ok := ctx.(*cancelContext)
		if ok {
			wrapped.exitCode = exitCode
			wrapped.cancel()
		}
		log.Printf("Exit requested with code %d", exitCode)
	}
}

func healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}
}

// withGracefulShutdown returns a context that cancels on SIGTERM/SIGINT.
func withGracefulShutdown(ctx context.Context, exitCode int) (context.Context, *cancelContext) {
	ctx, cancel := context.WithCancel(ctx)
	wrapped := &cancelContext{Context: ctx, cancel: cancel, exitCode: exitCode}

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

	return wrapped, wrapped
}

// cancelContext wraps context with an exposed cancel for the exit handler.
type cancelContext struct {
	context.Context
	cancel   context.CancelFunc
	exitCode int
}
