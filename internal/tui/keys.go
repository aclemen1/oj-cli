package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Keys shared by the ecosystem's TUIs: j k, gg G, / filter, tab and J K for
// the item pane, enter l open, esc h back, 1–9 views, r reload, ? help, q quit.

// keyCommon handles the shared keys. It returns done when the key is used;
// h and l are rewritten to esc and enter for the view's own handler.
func (m *model) keyCommon(k *tea.KeyPressMsg) (tea.Cmd, bool) {
	key := k.String()
	if m.gPending {
		m.gPending = false
		switch key {
		case "g":
			return m.jumpEdge(false), true
		case "r":
			if m.view == vAgenda && m.agenda != nil {
				m.view, m.selR = vStanding, 0
				return m.loadStanding(), true
			}
		}
		return nil, true
	}
	switch key {
	case "g":
		m.gPending = true
		return nil, true
	case "G", "end":
		return m.jumpEdge(true), true
	case "home":
		return m.jumpEdge(false), true
	case "/":
		if m.listView() {
			return m.ask(pFilter, m.tr("filtrer", "filter"), "", m.query), true
		}
	case "tab":
		m.paneOff = !m.paneOff
		return nil, true
	case "r":
		return m.reload(), true
	case "1":
		m.view, m.back = vMeetings, vMeetings
		return m.loadMeetings(), true
	case "2":
		if m.view != vActions {
			m.back = m.view
		}
		m.view = vActions
		return m.loadActions(), true
	case "3":
		return m.openOverview("", vMeetings), true
	case "esc":
		if m.query != "" && m.listView() {
			m.query = ""
			return m.reload(), true
		}
	case "h", "left":
		*k = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "l", "right":
		*k = tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	return nil, false
}

// listView is true for the views whose list / filters.
func (m *model) listView() bool {
	switch m.view {
	case vMeetings, vAgenda, vActions, vSittings:
		return true
	}
	return false
}

// jumpEdge goes to the first or the last line of the view.
func (m *model) jumpEdge(end bool) tea.Cmd {
	last := func(n int) int {
		if end {
			return max(0, n-1)
		}
		return 0
	}
	switch m.view {
	case vMeetings:
		m.selM = last(len(m.meetings))
	case vAgenda:
		m.sel, m.paneScroll = last(len(m.rows)), 0
	case vActions:
		m.selA = last(len(m.actions))
	case vSittings:
		m.selO = last(len(m.ovRows))
	case vStanding:
		m.selR = last(len(m.standing))
	case vLive:
		if m.agenda != nil {
			m.pause()
			m.live.cur, m.paneScroll = last(len(m.agenda.Items)), 0
		}
	case vItem, vDoc:
		m.scroll = 0
		if end {
			m.scroll = 1 << 20
		}
	}
	return nil
}

// matches is true when q is empty or one of the fields contains it, case aside.
func matches(q string, fields ...string) bool {
	if q == "" {
		return true
	}
	q = strings.ToLower(q)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}

// filterOverview keeps the items that match, under their meeting and sitting.
func filterOverview(q string, rows []ovRow) []ovRow {
	if q == "" {
		return rows
	}
	var out []ovRow
	var meeting, sitting *ovRow
	for i := range rows {
		r := rows[i]
		switch {
		case r.item == nil && r.sitting == "":
			meeting, sitting = &rows[i], nil
			continue
		case r.item == nil:
			sitting = &rows[i]
			continue
		}
		if !matches(q, r.item.ID, r.item.Title, r.meeting) {
			continue
		}
		if meeting != nil {
			out = append(out, *meeting)
			meeting = nil
		}
		if sitting != nil {
			out = append(out, *sitting)
			sitting = nil
		}
		out = append(out, r)
	}
	return out
}
