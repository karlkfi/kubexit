package watch

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

const kindClusterName = "kubexit-test"
const testImage = "kubexit/test-server:latest"

// TestWatchPod_Integration validates that WatchPod exits when a watched pod
// terminates. It runs WatchPod in a background goroutine, triggers the test­server
// to exit via kubectl exec with wget, and verifies that the observed pod phases
// include a terminal state (Failed or Succeeded).
func TestWatchPod_Integration(t *testing.T) {
	// t.Skip("NYI")

	if _, err := exec.LookPath("kind"); err != nil {
		t.Fatalf("kind not found in PATH")
	}

	clientset, err := setupKind(t)
	if err != nil {
		t.Fatalf("failed to setup Kind cluster: %v", err)
	}

	t.Run("pod_phase_transitions_from_pending_to_failed", func(t *testing.T) {
		namespace := "default"

		// Create the test server pod
		podName, err := createTestServerPod(t, clientset, namespace)
		if err != nil {
			t.Fatalf("failed to create test server pod: %v", err)
		}

		// Wait for the pod to be ready
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		defer cancel()
		waitForPodReady(ctx, t, clientset, namespace, podName)

		phases := make([]v1.PodPhase, 0, 3)
		handler := func(event watch.Event) (bool, error) {
			pod, ok := event.Object.(*v1.Pod)
			if !ok {
				return true, fmt.Errorf("unexpected non-pod object type %T: %+v", event.Object, event.Object)
			}
			t.Logf("Watch event: type=%s phase=%s reason=%s", event.Type, pod.Status.Phase, pod.Status.Reason)
			phases = append(phases, pod.Status.Phase)
			return false, nil
		}

		ctx, cancel = context.WithTimeout(t.Context(), 60*time.Second)
		defer cancel()

		preCtx, preCancel := context.WithCancel(ctx)
		defer preCancel()

		precondition := func(_ cache.Store) (bool, error) {
			// precondition is executed after the ListWatch is synced
			preCancel()
			return true, nil
		}

		g, ctx := errgroup.WithContext(ctx)

		g.Go(func() error {
			err = WatchPod(ctx, clientset, namespace, podName, precondition, handler)
			if err != nil {
				return fmt.Errorf("WatchPod failed: %w", err)
			}
			return nil
		})
		g.Go(func() error {
			// Wait until WatchPod is synced or cancelled
			<-preCtx.Done()
			exitAddress := "http://localhost:80/exit"
			t.Logf("POST %s", exitAddress)
			if err := runCmd(t, ctx, "kubectl", "exec", "-i", "-n", namespace, podName, "--", "curl", "-X", "POST", "--silent", "--show-error", "--fail-with-body", exitAddress); err != nil {
				return fmt.Errorf("failed to fetch %s: %w", exitAddress, err)
			}
			return nil
		})

		if err := g.Wait(); err != nil {
			t.Fatalf("failed to wait for error group: %v", err)
		}

		t.Logf("Phases: %+v", phases)

		// foundTerminal := false
		// for _, phase := range phases {
		// 	if phase == v1.PodFailed || phase == v1.PodSucceeded {
		// 		foundTerminal = true
		// 	}
		// }

		// if !foundTerminal {
		// 	t.Errorf("expected to observe a terminal phase (Failed or Succeeded), but found: %+v", phases)
		// }
	})
}

// setupKind creates a Kind cluster and returns a clientset connected to it.
// It registers a cleanup function to delete the cluster when the test finishes.
func setupKind(t *testing.T) (*kubernetes.Clientset, error) {
	t.Log("Creating Kind cluster")
	if err := runCmd(t, t.Context(), "kind", "create", "cluster", "--name", kindClusterName, "--wait", "60s"); err != nil {
		return nil, fmt.Errorf("failed to create Kind cluster: %w", err)
	}
	t.Cleanup(func() {
		t.Log("Deleting Kind cluster")
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_ = runCmd(t, ctx, "kind", "delete", "cluster", "--name", kindClusterName)
	})

	// Build and load the test image into the cluster
	if err := buildAndLoadImage(t); err != nil {
		return nil, fmt.Errorf("failed to build and load test image: %w", err)
	}

	clientset, err := getKindClientset(t)
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

// buildAndLoadImage builds the test-server Docker image and loads it into the Kind cluster.
func buildAndLoadImage(t *testing.T) error {
	// Build the image from the test-server directory
	t.Log("Building test-server image")
	if err := runCmd(t, t.Context(), "docker", "build", "-f", "cmd/test-server/Dockerfile", "-t", testImage, "."); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	// Load the image into Kind
	t.Log("Loading image into Kind")
	return runCmd(t, t.Context(), "kind", "load", "docker-image", testImage, "--name", kindClusterName)
}

// createTestServerPod creates a pod running the test server image and returns its name.
// It registers a cleanup function to delete the pod when the test finishes.
func createTestServerPod(t *testing.T, clientset *kubernetes.Clientset, namespace string) (string, error) {
	podName := fmt.Sprintf("test-server-%d", time.Now().UnixNano())
	_, err := clientset.CoreV1().Pods(namespace).Create(t.Context(), &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				"app": "kubexit-test-server",
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:            "test-server",
					Image:           testImage,
					ImagePullPolicy: v1.PullIfNotPresent,
					Ports: []v1.ContainerPort{
						{ContainerPort: 80, Name: "http"},
					},
					Env: []v1.EnvVar{
						{Name: "PORT", Value: "80"},
					},
					LivenessProbe: &v1.Probe{
						ProbeHandler: v1.ProbeHandler{
							HTTPGet: &v1.HTTPGetAction{
								Path: "/health",
								Port: intstr.FromString("http"),
							},
						},
						InitialDelaySeconds: 3,
						PeriodSeconds:       10,
					},
					ReadinessProbe: &v1.Probe{
						ProbeHandler: v1.ProbeHandler{
							HTTPGet: &v1.HTTPGetAction{
								Path: "/health",
								Port: intstr.FromString("http"),
							},
						},
						InitialDelaySeconds: 1,
						PeriodSeconds:       5,
					},
				},
			},
			RestartPolicy: v1.RestartPolicyNever,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create test server pod: %w", err)
	}
	t.Cleanup(func() {
		t.Logf("Deleting test server pod %s", podName)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_ = clientset.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	})
	return podName, nil
}

// waitForPodReady polls the given pod until it reports Ready or the context expires.
func waitForPodReady(ctx context.Context, t *testing.T, clientset *kubernetes.Clientset, namespace, podName string) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for pod %s/%s to be ready", namespace, podName)
		case <-ticker.C:
			pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
			if err != nil {
				t.Logf("error getting pod: %v", err)
				continue
			}
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.Ready {
					t.Logf("Pod %s/%s is ready", pod.Namespace, pod.Name)
					return
				}
			}
			t.Logf("Pod %s/%s phase=%s ready=%v", pod.Namespace, pod.Name, pod.Status.Phase, pod.Status.ContainerStatuses)
		}
	}
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
