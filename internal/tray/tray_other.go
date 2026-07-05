//go:build !windows

// Non-Windows stub: the tray feature is Windows-only, but the package must
// compile everywhere because internal/cli imports it unconditionally.
package tray

type Manager struct{}

func IsAvailable() bool { return false }

func New(_, _ func()) *Manager { return &Manager{} }

func (*Manager) Show()    {}
func (*Manager) Restore() {}
func (*Manager) Stop()    {}
