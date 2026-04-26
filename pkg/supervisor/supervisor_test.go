package supervisor

import (
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestNew verifies that New creates a command with the correct path and arguments.

func TestNew(t *testing.T) {
	s := New("echo", "hello")
	if s.cmd == nil {
		t.Fatal("expected cmd to be set")
	}
	if len(s.cmd.Args) != 2 || s.cmd.Args[1] != "hello" {
		t.Errorf("cmd.Args = %v, want [echo hello]", s.cmd.Args)
	}
}

// TestSupervisorStartAndWait verifies that Start and Wait complete successfully for a quick command.

func TestSupervisorStartAndWait(t *testing.T) {
	s := New("sleep", "0.1")
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

// TestSupervisorStartAndWait_FailingCommand verifies that Wait returns an error for non-zero exit codes.

func TestSupervisorStartAndWait_FailingCommand(t *testing.T) {
	s := New("sh", "-c", "exit 42")
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	err := s.Wait()
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	ee, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T", err)
	}
	if ee.ExitCode() != 42 {
		t.Errorf("exit code = %d, want 42", ee.ExitCode())
	}
}

// TestSupervisorShutdownNow verifies that ShutdownNow terminates a running process immediately.

func TestSupervisorShutdownNow(t *testing.T) {
	s := New("sleep", "60")
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := s.ShutdownNow(); err != nil {
		t.Fatalf("ShutdownNow: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = s.Wait()
	}()

	wg.Wait()
}

// TestSupervisorShutdownNow_NotRunning verifies that ShutdownNow returns no error when the process hasn't started.

func TestSupervisorShutdownNow_NotRunning(t *testing.T) {
	s := New("echo", "hello")
	err := s.ShutdownNow()
	if err != nil {
		t.Fatalf("expected no error when shutting down non-running process: %v", err)
	}
}

// TestSupervisorShutdownWithTimeout verifies that ShutdownWithTimeout terminates a running process gracefully.

func TestSupervisorShutdownWithTimeout(t *testing.T) {
	s := New("sleep", "60")
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := s.ShutdownWithTimeout(1 * time.Second); err != nil {
		t.Fatalf("ShutdownWithTimeout: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = s.Wait()
	}()

	wg.Wait()
}

// TestSupervisorShutdownWithTimeout_NotRunning verifies that ShutdownWithTimeout returns no error when the process hasn't started.

func TestSupervisorShutdownWithTimeout_NotRunning(t *testing.T) {
	s := New("echo", "hello")
	err := s.ShutdownWithTimeout(1 * time.Second)
	if err != nil {
		t.Fatalf("expected no error when shutting down non-running process: %v", err)
	}
}

// TestSupervisorString verifies that String returns a formatted command string with arguments.

func TestSupervisorString(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantArgs  string
	}{
		{"simple", []string{"arg1"}, "arg1"},
		{"with spaces", []string{"has space"}, `"has space"`},
		{"multiple args", []string{"a", "b", "c"}, "a b c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New("echo", tt.args...)
			got := s.String()
			want := s.cmd.Path + " " + tt.wantArgs
			if got != want {
				t.Errorf("String() = %q, want %q", got, want)
			}
		})
	}
}

// TestSupervisorString_EmptyPath verifies that String returns an empty string when the command path is empty.

func TestSupervisorString_EmptyPath(t *testing.T) {
	s := &Supervisor{cmd: &exec.Cmd{}}
	got := s.String()
	if got != "" {
		t.Errorf("String() = %q, want empty", got)
	}
}

// TestSupervisorStart_Fail verifies that Start returns an error when the binary doesn't exist.

func TestSupervisorStart_Fail(t *testing.T) {
	s := New("/nonexistent/binary/that/does/not/exist")
	err := s.Start()
	if err == nil {
		t.Fatal("expected error starting nonexistent binary")
	}
}

// TestSupervisorIsRunning verifies the isRunning state transitions: not running → running → not running.

func TestSupervisorIsRunning(t *testing.T) {
	s := New("sleep", "60")

	if s.isRunning() {
		t.Error("should not be running before Start")
	}

	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !s.isRunning() {
		t.Error("should be running after Start")
	}

	s.ShutdownNow()
	s.Wait()

	if s.isRunning() {
		t.Error("should not be running after Wait")
	}
}

// TestSupervisorShutdownWithTimeout_AlreadyStarted verifies that a second shutdown attempt returns an error.

func TestSupervisorShutdownWithTimeout_AlreadyStarted(t *testing.T) {
	s := New("sleep", "60")
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := s.ShutdownWithTimeout(2 * time.Second); err != nil {
		t.Fatalf("first ShutdownWithTimeout: %v", err)
	}

	err := s.ShutdownWithTimeout(2 * time.Second)
	if err == nil {
		t.Fatal("expected error when shutdown already started")
	}

	s.Wait()
}

// TestSupervisorSignalPropagation verifies that signals sent to the child process cause it to exit.

func TestSupervisorSignalPropagation(t *testing.T) {
	s := New("sleep", "600")
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- s.Wait() }()

	err := s.cmd.Process.Signal(syscall.SIGTERM)
	if err != nil {
		t.Fatalf("failed to send signal: %v", err)
	}

	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for process")
		s.ShutdownNow()
	}
}
