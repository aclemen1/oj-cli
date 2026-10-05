package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/aclemen1/ordo-cli/internal/store"
)

func (m *model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m *model) render() string {
	var lines []string
	var help string
	switch m.view {
	case vMeetings:
		lines, help = m.renderMeetings(), helpLine("enter", "agenda", "A", "actions", "R", "refresh", "q", "quit")
	case vAgenda:
		lines, help = m.renderAgenda(), helpLine("n", "new", "a", "accept", "d", "defer", "x", "drop", "J/K", "move", "+/-", "5 min",
			"e", "edit", "f", "freeze", "r", "reopen", "l", "live", "h", "hold", "m", "minutes", "[/]", "sitting", "esc", "back")
	case vItem:
		lines, help = m.renderItem(), helpLine("e", "edit", "j/k", "scroll", "esc", "back")
	case vLive:
		lines, help = m.renderLive(), helpLine("space", "timer", "n/p", "next/previous", "s", "summary", "D", "decision",
			"t", "action", "-", "defer", "h", "hold", "esc", "agenda")
	case vActions:
		lines, help = m.renderActions(), helpLine("space", "done/open", "o", "show done", "esc", "back")
	}
	foot := help
	switch {
	case m.prompt != pNone:
		foot = m.input.View()
	case m.status != "" && m.statusErr:
		foot = sErr.Render(m.status)
	case m.status != "":
		foot = sOK.Render(m.status)
	}
	head := sTitle.Render("ordo") + " " + sphereTag(m.st.Sphere)
	return pad(head, m.w) + "\n" + block(lines, m.w, m.h-2) + "\n" + pad(foot, m.w)
}

// window keeps the selected line on screen.
func window(lines []string, sel, h int) []string {
	if len(lines) <= h || sel < h {
		return lines
	}
	return lines[sel-h+1:]
}

func (m *model) renderMeetings() []string {
	if len(m.meetings) == 0 {
		return []string{"", sMuted.Render("  No meeting yet. Create one: ordo meeting add RDIR --title … --sphere " + m.st.Sphere)}
	}
	var out []string
	for i, r := range m.meetings {
		next := sMuted.Render("no upcoming sitting")
		if r.next != "" {
			next = fmt.Sprintf("%s  %s on the agenda", r.next, sBold.Render(fmt.Sprint(r.accepted)))
			if r.proposed > 0 {
				next += "  " + sWarn.Render(fmt.Sprintf("%d proposed", r.proposed))
			}
		}
		line := fmt.Sprintf("  %-10s %-40s %s", sBold.Render(r.alias), r.title, next)
		if i == m.selM {
			line = selectLine(line, m.w)
		}
		out = append(out, line)
	}
	return out
}

func (m *model) sittingHeader() []string {
	s := m.agenda.Sitting
	when := strings.TrimSpace(s.Date + " " + s.Time)
	line := fmt.Sprintf("%s  %s  %s", sBold.Render(m.agenda.Title), when, stateStyle(s.State).Render(s.State))
	if s.Place != "" {
		line += sMuted.Render("  " + s.Place)
	}
	if s.Virtual {
		line += sMuted.Render("  (from the recurrence)")
	}
	total := "planned " + m.agenda.Planned
	if m.agenda.Duration != "" {
		total += " of " + m.agenda.Duration
	}
	if m.agenda.Over {
		total = sWarn.Render(total + " — over time")
	} else {
		total = sMuted.Render(total)
	}
	return []string{line, sMuted.Render(s.ID) + "  " + total, ""}
}

func outcomeMark(o *store.Outcome) string {
	switch {
	case o == nil:
		return " "
	case o.Status == "draft":
		return sWarn.Render("✎")
	case o.Next == "deferred":
		return sWarn.Render("↷")
	}
	return sOK.Render("✓")
}

func (m *model) renderAgenda() []string {
	if m.agenda == nil {
		return []string{sMuted.Render("  loading…")}
	}
	out := m.sittingHeader()
	var rows []string
	shownProposed := false
	for i, r := range m.rows {
		if r.proposed && !shownProposed {
			rows = append(rows, "", sSection.Render("Proposed"))
			shownProposed = true
		}
		it := r.item
		owner := ""
		if it.Owner != "" {
			owner = sMuted.Render(" — " + it.Owner)
		}
		var line string
		if r.proposed {
			line = fmt.Sprintf("  %-10s %5s  %s%s", it.ID, it.Duration, it.Title, owner)
		} else {
			line = fmt.Sprintf("%s %5s %-10s %5s  %s%s  %s", outcomeMark(r.outcome), r.start, it.ID, it.Duration, it.Title, owner,
				sMuted.Render(it.Kind)+" "+stateStyle(it.State).Render(it.State))
		}
		if i == m.sel {
			line = selectLine(line, m.w)
		}
		rows = append(rows, line)
	}
	if len(m.agenda.Items) == 0 {
		rows = append([]string{sMuted.Render("  (no item on the agenda — n adds one)")}, rows...)
	}
	// The list keeps at least half of the room when it needs it; the pane takes the rest.
	room := m.h - 2 - len(out)
	listH := max(3, min(len(rows), room/2))
	var pane []string
	if r := m.current(); r != nil {
		pane = m.pane(r.item, r.outcome, room-listH-1)
	}
	rows = window(rows, m.sel+2, listH)
	if len(pane) > 0 {
		rows = append(rows, "")
	}
	return append(append(out, rows...), pane...)
}

