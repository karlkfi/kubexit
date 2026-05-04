package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestServerIntegration(t *testing.T) {
	port := pickPort(t)
	binary := mustBuild(t)

	ctx := t.Context()
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%s", strconv.Itoa(port)))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// Ensure cleanup on test end.
	t.Cleanup(func() {
		if cmd.Process != nil && cmd.ProcessState == nil {
			cmd.Process.Signal(syscall.SIGTERM)
			cmd.Wait()
		}
	})

	baseURL := fmt.Sprintf("http://localhost:%d", port)
	if err := waitForReady(baseURL, 3*time.Second); err != nil {
		t.Fatal(err)
	}

	t.Run("health endpoint returns 200", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/health")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if string(body) != "ok" {
			t.Errorf("expected body 'ok', got %q", string(body))
		}
	})

	t.Run("exit endpoint returns 200 and shuts down", func(t *testing.T) {
		// The /exit POST triggers graceful shutdown, so the parent process
		// will exit after this sub-test. The health test above runs first
		// (alphabetical order: "health" < "exit" is false, actually "e" < "h").
		// We rely on test ordering: health runs first, then exit kills the server.
		resp, err := http.Post(baseURL+"/exit", "text/plain", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}

		// Server should exit cleanly after /exit.
		if err := cmd.Wait(); err != nil {
			t.Fatalf("server did not exit cleanly: %v", err)
		}
	})
}

func TestSIGTERM(t *testing.T) {
	port := pickPort(t)
	binary := mustBuild(t)

	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%s", strconv.Itoa(port)))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	baseURL := fmt.Sprintf("http://localhost:%d", port)
	if err := waitForReady(baseURL, 3*time.Second); err != nil {
		t.Fatal(err)
	}

	// Send SIGTERM — server should shut down gracefully.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	if err := cmd.Wait(); err != nil {
		t.Fatalf("server did not exit cleanly after SIGTERM: %v", err)
	}
}

func TestExitCodeURLParam(t *testing.T) {
	port := pickPort(t)
	binary := mustBuild(t)

	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%s", strconv.Itoa(port)))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	baseURL := fmt.Sprintf("http://localhost:%d", port)
	if err := waitForReady(baseURL, 3*time.Second); err != nil {
		t.Fatal(err)
	}

	// Hit /exit with a custom exit code.
	resp, err := http.Post(baseURL+"/exit?exit_code=42", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	err = cmd.Wait()
	if err == nil {
		t.Fatal("expected non-zero exit")
	}

	// The exit code for a process that calls os.Exit(42) is 42 << 8 on Unix.
	if exitErr, ok := err.(*exec.ExitError); !ok {
		t.Fatalf("expected *exec.ExitError, got %T", err)
	} else if ws, ok := exitErr.Sys().(syscall.WaitStatus); !ok {
		t.Fatalf("expected syscall.WaitStatus, got %T", exitErr.Sys())
	} else if ws.ExitStatus() != 42 {
		t.Errorf("expected exit code 42, got %d", ws.ExitStatus())
	}
}

func TestExitCodePostBody(t *testing.T) {
	port := pickPort(t)
	binary := mustBuild(t)

	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%s", strconv.Itoa(port)))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	baseURL := fmt.Sprintf("http://localhost:%d", port)
	if err := waitForReady(baseURL, 3*time.Second); err != nil {
		t.Fatal(err)
	}

	// Hit /exit with a custom exit code.
	resp, err := http.Post(baseURL+"/exit", "application/x-www-form-urlencoded", strings.NewReader("exit_code=42"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	err = cmd.Wait()
	if err == nil {
		t.Fatal("expected non-zero exit")
	}

	// The exit code for a process that calls os.Exit(42) is 42 << 8 on Unix.
	if exitErr, ok := err.(*exec.ExitError); !ok {
		t.Fatalf("expected *exec.ExitError, got %T", err)
	} else if ws, ok := exitErr.Sys().(syscall.WaitStatus); !ok {
		t.Fatalf("expected syscall.WaitStatus, got %T", exitErr.Sys())
	} else if ws.ExitStatus() != 42 {
		t.Errorf("expected exit code 42, got %d", ws.ExitStatus())
	}
}

func TestExitCodeDefault(t *testing.T) {
	port := pickPort(t)
	binary := mustBuild(t)

	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%s", strconv.Itoa(port)))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	baseURL := fmt.Sprintf("http://localhost:%d", port)
	if err := waitForReady(baseURL, 3*time.Second); err != nil {
		t.Fatal(err)
	}

	// Hit /exit without exit_code query param — should default to 0.
	resp, err := http.Post(baseURL+"/exit", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	err = cmd.Wait()
	if err != nil {
		t.Fatalf("server did not exit cleanly: %v", err)
	}
}

func TestSIGKILL(t *testing.T) {
	port := pickPort(t)
	binary := mustBuild(t)

	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%s", strconv.Itoa(port)))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	baseURL := fmt.Sprintf("http://localhost:%d", port)
	if err := waitForReady(baseURL, 3*time.Second); err != nil {
		t.Fatal(err)
	}

	// SIGKILL cannot be caught — server is force-killed, exit is not clean.
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}

	if err := cmd.Wait(); err == nil {
		t.Fatal("expected non-clean exit after SIGKILL")
	}
}

func pickPort(tb testing.TB) int {
	tb.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	port := lis.Addr().(*net.TCPAddr).Port
	lis.Close()
	return port
}

func mustBuild(t *testing.T) string {
	binary, err := buildBinary(t)
	if err != nil {
		t.Fatal(err)
	}
	return binary
}

func buildBinary(tb testing.TB) (string, error) {
	tmp, err := os.MkdirTemp("", "test-server")
	if err != nil {
		return "", err
	}
	dst := filepath.Join(tmp, "test-server")
	cmd := exec.Command("go", "build", "-o", dst, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build failed: %v: %s", err, out)
	}
	return dst, nil
}

func waitForReady(baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("server at %s did not become ready within %s", baseURL, timeout)
}
