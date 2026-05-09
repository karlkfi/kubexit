//go:build integration

package watch

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const testImage = "kubexit/test-server:latest"

type WatchSuite struct {
	suite.Suite
	clientset *kubernetes.Clientset
}

func (s *WatchSuite) SetupSuite() {
	s.T().Log("TestSuite: SetupSuite...")

	redirectLogs(s.T())

	if _, err := exec.LookPath("kind"); err != nil {
		s.Require().NoError(err, "kind not found in PATH")
	}

	kindClusterName := os.Getenv("KIND_CLUSTER_NAME")
	createCluster := false
	if kindClusterName == "" {
		createCluster = true
		kindClusterName = "kubexit-test"
	}

	var err error
	s.clientset, err = setupKind(s.T(), kindClusterName, createCluster)
	if err != nil {
		s.Require().NoError(err, "Failed to setup Kind cluster")
	}

	// Build and load the test image into the cluster
	if err := buildAndLoadImage(s.T(), kindClusterName); err != nil {
		s.Require().NoError(err, "Failed to build and load test image")
	}
}

func (s *WatchSuite) TearDownSuite() {
	s.T().Log("TestSuite: TearDownSuite...")
}

func TestWatchSuite(t *testing.T) {
	suite.Run(t, new(WatchSuite))
}

type testLogWriter struct {
	t *testing.T
}

func (w *testLogWriter) Write(p []byte) (n int, err error) {
	// Skip 3 frames:
	// 0: runtime.Callers, 1: Write, 2: log.Output, 3: Your actual code
	_, file, line, ok := runtime.Caller(3)
	if ok {
		// Add log file path and log line
		// Format it so it looks like a standard Go error/log
		w.t.Logf("%s:%d: %s", filepath.Base(file), line, string(p))
	} else {
		w.t.Log(string(p))
	}
	return len(p), nil
}

func redirectLogs(t *testing.T) {
	t.Helper()
	oldOut := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&testLogWriter{t})
	log.SetFlags(0) // Disable log file path, log line, timestamp
	t.Cleanup(func() {
		log.SetOutput(oldOut)
		log.SetFlags(oldFlags)
	})
}

// setupKind creates a Kind cluster and returns a clientset connected to it.
// It registers a cleanup function to delete the cluster when the test finishes.
func setupKind(t *testing.T, kindClusterName string, createCluster bool) (*kubernetes.Clientset, error) {
	if createCluster {
		t.Logf("Creating Kind cluster")
		if _, err := runCmdOutput(t, t.Context(), "kind", "create", "cluster", "--name", kindClusterName, "--wait", "60s"); err != nil {
			return nil, fmt.Errorf("failed to create Kind cluster: %w", err)
		}
		t.Cleanup(func() {
			t.Logf("Deleting Kind cluster")
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			_ = runCmd(t, ctx, "kind", "delete", "cluster", "--name", kindClusterName)
		})
	}

	clientset, err := getKindClientset(t, kindClusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to get kind clientset: %w", err)
	}

	return clientset, nil
}

func runCmd(t *testing.T, ctx context.Context, cmd string, args ...string) error {
	t.Logf("Running: %s %s", cmd, args)
	c := exec.CommandContext(ctx, cmd, args...)
	c.Dir = "../.."
	out, err := c.CombinedOutput()
	if len(out) > 0 {
		t.Logf("Output: %s", out)
	}
	return err
}

func runCmdOutput(t *testing.T, ctx context.Context, cmd string, args ...string) (string, error) {
	t.Logf("Running: %s %s", cmd, args)
	c := exec.CommandContext(ctx, cmd, args...)
	c.Dir = "../.."
	out, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %s", err, out)
	}
	return string(out), nil
}

// buildAndLoadImage builds the test-server Docker image and loads it into the Kind cluster.
func buildAndLoadImage(t *testing.T, kindClusterName string) error {
	// Build the image from the test-server directory
	t.Logf("Building test-server image")
	// docker build -f cmd/test-server/Dockerfile -t kubexit/test-server:latest .
	if err := runCmd(t, t.Context(), "docker", "build", "-f", "cmd/test-server/Dockerfile", "-t", testImage, "."); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	// Load the image into Kind
	t.Logf("Loading image into Kind")
	// kind load docker-image kubexit/test-server:latest --name kubexit-test
	return runCmd(t, t.Context(), "kind", "load", "docker-image", testImage, "--name", kindClusterName)
}

func getKindClientset(t *testing.T, kindClusterName string) (*kubernetes.Clientset, error) {
	kubeconfigData, err := runCmdOutput(t, t.Context(), "kind", "get", "kubeconfig", "--name", kindClusterName)
	if err != nil {
		return nil, fmt.Errorf("getting kind kubeconfig: %w", err)
	}

	config, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfigData))
	if err != nil {
		return nil, fmt.Errorf("parsing kind kubeconfig: %w", err)
	}

	return kubernetes.NewForConfig(config)
}