// pane shows the content of one item under the list: owner, deferrals,
// question, attachments, refs, notes, its outcome in this sitting, and what
// the sphere's ref commands say about its refs (the dossier behind office:…).
func (m *model) pane(it *store.Item, o *store.Outcome, maxLines int) []string {
	w := max(20, m.w-2)
	maxLines = max(4, maxLines)
	head := fmt.Sprintf("── %s · %s ", it.ID, it.Title)
	lines := []string{sTitle.Render(pad(head+strings.Repeat("─", max(0, w-ansi.StringWidth(head))), w))}
	field := func(label, v string) {
		if v == "" {
			return
		}
		for i, l := range strings.Split(ansi.Wordwrap(v, max(10, w-11), ""), "\n") {
			key := ""
			if i == 0 {
				key = label
			}
			lines = append(lines, fmt.Sprintf("%s %s", sMuted.Render(fmt.Sprintf("%-9s", key)), l))
		}
	}
	who := strings.Join(nonEmpty(it.Owner, it.Kind, it.Duration), " · ")
	var deferred []string
	for _, e := range it.History {
		if e.Result == "deferred" {
			deferred = append(deferred, e.Sitting)
		}
	}
	if len(deferred) > 0 {
		who += fmt.Sprintf(" · deferred %d× (%s)", len(deferred), strings.Join(deferred, ", "))
	}
	field("Owner", who)
	field("Question", it.Expected)
	field("Attached", strings.Join(it.Attachments, ", "))
	field("Refs", strings.Join(it.Refs, ", "))
	field("Notes", it.Notes)
	if o != nil {
		field("Summary", o.Summary)
		field("Decision", o.Decision)
		for _, a := range o.Actions {
			field("Action", strings.Join(nonEmpty(a.What, a.Who, a.Due), " · "))
		}
		field("Outcome", fmt.Sprintf("%s by %s, next %s", o.Status, o.By, o.Next))
	}
	field("Reason", it.Reason)
	for _, ref := range it.Refs {
		if !m.st.CanShowRef(ref) {
			continue
		}
		head := fmt.Sprintf("·· %s ", ref)
		lines = append(lines, sMuted.Render(head+strings.Repeat("·", max(0, w-ansi.StringWidth(head)))))
		e, ok := m.refs[ref]
		switch {
		case !ok || (e.loading && e.shown.Ref == ""):
			lines = append(lines, sMuted.Render("  loading…"))
		case e.shown.Error != "":
			lines = append(lines, sErr.Render("  "+e.shown.Error))
		default:
			for _, l := range strings.Split(e.shown.Text, "\n") {
				if strings.TrimSpace(l) == "" && len(lines) > 0 && lines[len(lines)-1] == "" {
					continue
				}
				lines = append(lines, strings.Split(ansi.Wordwrap(l, w, ""), "\n")...)
			}
		}
	}
	if len(lines) > maxLines {
		lines = append(lines[:maxLines-1], sMuted.Render("          … enter shows the whole item"))
	}
	return lines
}

