package sandbox

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// PoolConfig configures the pre-warming pool.
type PoolConfig struct {
	// PoolSize is the number of warm containers to maintain (default 2).
	PoolSize int
	// Image is the Docker image to pre-warm with.
	Image string
	// Network is the Docker network for pre-warmed containers.
	Network string
	// Env is the base environment variables for pre-warmed containers.
	Env []string
	// RefillInterval is how often to check and refill the pool (default 10s).
	RefillInterval time.Duration
}

// Pool wraps a Runtime and maintains a pool of pre-created containers
// for near-instant session startup.
type Pool struct {
	inner  Runtime
	config PoolConfig

	mu    sync.Mutex
	warm  []string // pre-created container IDs
	ctx   context.Context
	cancel context.CancelFunc
	wg    sync.WaitGroup
}

// NewPool creates a pre-warming pool around the given runtime.
func NewPool(inner Runtime, cfg PoolConfig) *Pool {
	if cfg.PoolSize <= 0 {
		cfg.PoolSize = 2
	}
	if cfg.RefillInterval <= 0 {
		cfg.RefillInterval = 10 * time.Second
	}
	return &Pool{
		inner:  inner,
		config: cfg,
	}
}

// StartPool begins the background refill loop. Call StopPool to shut down.
func (p *Pool) StartPool(ctx context.Context) {
	p.ctx, p.cancel = context.WithCancel(ctx)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.refillLoop()
	}()
}

// StopPool stops the refill loop and cleans up warm containers.
func (p *Pool) StopPool() {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()

	// Clean up any remaining warm containers.
	p.mu.Lock()
	warm := p.warm
	p.warm = nil
	p.mu.Unlock()

	ctx := context.Background()
	for _, id := range warm {
		if err := p.inner.Stop(ctx, id); err != nil {
			log.Printf("pool: failed to clean up warm container %s: %v", shortID(id), err)
		}
	}
}

// PoolStats returns the current number of warm containers.
func (p *Pool) PoolStats() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.warm)
}

// Start claims a pre-warmed container if available, or falls back to
// creating a fresh one. The claimed container is reconfigured with
// the session's environment variables.
func (p *Pool) Start(ctx context.Context, opts StartOptions) (string, error) {
	containerID := p.claimWarm()
	if containerID != "" {
		log.Printf("pool: claimed pre-warmed container %s for session %s", shortID(containerID), opts.SessionID)
		// Reconfigure the warm container with session-specific env and start the workload.
		err := p.reconfigure(ctx, containerID, opts)
		if err != nil {
			// Reconfigure failed — stop this container and fall back.
			log.Printf("pool: reconfigure failed, falling back to cold start: %v", err)
			p.inner.Stop(ctx, containerID)
			return p.inner.Start(ctx, opts)
		}
		return containerID, nil
	}

	// No warm container available — cold start.
	return p.inner.Start(ctx, opts)
}

func (p *Pool) Stop(ctx context.Context, containerID string) error {
	return p.inner.Stop(ctx, containerID)
}

func (p *Pool) Wait(ctx context.Context, containerID string) (int, error) {
	return p.inner.Wait(ctx, containerID)
}

func (p *Pool) StreamLogs(ctx context.Context, containerID string) (LineScanner, error) {
	return p.inner.StreamLogs(ctx, containerID)
}

func (p *Pool) Exec(ctx context.Context, containerID string, cmd []string) (LineScanner, error) {
	return p.inner.Exec(ctx, containerID, cmd)
}

func (p *Pool) ExecCollect(ctx context.Context, containerID string, cmd []string) (string, error) {
	return p.inner.ExecCollect(ctx, containerID, cmd)
}

func (p *Pool) CommitAndPush(ctx context.Context, containerID, message, branch string) error {
	return p.inner.CommitAndPush(ctx, containerID, message, branch)
}

func (p *Pool) EnsureNetwork(ctx context.Context, name string) error {
	return p.inner.EnsureNetwork(ctx, name)
}

func (p *Pool) IsRunning(ctx context.Context, containerID string) bool {
	return p.inner.IsRunning(ctx, containerID)
}

// claimWarm pops a container from the warm pool. Returns "" if none available.
func (p *Pool) claimWarm() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.warm) == 0 {
		return ""
	}
	id := p.warm[0]
	p.warm = p.warm[1:]
	return id
}

// reconfigure injects session-specific env vars and starts the entrypoint
// inside a pre-warmed container (which was started with `sleep infinity`).
func (p *Pool) reconfigure(ctx context.Context, containerID string, opts StartOptions) error {
	// Build environment export commands.
	envExports := fmt.Sprintf("export TELECODER_SESSION_ID=%q TELECODER_REPO=%q TELECODER_BRANCH=%q",
		opts.SessionID, opts.Repo, opts.Branch)
	if opts.Prompt != "" {
		envExports += fmt.Sprintf(" TELECODER_PROMPT=%q", opts.Prompt)
	}
	for _, e := range opts.Env {
		envExports += fmt.Sprintf(" %s", e)
	}

	// Run the entrypoint in the background inside the warm container.
	cmd := fmt.Sprintf("%s && /entrypoint.sh", envExports)
	_, err := p.inner.Exec(ctx, containerID, []string{"bash", "-c", cmd})
	return err
}

func (p *Pool) refillLoop() {
	// Initial fill.
	p.refill()

	ticker := time.NewTicker(p.config.RefillInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.refill()
		}
	}
}

func (p *Pool) refill() {
	p.mu.Lock()
	deficit := p.config.PoolSize - len(p.warm)
	p.mu.Unlock()

	for i := 0; i < deficit; i++ {
		id, err := p.createWarm()
		if err != nil {
			log.Printf("pool: failed to pre-warm container: %v", err)
			return
		}
		p.mu.Lock()
		p.warm = append(p.warm, id)
		p.mu.Unlock()
		log.Printf("pool: pre-warmed container %s (%d/%d)", shortID(id), p.PoolStats(), p.config.PoolSize)
	}
}

// shortID returns the first 12 characters of an ID, or the full ID if shorter.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// createWarm creates a sleeping container with the base image.
func (p *Pool) createWarm() (string, error) {
	return p.inner.Start(p.ctx, StartOptions{
		SessionID:  fmt.Sprintf("warm-%d", time.Now().UnixNano()),
		Persistent: true, // starts with `sleep infinity`
		Image:      p.config.Image,
		Env:        p.config.Env,
		Network:    p.config.Network,
	})
}
