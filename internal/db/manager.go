package db

import (
	"context"
	"fmt"
	"sync"

	"sql-proxy/internal/config"
)

// Manager manages multiple database connections
type Manager struct {
	connections map[string]Driver
	mu          sync.RWMutex
}

// NewManager creates a new connection manager from database configs
func NewManager(configs []config.DatabaseConfig) (*Manager, error) {
	m := &Manager{
		connections: make(map[string]Driver),
	}

	for _, cfg := range configs {
		driver, err := NewDriver(cfg)
		if err != nil {
			// Clean up any connections we've already made
			m.Close()
			return nil, fmt.Errorf("failed to connect to database %s: %w", cfg.Name, err)
		}

		m.connections[cfg.Name] = driver
	}

	return m, nil
}

// Get returns the database driver with the given name
func (m *Manager) Get(name string) (Driver, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	driver, ok := m.connections[name]
	if !ok {
		return nil, fmt.Errorf("unknown database connection: %s", name)
	}
	return driver, nil
}

// Names returns all connection names
func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.connections))
	for name := range m.connections {
		names = append(names, name)
	}
	return names
}

// IsReadOnly returns whether the named connection is read-only
func (m *Manager) IsReadOnly(name string) (bool, error) {
	driver, err := m.Get(name)
	if err != nil {
		return false, err
	}
	return driver.IsReadOnly(), nil
}

// Close closes all database connections
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for name, driver := range m.connections {
		if err := driver.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to close connection %s: %w", name, err)
		}
	}
	m.connections = make(map[string]Driver)
	return firstErr
}

// Ping checks connectivity to all databases
// Returns a map of connection name -> error (nil if healthy)
func (m *Manager) Ping(ctx context.Context) map[string]error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make(map[string]error)
	for name, driver := range m.connections {
		results[name] = driver.Ping(ctx)
	}
	return results
}

// PingAll returns true if all connections are healthy
func (m *Manager) PingAll(ctx context.Context) bool {
	results := m.Ping(ctx)
	for _, err := range results {
		if err != nil {
			return false
		}
	}
	return true
}

// Reconnect attempts to reconnect a specific database
func (m *Manager) Reconnect(name string) error {
	m.mu.RLock()
	driver, ok := m.connections[name]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("unknown database connection: %s", name)
	}
	return driver.Reconnect()
}

// ReconnectAll attempts to reconnect all databases
func (m *Manager) ReconnectAll() map[string]error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make(map[string]error)
	for name, driver := range m.connections {
		results[name] = driver.Reconnect()
	}
	return results
}

// Count returns the number of configured connections
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.connections)
}
