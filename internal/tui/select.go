package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// openSelected opens the TUI on an item (the sitting that carries it, the item
// selected) or on a sitting; an unknown id leaves the meetings with a message.
func (m *model) openSelected(id string) tea.Cmd {
	sphere, bare := "", id
	if s, rest, ok := strings.Cut(id, ":"); ok {
		sphere, bare = s, rest
	}
	for _, st := range m.stores {
		if sphere != "" && st.Sphere != sphere {
			continue
		}
		if it, err := st.Item(bare); err == nil {
			m.enter(st.Sphere)
			m.pick = it.ID
			target := it.Sitting
			if target == "" {
				target = it.Meeting
			}
			return m.loadAgenda(target)
		}
	}
	for _, st := range m.stores {
		if sphere != "" && st.Sphere != sphere {
			continue
		}
		if a, err := st.Agenda(bare); err == nil {
			m.enter(st.Sphere)
			return m.loadAgenda(a.Sitting.ID)
		}
	}
	m.setStatus(m.tr("--select : "+id+" introuvable (ni point ni séance)", "--select: "+id+" not found (neither an item nor a sitting)"), true)
	return nil
}

// withoutSelect drops --select and its value from the arguments of a reload,
// so a rebuild keeps where the user went since.
func withoutSelect(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--select":
			i++
		case strings.HasPrefix(a, "--select="):
		default:
			out = append(out, a)
		}
	}
	return out
}
