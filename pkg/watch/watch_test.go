package watch

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
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
	redirectLogs(t)

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

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		// Wait for the pod to be ready
		ctxReady, cancelReady := context.WithTimeout(ctx, 60*time.Second)
		defer cancelReady()
		waitForPodReady(ctxReady, t, clientset, namespace, podName)

		// Determine if a pod is in a terminal phase (not deleted from API yet).
		isTerminal := func(pod *corev1.Pod) bool {
			return pod != nil && (pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded)
		}

		phasesCh := make(chan corev1.PodPhase, 10)
		handler := func(event watch.Event) (bool, error) {
			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				return true, fmt.Errorf("unexpected non-pod object type %T: %+v", event.Object, event.Object)
			}
			t.Logf("Watch event: type=%s phase=%s reason=%s", event.Type, pod.Status.Phase, pod.Status.Reason)
			select {
			case <-ctx.Done():
				t.Logf("Context cancelled before phase read from phasesCh: %s\n", pod.Status.Phase)
				return true, nil
			case phasesCh <- pod.Status.Phase:
				if isTerminal(pod) {
					t.Logf("Terminal phase reached: %s\n", pod.Status.Phase)
					return true, nil
				}
			}
			return false, nil // continue watching
		}

		ctxWatchPod, cancelWatchPod := context.WithTimeout(ctx, 60*time.Second)
		defer cancelWatchPod()

		syncCtx, syncCancel := context.WithCancel(ctxWatchPod)
		defer syncCancel()

		onSync := func(s cache.Store) (bool, error) {
			// precondition is executed after the ListWatch is synced
			defer syncCancel()
			obj, exists, err := s.GetByKey(cache.ObjectName{Namespace: namespace, Name: podName}.String())
			if err != nil {
				return true, fmt.Errorf("Failed to lookup Pod at sync time: %w", err)
			}
			if exists {
				pod, ok := obj.(*corev1.Pod)
				if !ok {
					return true, fmt.Errorf("unexpected non-pod object type %T: %+v", obj, obj)
				}
				t.Logf("Synced Pod Phase: %+v", pod.Status.Phase)
			} else {
				return true, fmt.Errorf("Pod not found at sync time: %w", err)
			}
			return false, nil // continue watching
		}

		t.Log("WatchPod starting")
		err = WatchPod(ctxWatchPod, clientset, namespace, podName, onSync, handler)
		if err != nil {
			t.Fatalf("WatchPod failed: %v", err)
		}
		t.Log("WatchPod returned without error")

		t.Log("Waiting for sync or timeout...")
		select {
		case <-syncCtx.Done():
			t.Log("Sync complete")
		case <-ctxWatchPod.Done():
			t.Fatal("timed out waiting for ctxWatchPod to complete")
		}

		g, ctxGroup := errgroup.WithContext(ctxWatchPod)

		g.Go(func() error {
			exitAddress := "http://localhost:80/exit"
			exitCode := 1
			t.Logf("POST %s?exit_code=%d", exitAddress, exitCode)
			// kubectl exec -i -n default test-server -- curl -X POST -d exit_code=1 --silent --show-error --fail-with-body http://localhost:80/exit
			if err := runCmd(t, ctxGroup,
				"kubectl", "exec", "-i", "-n", namespace, podName, "--",
				"curl", "-X", "POST", "-d", fmt.Sprintf("exit_code=%d", exitCode), "--silent", "--show-error", "--fail-with-body", exitAddress,
			); err != nil {
				return fmt.Errorf("failed to POST %s: %w", exitAddress, err)
			}
			return nil
		})

		var phases []corev1.PodPhase
		g.Go(func() error {
			ctxWait, cancelWait := context.WithTimeout(ctx, 30*time.Second)
			defer cancelWait()
			for {
				select {
				case <-ctxWait.Done():
					return fmt.Errorf("waiting for pod terminal condition: %w", ctxWait.Err())
				case phase, ok := <-phasesCh:
					if !ok {
						return fmt.Errorf("phasesCh closed early")
					}
					phases = append(phases, phase)
					t.Logf("Read Pod phase: %v\n", phase)
					if phase == corev1.PodFailed || phase == corev1.PodSucceeded {
						return nil
					}
				}
			}
		})

		if err := g.Wait(); err != nil {
			t.Errorf("failed to wait for error group: %v", err)
			pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("error getting pod: %v", err)
			}
			t.Fatalf("Pod Phase: %s", pod.Status.Phase)
		}

		// Stop WatchPod
		cancelWatchPod()
		// TODO: Wait until WatchPod is actually done. Need doneCh from WatchPod.
		close(phasesCh)

		t.Logf("Phases: %+v", phases)

		// foundTerminal := false
		// for _, phase := range phases {
		// 	if phase == corev1.PodFailed || phase == corev1.PodSucceeded {
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
	// docker build -f cmd/test-server/Dockerfile -t kubexit/test-server:latest .
	if err := runCmd(t, t.Context(), "docker", "build", "-f", "cmd/test-server/Dockerfile", "-t", testImage, "."); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	// Load the image into Kind
	t.Log("Loading image into Kind")
	// kind load docker-image kubexit/test-server:latest --name kubexit-test
	return runCmd(t, t.Context(), "kind", "load", "docker-image", testImage, "--name", kindClusterName)
}

// createTestServerPod creates a pod running the test server image and returns its name.
// It registers a cleanup function to delete the pod when the test finishes.
func createTestServerPod(t *testing.T, clientset *kubernetes.Clientset, namespace string) (string, error) {
	podName := fmt.Sprintf("test-server-%d", time.Now().UnixNano())
	_, err := clientset.CoreV1().Pods(namespace).Create(t.Context(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				"app": "kubexit-test-server",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "test-server",
					Image:           testImage,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Ports: []corev1.ContainerPort{
						{ContainerPort: 80, Name: "http"},
					},
					Env: []corev1.EnvVar{
						{Name: "PORT", Value: "80"},
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/health",
								Port: intstr.FromString("http"),
							},
						},
						InitialDelaySeconds: 3,
						PeriodSeconds:       10,
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/health",
								Port: intstr.FromString("http"),
							},
						},
						InitialDelaySeconds: 1,
						PeriodSeconds:       5,
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
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
