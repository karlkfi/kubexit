package supervisor

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Supervisor struct {
	cmd           *exec.Cmd
	sigCh         chan os.Signal
	startStopLock sync.Mutex
	shutdown      bool
	shutdownTimer *time.Timer
}

func New(name string, args ...string) *Supervisor {
	// Don't use CommandContext.
	// We want the child process to exit on its own so we can return its exit code.
	// If the child doesn't exit on TERM, then neither should the supervisor.
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return &Supervisor{
		cmd: cmd,
		shutdown: false,
	}
}

func (s *Supervisor) Start() error {
	s.startStopLock.Lock()
	defer s.startStopLock.Unlock()

	if s.shutdown {
		return errors.New("not starting child process: shutdown already started")
	}

	log.Printf("Starting: %s\n", s)
	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start child process: %v", err)
	}

	// Propegate all signals to the child process
	s.sigCh = make(chan os.Signal, 1)
	signal.Notify(s.sigCh)

	go func() {
		for {
			sig, ok := <-s.sigCh
			if !ok {
				return
			}
			// log everything but "urgent I/O condition", which gets noisy
			if sig != syscall.SIGURG {
				log.Printf("Received signal: %v\n", sig)
			}
			// ignore "child exited" signal
			if sig == syscall.SIGCHLD {
				continue
			}
			err := s.cmd.Process.Signal(sig)
			if err != nil {
				log.Printf("Signal propegation failed: %v\n", err)
			}
		}
	}()

	return nil
}

func (s *Supervisor) Wait() error {
	defer func() {
		signal.Reset()
		if s.sigCh != nil {
			close(s.sigCh)
		}
		if s.shutdownTimer != nil {
			s.shutdownTimer.Stop()
		}
	}()
	log.Println("Waiting for child process to exit...")
	return s.cmd.Wait()
}

func (s *Supervisor) ShutdownNow() error {
	s.startStopLock.Lock()
	defer s.startStopLock.Unlock()

	s.shutdown = true

	if !s.isRunning() {
		log.Println("Skipping ShutdownNow: child process not running")
		return nil
	}

	log.Println("Killing child process...")
	// TODO: Use Process.Kill() instead?
	// Sending Interrupt on Windows is not implemented.
	err := s.cmd.Process.Signal(syscall.SIGKILL)
	if err != nil {
		return fmt.Errorf("failed to kill child process: %v", err)
	}
	return nil
}

func (s *Supervisor) ShutdownWithTimeout(timeout time.Duration) error {
	s.startStopLock.Lock()
	defer s.startStopLock.Unlock()

	s.shutdown = true

	if !s.isRunning() {
		log.Println("Skipping ShutdownWithTimeout: child process not running")
		return nil
	}

	if s.shutdownTimer != nil {
		return errors.New("shutdown already started")
	}

	log.Println("Terminating child process...")
	err := s.cmd.Process.Signal(syscall.SIGTERM)
	if err != nil {
		return fmt.Errorf("failed to terminate child process: %v", err)
	}

	s.shutdownTimer = time.AfterFunc(timeout, func() {
		log.Printf("Timeout elapsed: %s\n", timeout)
		err := s.ShutdownNow()
		if err != nil {
			// TODO: ignorable?
			log.Printf("Failed after timeout: %v\n", err)
		}
	})

	return nil
}

func (s *Supervisor) isRunning() bool {
	// Process set by cmd.Start - means started
	// https://golang.org/src/os/exec/exec.go?s=11514:11541#L422
	// ProcessState set by cmd.Wait - means exited
	// https://golang.org/src/os/exec/exec.go?s=14689:14715#L511
	return s.cmd.Process != nil && s.cmd.ProcessState == nil
}

// String joins the command Path and Args and quotes any with spaces
func (s *Supervisor) String() string {
	if s.cmd.Path == "" {
		return ""
	}

	var buffer bytes.Buffer

	quote := strings.ContainsRune(s.cmd.Path, ' ')
	if quote {
		buffer.WriteRune('"')
	}
	buffer.WriteString(s.cmd.Path)
	if quote {
		buffer.WriteRune('"')
	}

	if len(s.cmd.Args) > 1 {
		for _, arg := range s.cmd.Args[1:] {
			buffer.WriteRune(' ')
			quote = strings.ContainsRune(arg, ' ')
			if quote {
				buffer.WriteRune('"')
			}
			buffer.WriteString(arg)
			if quote {
				buffer.WriteRune('"')
			}
		}
	}

	return buffer.String()
}
