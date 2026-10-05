package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
)

// saved is what the TUI keeps across restarts, per sphere.
type saved struct {
	View     view   `json:"view"`
	Back     view   `json:"back"`
	Meeting  string `json:"meeting,omitempty"`
	Sitting  string `json:"sitting,omitempty"`
	Item     string `json:"item,omitempty"`
	ItemView string `json:"item_view,omitempty"`
	HelpOff  bool   `json:"help_off,omitempty"`
	ShowDone bool   `json:"show_done,omitempty"`
	OvScope  string `json:"ov_scope,omitempty"`
	SelO     int    `json:"sel_o,omitempty"`
	SelA     int    `json:"sel_a,omitempty"`
	LiveItem string `json:"live_item,omitempty"`
	// Spent is the timer of each item in seconds; Since is when the running timer started.
	Spent   map[string]float64 `json:"spent,omitempty"`
	Running bool               `json:"running,omitempty"`
	Since   time.Time          `json:"since,omitzero"`
}

// statePath is $XDG_STATE_HOME/oj/tui-<sphere>.json, or ~/.local/state/oj/….
func statePath(sphere string) string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "oj", "tui-"+sphere+".json")
}

func (m *model) snapshot() saved {
	s := saved{View: m.view, Back: m.back, HelpOff: m.helpOff, ShowDone: m.showDone,
		OvScope: m.ovScope, SelO: m.selO, SelA: m.selA}
	if m.selM < len(m.meetings) {
		s.Meeting = m.meetings[m.selM].alias
	}
	if m.agenda != nil {
		s.Sitting = m.agenda.Sitting.ID
		if r := m.current(); r != nil {
			s.Item = r.item.ID
		}
		if it := m.liveItem(); it != nil {
			s.LiveItem = it.ID
		}
	}
	if m.item != nil {
		s.ItemView = m.item.ID
	}
	if len(m.live.spent) > 0 || m.live.running {
		s.Spent = map[string]float64{}
		for k, v := range m.live.spent {
			s.Spent[k] = v.Seconds()
		}
		s.Running, s.Since = m.live.running, m.live.since
	}
	return s
}

// persist writes the state when it changed.
func (m *model) persist() {
	if m.statePath == "" {
		return
	}
	b, err := json.Marshal(m.snapshot())
	if err != nil || string(b) == m.lastSaved {
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.statePath), 0o755); err != nil {
		return
	}
	tmp := m.statePath + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil && os.Rename(tmp, m.statePath) == nil {
		m.lastSaved = string(b)
	}
}

// restore reads the saved state and returns the commands that bring the
// views back; the agenda and the item load as at the time of the save.
func (m *model) restore() tea.Cmd {
	if m.statePath == "" {
		return nil
	}
	b, err := os.ReadFile(m.statePath)
	if err != nil {
		return nil
	}
	var s saved
	if json.Unmarshal(b, &s) != nil {
		return nil
	}
	m.lastSaved = string(b)
	m.helpOff, m.showDone, m.ovScope, m.selO, m.selA = s.HelpOff, s.ShowDone, s.OvScope, s.SelO, s.SelA
	m.restoreMeeting = s.Meeting
	m.live.spent = map[string]time.Duration{}
	for k, v := range s.Spent {
		m.live.spent[k] = time.Duration(v * float64(time.Second))
	}
	m.live.running, m.live.since = s.Running, s.Since
	m.restoreLive = s.LiveItem
	var cmds []tea.Cmd
	needsAgenda := s.Sitting != "" && (s.View == vAgenda || s.View == vLive || s.Back == vAgenda ||
		(s.View == vItem && s.Back != vSittings))
	if needsAgenda {
		m.pick = s.Item
		m.keepView = true
		cmds = append(cmds, m.loadAgenda(s.Sitting))
	}
	m.view, m.back = s.View, s.Back
	switch s.View {
	case vItem:
		if s.ItemView != "" {
			cmds = append(cmds, m.loadItem(s.ItemView))
		} else {
			m.view = vMeetings
		}
	case vActions:
		cmds = append(cmds, m.loadActions())
	case vSittings:
		cmds = append(cmds, m.loadOverview())
	case vAgenda, vLive:
		if !needsAgenda {
			m.view = vMeetings
		}
	}
	if m.view == vLive && m.live.running {
		cmds = append(cmds, tick())
	}
	return tea.Batch(cmds...)
}