func nonEmpty(l ...string) []string {
	var out []string
	for _, s := range l {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (m *model) renderItem() []string {
	it := m.item
	if it == nil {
		return []string{sMuted.Render("  loading…")}
	}
	field := func(k, v string) string {
		if v == "" {
			return ""
		}
		return fmt.Sprintf("  %-12s %s", sMuted.Render(k), v)
	}
	lines := []string{sBold.Render(it.Title), sMuted.Render(it.ID) + "  " + stateStyle(it.State).Render(it.State), ""}
	for _, l := range []string{
		field("sitting", it.Sitting), field("owner", it.Owner), field("kind", it.Kind), field("duration", it.Duration),
		field("question", it.Expected), field("reason", it.Reason),
		field("attachments", strings.Join(it.Attachments, ", ")), field("refs", strings.Join(it.Refs, ", ")),
	} {
		if l != "" {
			lines = append(lines, l)
		}
	}
	if it.Notes != "" {
		lines = append(lines, "", sSection.Render("Notes"))
		for _, l := range strings.Split(it.Notes, "\n") {
			lines = append(lines, "  "+l)
		}
	}
	if len(it.History) > 0 {
		lines = append(lines, "", sSection.Render("History"))
		for _, e := range it.History {
			lines = append(lines, fmt.Sprintf("  %s  %s", e.Sitting, stateStyle(e.Result).Render(e.Result)))
			if o := e.Outcome; o != nil {
				if o.Summary != "" {
					lines = append(lines, "    = "+o.Summary)
				}
				if o.Decision != "" {
					lines = append(lines, "    ! "+o.Decision)
				}
				for _, a := range o.Actions {
					mark := "[ ]"
					if a.Done {
						mark = "[x]"
					}
					lines = append(lines, fmt.Sprintf("    %s %s (%s, %s)", mark, a.What, a.Who, a.Due))
				}
				lines = append(lines, sMuted.Render(fmt.Sprintf("    %s by %s, next %s", o.Status, o.By, o.Next)))
			}
		}
	}
	for _, ref := range it.Refs {
		if !m.st.CanShowRef(ref) {
			continue
		}
		lines = append(lines, "", sSection.Render(ref))
		switch e, ok := m.refs[ref]; {
		case !ok || (e.loading && e.shown.Ref == ""):
			lines = append(lines, sMuted.Render("  loading…"))
		case e.shown.Error != "":
			lines = append(lines, sErr.Render("  "+e.shown.Error))
		default:
			for _, l := range strings.Split(e.shown.Text, "\n") {
				for _, wl := range strings.Split(ansi.Wordwrap(l, max(20, m.w-4), ""), "\n") {
					lines = append(lines, "  "+wl)
				}
			}
		}
	}
	lines = append(lines, "", sSection.Render("Log"))
	for _, l := range it.Log {
		lines = append(lines, sMuted.Render(fmt.Sprintf("  %s  %s  %s", l.At, l.By, l.What)))
	}
	return lines[min(m.scroll, max(0, len(lines)-1)):]
}

func clock(d time.Duration) string {
	d = d.Round(time.Second)
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

func (m *model) renderLive() []string {
	if m.agenda == nil {
		return []string{sMuted.Render("  loading…")}
	}
	out := m.sittingHeader()
	var total, planned time.Duration
	for i, ai := range m.agenda.Items {
		it := ai.Item
		plan, _ := time.ParseDuration(it.Duration)
		spent := m.spent(it.ID)
		total += spent
		planned += plan
		timer := clock(spent) + sMuted.Render(" / "+clock(plan))
		if plan > 0 && spent > plan {
			timer = sWarn.Render(clock(spent)) + sMuted.Render(" / "+clock(plan))
		}
		line := fmt.Sprintf("  %s %-10s %s  %s", outcomeMark(ai.Outcome), it.ID, pad(timer, 14), it.Title)
		if i == m.live.cur {
			line = selectLine(line, m.w)
		}
		out = append(out, line)
		if i == m.live.cur && ai.Outcome != nil {
			if ai.Outcome.Summary != "" {
				out = append(out, "      = "+ai.Outcome.Summary)
			}
			if ai.Outcome.Decision != "" {
				out = append(out, "      ! "+ai.Outcome.Decision)
			}
			for _, a := range ai.Outcome.Actions {
				out = append(out, fmt.Sprintf("      > %s (%s, %s)", a.What, a.Who, a.Due))
			}
		}
	}
	state := sMuted.Render("timer paused")
	if m.live.running {
		state = sOK.Render("timer running")
	}
	out = append(out, "", fmt.Sprintf("  elapsed %s of %s  %s", sBold.Render(clock(total)), clock(planned), state), "")
	if m.live.cur < len(m.agenda.Items) {
		ai := m.agenda.Items[m.live.cur]
		out = append(out, m.pane(ai.Item, ai.Outcome, m.h-2-len(out))...)
	}
	return out
}

func (m *model) renderActions() []string {
	if len(m.actions) == 0 {
		return []string{"", sMuted.Render("  No open action.")}
	}
	var out []string
	for i, a := range m.actions {
		mark := "[ ]"
		if a.Done {
			mark = sOK.Render("[x]")
		}
		due := a.Due
		if due != "" && due < m.now().Format("2006-01-02") && !a.Done {
			due = sErr.Render(due)
		}
		line := fmt.Sprintf("  %s %-10s %-14s %s  %s", mark, due, a.Who, a.What, sMuted.Render(fmt.Sprintf("%s#%d", a.Item, a.N)))
		if i == m.selA {
			line = selectLine(line, m.w)
		}
		out = append(out, line)
	}
	return window(out, m.selA, m.h-2)
}
