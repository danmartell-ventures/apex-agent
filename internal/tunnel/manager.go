package tunnel

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/danmartell-ventures/apexagent/internal/config"
)

// State represents the tunnel connection state.
type State int

const (
	StateDisconnected State = iota
	StateConnecting
	StateConnected
)

func (s State) String() string {
	switch s {
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	default:
		return "disconnected"
	}
}

// Manager handles the SSH tunnel lifecycle.
type Manager struct {
	cfg    config.TunnelConfig
	hostID string
	log    *slog.Logger

	mu          sync.RWMutex
	state       State
	client      *ssh.Client
	connectedAt time.Time
	forwards    map[Forward]net.Listener
	lastError   error

	stateCallbacks []func(State)
}

// Forward describes a remote-to-local port forward.
type Forward struct {
	RemotePort int
	LocalHost  string
	LocalPort  int
}

func (f Forward) String() string {
	return fmt.Sprintf("R:%d→%s:%d", f.RemotePort, f.LocalHost, f.LocalPort)
}

// NewManager creates a tunnel manager.
func NewManager(cfg config.TunnelConfig, hostID string, log *slog.Logger) *Manager {
	return &Manager{
		cfg:      cfg,
		hostID:   hostID,
		log:      log.With("component", "tunnel"),
		forwards: make(map[Forward]net.Listener),
	}
}

// State returns the current tunnel state.
func (m *Manager) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// ConnectedAt returns when the tunnel was established.
func (m *Manager) ConnectedAt() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connectedAt
}

// LastError returns the last connection error.
func (m *Manager) LastError() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastError
}

// OnStateChange registers a callback for state transitions.
func (m *Manager) OnStateChange(cb func(State)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stateCallbacks = append(m.stateCallbacks, cb)
}

func (m *Manager) setState(s State) {
	m.mu.Lock()
	m.state = s
	callbacks := make([]func(State), len(m.stateCallbacks))
	copy(callbacks, m.stateCallbacks)
	m.mu.Unlock()

	for _, cb := range callbacks {
		cb(s)
	}
}

// Run connects the tunnel and reconnects on failure. Blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) error {
	backoff := newBackoff(time.Second, 60*time.Second)

	for {
		select {
		case <-ctx.Done():
			m.disconnect()
			return ctx.Err()
		default:
		}

		m.setState(StateConnecting)
		err := m.connect(ctx)
		if err != nil {
			m.mu.Lock()
			m.lastError = err
			m.mu.Unlock()
			m.setState(StateDisconnected)
			m.log.Error("tunnel connection failed", "error", err)

			wait := backoff.next()
			m.log.Info("reconnecting", "backoff", wait)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
				continue
			}
		}

		// Connected successfully — reset backoff
		backoff.reset()
		m.setState(StateConnected)
		m.mu.Lock()
		m.connectedAt = time.Now()
		m.mu.Unlock()
		m.log.Info("tunnel connected", "host", m.cfg.ManagementHost, "port", m.cfg.TunnelPort)

		// Re-establish forwards
		if err := m.reestablishForwards(ctx); err != nil {
			m.log.Error("failed to re-establish forwards", "error", err)
		}

		// Wait for connection to drop
		err = m.waitForDisconnect(ctx)
		m.setState(StateDisconnected)
		if ctx.Err() != nil {
			m.disconnect()
			return ctx.Err()
		}
		m.log.Warn("tunnel disconnected", "error", err)
	}
}

func (m *Manager) connect(ctx context.Context) error {
	keyData, err := os.ReadFile(m.cfg.KeyPath)
	if err != nil {
		return fmt.Errorf("reading tunnel key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return fmt.Errorf("parsing tunnel key: %w", err)
	}

	sshCfg := &ssh.ClientConfig{
		User: m.cfg.TunnelUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // Tunnel server, known endpoint
		Timeout:         10 * time.Second,
	}

	addr := fmt.Sprintf("%s:22", m.cfg.ManagementHost)
	m.log.Debug("dialing", "addr", addr)

	// Use a dialer with context for cancellation
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, sshCfg)
	if err != nil {
		conn.Close()
		return fmt.Errorf("ssh handshake: %w", err)
	}

	client := ssh.NewClient(sshConn, chans, reqs)

	// Set up the host tunnel port (reverse forward)
	remoteAddr := fmt.Sprintf("127.0.0.1:%d", m.cfg.TunnelPort)
	listener, err := client.Listen("tcp", remoteAddr)
	if err != nil {
		client.Close()
		return fmt.Errorf("reverse listen on %s: %w", remoteAddr, err)
	}

	m.mu.Lock()
	m.client = client
	m.mu.Unlock()

	// Accept connections on the tunnel port and forward to local SSH
	go m.acceptLoop(ctx, listener)

	return nil
}

func (m *Manager) acceptLoop(ctx context.Context, listener net.Listener) {
	defer listener.Close()
	for {
		remote, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			m.log.Debug("accept error", "error", err)
			return
		}
		go m.forwardToLocalSSH(remote)
	}
}

