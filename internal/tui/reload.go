package tui

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"github.com/aclemen1/oj-cli/internal/actions"
)

// reloadedEnv tells the new process that it replaces a TUI after a rebuild.
const reloadedEnv = "OJ_TUI_RELOADED"

// reloadMsg asks for a reload (SIGUSR1), under the same rules as a rebuild.
type reloadMsg struct{}

// self is the running binary, followed through symlinks.
func self() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// binStamp is the binary's mtime and inode; a rebuild changes one of them.
func binStamp(path string) string {
	if path == "" {
		return ""
	}
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	var ino uint64
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		ino = uint64(st.Ino)
	}
	return fmt.Sprintf("%d/%d", fi.ModTime().UnixNano(), ino)
}

// buildLabel is the version and the build time of a binary.
func buildLabel(path string) string {
	label := "v" + actions.Version
	if fi, err := os.Stat(path); err == nil {
		label += " · " + fi.ModTime().Format("02.01 15:04:05")
	}
	return label
}

// idle is true when nothing is being typed, picked or edited.
func (m *model) idle() bool {
	return m.prompt == pNone && m.moving == nil && !m.editing
}

// maybeReload quits for a re-exec once a new binary waits and the TUI is idle.
func (m *model) maybeReload() tea.Cmd {
	if !m.newBin || !m.idle() || m.exe == "" {
		return nil
	}
	m.reexec = true
	return tea.Quit
}

// run starts the program; SIGUSR1 asks for a reload, and a reload replaces
// the process with the binary on disk, which restores the saved state.
func run(m *model) error {
	p := tea.NewProgram(m)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGUSR1)
	defer signal.Stop(sig)
	go func() {
		for range sig {
			p.Send(reloadMsg{})
		}
	}()
	if _, err := p.Run(); err != nil {
		return err
	}
	if !m.reexec {
		return nil
	}
	m.persist()
	signal.Stop(sig)
	return syscall.Exec(m.exe, withoutSelect(os.Args), append(os.Environ(), reloadedEnv+"="+buildLabel(m.exe)))
}
