package sandbox

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// mockRuntime is a fake sandbox.Runtime for testing the pool.
type mockRuntime struct {
	mu       sync.Mutex
	started  int
	stopped  []string
	running  map[string]bool
}

func newMockRuntime() *mockRuntime {
	return &mockRuntime{running: make(map[string]bool)}
}

func (m *mockRuntime) Start(_ context.Context, opts StartOptions) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started++
	id := fmt.Sprintf("container-%d", m.started)
	m.running[id] = true
	return id, nil
}

func (m *mockRuntime) Stop(_ context.Context, containerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = append(m.stopped, containerID)
	delete(m.running, containerID)
	return nil
}

func (m *mockRuntime) Wait(_ context.Context, _ string) (int, error)    { return 0, nil }
func (m *mockRuntime) StreamLogs(_ context.Context, _ string) (LineScanner, error) { return nil, nil }
func (m *mockRuntime) Exec(_ context.Context, _ string, _ []string) (LineScanner, error) { return nil, nil }
func (m *mockRuntime) ExecCollect(_ context.Context, _ string, _ []string) (string, error) { return "", nil }
func (m *mockRuntime) CommitAndPush(_ context.Context, _, _, _ string) error { return nil }
func (m *mockRuntime) EnsureNetwork(_ context.Context, _ string) error { return nil }
func (m *mockRuntime) IsRunning(_ context.Context, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running[id]
}

func (m *mockRuntime) getStarted() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

func TestPoolPrewarms(t *testing.T) {
	mock := newMockRuntime()
	pool := NewPool(mock, PoolConfig{
		PoolSize:       2,
		Image:          "test-image",
		RefillInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool.StartPool(ctx)
	defer pool.StopPool()

	// Wait for the pool to fill.
	time.Sleep(200 * time.Millisecond)

	if pool.PoolStats() != 2 {
		t.Fatalf("expected 2 warm containers, got %d", pool.PoolStats())
	}
}

func TestPoolClaimsWarmContainer(t *testing.T) {
	mock := newMockRuntime()
	pool := NewPool(mock, PoolConfig{
		PoolSize:       2,
		Image:          "test-image",
		RefillInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool.StartPool(ctx)
	defer pool.StopPool()

	// Wait for pool to fill.
	time.Sleep(200 * time.Millisecond)

	initialStarts := mock.getStarted()

	// Claim a warm container.
	id, err := pool.Start(ctx, StartOptions{
		SessionID: "test-session",
		Repo:      "owner/repo",
		Branch:    "telecoder/abc",
		Image:     "test-image",
	})
	if err != nil {
		t.Fatalf("start error: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty container ID")
	}

	// The pool should have used an existing container (no new Start call beyond pool refills).
	if pool.PoolStats() != 1 {
		t.Fatalf("expected 1 warm container after claiming one, got %d", pool.PoolStats())
	}

	// Wait for pool to refill.
	time.Sleep(200 * time.Millisecond)
	if pool.PoolStats() != 2 {
		t.Fatalf("expected pool to refill to 2, got %d", pool.PoolStats())
	}

	// Total starts = 2 (initial warm) + 1 (refill after claim) = 3.
	// But the claim itself should NOT have called inner.Start.
	// It should have called inner.Start only for the refill.
	finalStarts := mock.getStarted()
	if finalStarts != initialStarts+1 {
		t.Fatalf("expected exactly 1 additional Start for refill, got %d (initial=%d, final=%d)",
			finalStarts-initialStarts, initialStarts, finalStarts)
	}
}

func TestPoolFallsBackToColdStart(t *testing.T) {
	mock := newMockRuntime()
	pool := NewPool(mock, PoolConfig{
		PoolSize:       0, // no pre-warming
		Image:          "test-image",
		RefillInterval: 1 * time.Hour, // effectively disabled
	})

	ctx := context.Background()

	id, err := pool.Start(ctx, StartOptions{
		SessionID: "test-session",
		Repo:      "owner/repo",
		Branch:    "telecoder/abc",
		Image:     "test-image",
	})
	if err != nil {
		t.Fatalf("start error: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty container ID")
	}

	// Should have done a cold start.
	if mock.getStarted() != 1 {
		t.Fatalf("expected 1 start, got %d", mock.getStarted())
	}
}

func TestPoolStopCleansUp(t *testing.T) {
	mock := newMockRuntime()
	pool := NewPool(mock, PoolConfig{
		PoolSize:       3,
		Image:          "test-image",
		RefillInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	pool.StartPool(ctx)

	// Wait for pool to fill.
	time.Sleep(200 * time.Millisecond)
	if pool.PoolStats() != 3 {
		t.Fatalf("expected 3 warm, got %d", pool.PoolStats())
	}

	cancel()
	pool.StopPool()

	// All warm containers should be cleaned up.
	if pool.PoolStats() != 0 {
		t.Fatalf("expected 0 warm after stop, got %d", pool.PoolStats())
	}

	mock.mu.Lock()
	stoppedCount := len(mock.stopped)
	mock.mu.Unlock()
	if stoppedCount != 3 {
		t.Fatalf("expected 3 stopped containers, got %d", stoppedCount)
	}
}
