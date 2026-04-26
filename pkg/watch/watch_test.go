package watch

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const kindClusterName = "kubexit-test"

// TestWatchPod_Integration serves as the harness for integration tests against
// a Kind cluster. It creates the cluster once, provides a shared clientset to
// each child test, and tears down the cluster when all children complete.
func TestWatchPod_Integration(t *testing.T) {
	if _, err := exec.LookPath("kind"); err != nil {
		t.Skip("kind not found in PATH")
	}

	clientset, err := setupKind(t)
	if err != nil {
		t.Fatalf("failed to setup Kind cluster: %v", err)
	}

	t.Run("pod_phase_transitions_from_pending_to_failed", func(t *testing.T) {
		namespace := "default"
		podName := fmt.Sprintf("kubexit-test-%d", time.Now().UnixNano())

		_, err := clientset.CoreV1().Pods(namespace).Create(context.Background(), &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: namespace},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:    "busybox",
						Image:   "busybox:latest",
						Command: []string{"/bin/sh", "-c", "exit 42"},
					},
				},
				RestartPolicy: v1.RestartPolicyNever,
			},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("failed to create pod: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		phases := make([]v1.PodPhase, 0, 3)
		handler := func(event watch.Event) {
			if pod, ok := event.Object.(*v1.Pod); ok {
				t.Logf("Watch event: type=%s phase=%s reason=%s", event.Type, pod.Status.Phase, pod.Status.Reason)
				phases = append(phases, pod.Status.Phase)
			}
		}

		err = WatchPod(ctx, clientset, namespace, podName, handler)
		if err != nil {
			t.Fatalf("WatchPod failed: %v", err)
		}

		foundPending := false
		foundTerminal := false
		for _, phase := range phases {
			if phase == v1.PodPending {
				foundPending = true
			}
			if phase == v1.PodFailed || phase == v1.PodSucceeded {
				foundTerminal = true
			}
		}

		if !foundPending {
			t.Error("expected to observe Pending phase")
		}
		if !foundTerminal {
			t.Error("expected to observe a terminal phase (Failed or Succeeded)")
		}
	})
}

// setupKind creates a Kind cluster and returns a clientset connected to it.
// It registers a cleanup function to delete the cluster when the test finishes.
func setupKind(t *testing.T) (*kubernetes.Clientset, error) {
	t.Log("Creating Kind cluster")
	if err := runCmd(t, "kind", "create", "cluster", "--name", kindClusterName, "--wait", "60s"); err != nil {
		return nil, fmt.Errorf("failed to create Kind cluster: %w", err)
	}
	t.Cleanup(func() {
		t.Log("Deleting Kind cluster")
		_ = runCmd(t, "kind", "delete", "cluster", "--name", kindClusterName)
	})

	return getKindClientset(t)
}

func runCmd(t *testing.T, cmd string, args ...string) error {
	t.Logf("Running: %s %s", cmd, args)
	c := exec.Command(cmd, args...)
	out, err := c.CombinedOutput()
	if len(out) > 0 {
		t.Logf("Output: %s", out)
	}
	return err
}

func runCmdOutput(t *testing.T, cmd string, args ...string) (string, error) {
	t.Logf("Running: %s %s", cmd, args)
	c := exec.Command(cmd, args...)
	out, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %s", err, out)
	}
	return string(out), nil
}

func getKindClientset(t *testing.T) (*kubernetes.Clientset, error) {
	kubeconfigData, err := runCmdOutput(t, "kind", "get", "kubeconfig", "--name", kindClusterName)
	if err != nil {
		return nil, fmt.Errorf("getting kind kubeconfig: %w", err)
	}

	config, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfigData))
	if err != nil {
		return nil, fmt.Errorf("parsing kind kubeconfig: %w", err)
	}

	return kubernetes.NewForConfig(config)
}
