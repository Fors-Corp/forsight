package cmd

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

// A shutdown that runs out of its budget must still look like a successful
// stop. The process is exiting because something asked it to, and the exit
// status is what `systemctl stop` and `docker stop` report — so a slow client,
// or just a loaded machine, must not turn SIGINT into a failure. CI caught
// this the expensive way: TestBootRealBinary failed once in 40 runs with
// "exited with code 1 after SIGINT" and `context deadline exceeded`, nothing
// else wrong.
func TestGracefulShutdown_BudgetExceededIsNotAFailure(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// A handler that outlives the budget is what makes Shutdown block: it
	// waits for in-flight requests, and this one is deliberately still in
	// flight when the deadline passes.
	released := make(chan struct{})
	handlerRunning := make(chan struct{})
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(handlerRunning)
			<-released
			w.WriteHeader(http.StatusOK)
		}),
		ReadHeaderTimeout: time.Second,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() {
		close(released)
		_ = srv.Close()
	})

	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	select {
	case <-handlerRunning:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never started: the request never reached the server")
	}

	if err := gracefulShutdown(srv, 50*time.Millisecond, logger); err != nil {
		t.Errorf("gracefulShutdown with an in-flight request = %v, want nil", err)
	}
}

// waitUntilAccepting blocks until addr accepts a TCP connection, so a test
// never races http.Server.Serve's own setup.
func waitUntilAccepting(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s never started accepting connections", addr)
}

// failingCloseListener reports an error when its listener is closed, which is
// the one way Shutdown returns something other than a deadline: it closes the
// server's listeners and hands back what that reported.
type failingCloseListener struct {
	net.Listener
	err error
}

func (l failingCloseListener) Close() error {
	_ = l.Listener.Close()
	return l.err
}

// The budget case is the only one that gets swallowed; anything else Shutdown
// reports still has to reach the caller, or a genuinely broken stop would look
// clean.
func TestGracefulShutdown_OtherErrorsPropagate(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	wantErr := errors.New("closing the listener went wrong")
	srv := &http.Server{
		Handler:           http.NotFoundHandler(),
		ReadHeaderTimeout: time.Second,
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(failingCloseListener{Listener: listener, err: wantErr}) }()
	// Shutdown only reports what closing the server's *registered* listeners
	// returned, and Serve registers ours asynchronously — without waiting,
	// Shutdown can find nothing to close and return nil, which is a race in
	// the test rather than a result.
	waitUntilAccepting(t, listener.Addr().String())

	gotErr := gracefulShutdown(srv, 5*time.Second, logger)
	if !errors.Is(gotErr, wantErr) {
		t.Errorf("gracefulShutdown = %v, want it to carry %v", gotErr, wantErr)
	}
	if errors.Is(gotErr, context.DeadlineExceeded) {
		t.Error("got the deadline error; this case is not a timeout")
	}
}

// With nothing in flight the budget is irrelevant: a clean drain returns nil
// without waiting for it.
func TestGracefulShutdown_IdleServerReturnsImmediately(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv := &http.Server{
		Handler:           http.NotFoundHandler(),
		ReadHeaderTimeout: time.Second,
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(listener) }()

	start := time.Now()
	if err := gracefulShutdown(srv, 5*time.Second, logger); err != nil {
		t.Errorf("gracefulShutdown on an idle server = %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %s to drain an idle server; it should not wait out the budget", elapsed)
	}
}