func (m *Manager) forwardToLocalSSH(remote net.Conn) {
	defer remote.Close()

	local, err := net.DialTimeout("tcp", "127.0.0.1:22", 5*time.Second)
	if err != nil {
		m.log.Error("failed to connect to local SSH", "error", err)
		return
	}
	defer local.Close()

	done := make(chan struct{}, 2)
	copy := func(dst, src net.Conn) {
		buf := make([]byte, 32*1024)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				if _, werr := dst.Write(buf[:n]); werr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		done <- struct{}{}
	}

	go copy(local, remote)
	go copy(remote, local)
	<-done
}

func (m *Manager) disconnect() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for fwd, listener := range m.forwards {
		listener.Close()
		delete(m.forwards, fwd)
	}

	if m.client != nil {
		m.client.Close()
		m.client = nil
	}
}

func (m *Manager) waitForDisconnect(ctx context.Context) error {
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()

	if client == nil {
		return fmt.Errorf("not connected")
	}

	// Use keepalive requests to detect disconnection
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_, _, err := client.SendRequest("keepalive@apex.host", true, nil)
			if err != nil {
				return fmt.Errorf("keepalive failed: %w", err)
			}
		}
	}
}

// AddForward adds a dynamic port forward.
func (m *Manager) AddForward(ctx context.Context, fwd Forward) error {
	m.mu.Lock()
	client := m.client
	m.mu.Unlock()

	if client == nil {
		// Store for later when we reconnect
		m.mu.Lock()
		m.forwards[fwd] = nil
		m.mu.Unlock()
		return nil
	}

	return m.setupForward(ctx, client, fwd)
}

// RemoveForward removes a dynamic port forward.
func (m *Manager) RemoveForward(fwd Forward) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if listener, ok := m.forwards[fwd]; ok {
		if listener != nil {
			listener.Close()
		}
		delete(m.forwards, fwd)
	}
}

// ActiveForwards returns currently configured forwards.
func (m *Manager) ActiveForwards() []Forward {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]Forward, 0, len(m.forwards))
	for fwd := range m.forwards {
		result = append(result, fwd)
	}
	return result
}

func (m *Manager) setupForward(ctx context.Context, client *ssh.Client, fwd Forward) error {
	remoteAddr := fmt.Sprintf("127.0.0.1:%d", fwd.RemotePort)
	listener, err := client.Listen("tcp", remoteAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", remoteAddr, err)
	}

	m.mu.Lock()
	m.forwards[fwd] = listener
	m.mu.Unlock()

	go m.forwardAcceptLoop(ctx, listener, fwd)

	m.log.Info("forward added", "forward", fwd.String())
	return nil
}

func (m *Manager) forwardAcceptLoop(ctx context.Context, listener net.Listener, fwd Forward) {
	defer listener.Close()

	for {
		remote, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			return
		}

		go func() {
			defer remote.Close()
			localAddr := fmt.Sprintf("%s:%d", fwd.LocalHost, fwd.LocalPort)
			local, err := net.DialTimeout("tcp", localAddr, 5*time.Second)
			if err != nil {
				m.log.Debug("forward dial failed", "target", localAddr, "error", err)
				return
			}
			defer local.Close()

			done := make(chan struct{}, 2)
			cp := func(dst, src net.Conn) {
				buf := make([]byte, 32*1024)
				for {
					n, rerr := src.Read(buf)
					if n > 0 {
						if _, werr := dst.Write(buf[:n]); werr != nil {
							break
						}
					}
					if rerr != nil {
						break
					}
				}
				done <- struct{}{}
			}
			go cp(local, remote)
			go cp(remote, local)
			<-done
		}()
	}
}

func (m *Manager) reestablishForwards(ctx context.Context) error {
	m.mu.RLock()
	client := m.client
	fwds := make([]Forward, 0, len(m.forwards))
	for fwd := range m.forwards {
		fwds = append(fwds, fwd)
	}
	m.mu.RUnlock()

	if client == nil {
		return fmt.Errorf("not connected")
	}

	for _, fwd := range fwds {
		if err := m.setupForward(ctx, client, fwd); err != nil {
			m.log.Error("failed to re-establish forward", "forward", fwd.String(), "error", err)
		}
	}
	return nil
}

// LoadForwards reads forwards.conf and adds them.
func (m *Manager) LoadForwards(path string) error {
	fwds, err := ParseForwardsFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	m.mu.Lock()
	for _, fwd := range fwds {
		m.forwards[fwd] = nil
	}
	m.mu.Unlock()

	m.log.Info("loaded forwards", "count", len(fwds))
	return nil
}

// Reconnect forces an immediate reconnection.
func (m *Manager) Reconnect() {
	m.mu.Lock()
	if m.client != nil {
		m.client.Close()
		m.client = nil
	}
	m.mu.Unlock()
}
