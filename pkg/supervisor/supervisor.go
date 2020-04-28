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
	shutdownLock  sync.Mutex
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
	}
}

func (s *Supervisor) Start() error {
	if err := s.cmd.Start(); err != nil {
		return err
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
		close(s.sigCh)
		if s.shutdownTimer != nil {
			s.shutdownTimer.Stop()
		}
	}()
	return s.cmd.Wait()
}

func (s *Supervisor) Signal(sig os.Signal) error {
	// Process set by Start
	if s.cmd.Process == nil {
		return errors.New("cannot signal unstarted child process")
	}

	err := s.cmd.Process.Signal(sig)
	if err != nil {
		return fmt.Errorf("failed to signal child process: %v", err)
	}
	return nil
}

func (s *Supervisor) ShutdownNow() error {
	log.Println("Killing child process...")
	// TODO: Use Process.Kill() instead?
	// Sending Interrupt on Windows is not implemented.
	err := s.Signal(syscall.SIGKILL)
	if err != nil {
		return fmt.Errorf("failed to shutdown child process: %v", err)
	}
	return nil
}

func (s *Supervisor) ShutdownWithTimeout(timeout time.Duration) error {
	// one shutdown timer at a time
	s.shutdownLock.Lock()
	defer s.shutdownLock.Unlock()

	if s.shutdownTimer != nil {
		return errors.New("shutdown already started")
	}

	log.Println("Terminating child process...")
	err := s.Signal(syscall.SIGTERM)
	if err != nil {
		return fmt.Errorf("failed to shutdown child process: %v", err)
	}

	s.shutdownTimer = time.AfterFunc(timeout, func() {
		// Process set by Start - not started
		if s.cmd.Process == nil {
			return
		}
		// ProcessState set by Wait - already exited
		if s.cmd.ProcessState != nil {
			return
		}

		log.Printf("Timeout elapsed: %s\n", timeout)
		err := s.ShutdownNow()
		if err != nil {
			// TODO: ignorable?
			log.Printf("Failed after timeout: %v\n", err)
		}
	})

	return nil
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
