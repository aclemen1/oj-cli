package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/aclemen1/oj-cli/internal/store"
)

func (m *model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

// helpSide is the width from which the help panel sits at the right.
const helpSide = 120

func (m *model) render() string {
	w, h := m.w, m.h
	var panel []string
	side := false
	if !m.helpOff {
		if w >= helpSide {
			side = true
			hw := min(52, w/3)
			panel = m.helpPanel(hw)
			m.w = w - hw - 3
		} else {
			panel = m.helpPanel(w - 2)
			ph := min(len(panel), max(4, (h-2)/2))
			panel = panel[:ph]
			m.h = h - ph - 1
		}
	}
	lines, foot := m.renderMain()
	mainW, mainH := m.w, m.h
	m.w, m.h = w, h
	head := sTitle.Render("oj") + " " + sphereTag(m.st.Sphere)
	var body string
	switch {
	case panel == nil:
		body = block(lines, w, h-2)
	case side:
		left := strings.Split(block(lines, mainW, h-2), "\n")
		right := strings.Split(block(panel, w-mainW-3, h-2), "\n")
		rows := make([]string, len(left))
		for i := range left {
			rows[i] = left[i] + " " + sMuted.Render("│") + " " + right[i]
		}
		body = strings.Join(rows, "\n")
	default:
		body = block(lines, w, mainH-2) + "\n" + sMuted.Render(strings.Repeat("─", w)) + "\n" + block(panel, w, h-mainH-1)
	}
	return pad(head, w) + "\n" + body + "\n" + pad(foot, w)
}

// renderMain renders the view at m.w × m.h and returns its lines and footer.
func (m *model) renderMain() ([]string, string) {
	var lines []string
	var help string
	if m.moving != nil {
		lines = []string{sBold.Render(m.tr("Déplacer ", "Move ") + m.moving.ID + " · " + m.moving.Title),
			sMuted.Render(m.tr("vers la séance :", "to the sitting:")), ""}
		for i, s := range m.choices {
			line := fmt.Sprintf("  %-20s %s", s.ID, strings.TrimSpace(s.Date+" "+s.Time))
			if key := strings.TrimPrefix(s.ID, s.Meeting+"-"); len(key) >= 10 && key[:10] != s.Date {
				line += sMuted.Render(m.tr("  (déplacée)", "  (moved)"))
			}
			if i == m.selC {
				line = selectLine(line, m.w)
			}
			lines = append(lines, line)
		}
		return lines, helpLine("enter", m.tr("déplacer", "move"), "j/k", m.tr("choisir", "choose"), "esc", m.tr("annuler", "cancel"))
	}
	switch m.view {
	case vMeetings:
		lines, help = m.renderMeetings(), helpLine("enter", "agenda", "S", "all sittings", "A", "actions", "R", "refresh", "q", "quit")
	case vAgenda:
		pairs := []string{"n", "new", "a", "accept", "d", "defer", "x", "drop"}
		if u := m.undoLabel(); u != "" {
			pairs = append(pairs, "u", u)
		}
		if u := undoSittingLabel(m.agenda); u != "" {
			pairs = append(pairs, "U", u)
		}
		pairs = append(pairs, "M", "move to", "J/K", "order", "+/-", "5 min", "e", "edit", "f", "freeze", "r", "reopen", "l", "live", "h", "hold",
			"m", "minutes", "o", "open ref", "c", "create ref", "S", "sittings", "[/]", "sitting", "esc", "back")
		lines, help = m.renderAgenda(), helpLine(pairs...)
	case vSittings:
		lines, help = m.renderOverview(), helpLine("enter", "open", "j/k", "move", "esc", "back")
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
	case !m.helpOff:
		foot = helpLine("?", m.tr("masquer l'aide", "hide help")) + sMuted.Render(" · ") + help
	default:
		foot = helpLine("?", m.tr("aide", "help")) + sMuted.Render(" · ") + help
	}
	return lines, foot
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
		return []string{"", sMuted.Render("  No meeting yet. Create one: oj meeting add RDIR --title … --sphere " + m.st.Sphere)}
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
	shownProposed, shownDropped, shownAway := false, false, false
	for i, r := range m.rows {
		if r.proposed && !shownProposed {
			rows = append(rows, "", sSection.Render("Proposed"))
			shownProposed = true
		}
		if r.away && !shownAway {
			rows = append(rows, "", sSection.Render("Deferred"))
			shownAway = true
		}
		if r.dropped && !shownDropped {
			rows = append(rows, "", sSection.Render("Dropped"))
			shownDropped = true
		}
		it := r.item
		owner := ""
		if it.Owner != "" {
			owner = sMuted.Render(" — " + it.Owner)
		}
		var line string
		switch {
		case r.away:
			line = fmt.Sprintf("  %-10s %5s  %s%s  %s", it.ID, it.Duration, it.Title, owner, sWarn.Render("→ "+orDash(it.Sitting)))
		case r.dropped:
			line = sMuted.Render(fmt.Sprintf("  %-10s %5s  %s — %s", it.ID, it.Duration, it.Title, it.Reason))
		case r.proposed:
			line = fmt.Sprintf("  %-10s %5s  %s%s", it.ID, it.Duration, it.Title, owner)
		default:
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
	if it.Notes != "" {
		lines = append(lines, sMuted.Render("Notes"))
		lines = append(lines, markdown(it.Notes, w)...)
	}
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
			lines = append(lines, markdown(e.shown.Text, w)...)
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
		lines = append(lines, markdown(it.Notes, max(20, m.w-2))...)
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
			lines = append(lines, markdown(e.shown.Text, max(20, m.w-2))...)
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

func orDash(s string) string {
	if s == "" {
		return "no sitting yet"
	}
	return s
}

// undoLabel names what u does on the selected item.
func (m *model) undoLabel() string {
	r := m.current()
	if r == nil {
		return ""
	}
	switch r.item.State {
	case "deferred":
		return "undefer"
	case "dropped", "done":
		return "restore"
	}
	return ""
}

// undoSittingLabel names what U does on the sitting.
func undoSittingLabel(a *store.Agenda) string {
	if a == nil {
		return ""
	}
	switch a.Sitting.State {
	case "frozen":
		return "reopen"
	case "held":
		return "unhold"
	case "minuted":
		return "unminute"
	case "cancelled":
		return "restore"
	}
	return ""
}

func (m *model) renderOverview() []string {
	if len(m.ovRows) == 0 {
		return []string{"", sMuted.Render("  loading…")}
	}
	var out []string
	sel := 0
	for i, r := range m.ovRows {
		var line string
		switch {
		case r.sitting == "" && r.item == nil:
			if i > 0 {
				out = append(out, "")
			}
			line = " " + sTitle.Render(r.meeting)
		case r.agenda != nil:
			s := r.agenda.Sitting
			total := r.agenda.Planned
			if r.agenda.Duration != "" {
				total += " of " + r.agenda.Duration
			}
			n := len(r.agenda.Items)
			line = fmt.Sprintf("  %s  %s  %s  %s", sBold.Render(fmt.Sprintf("%-20s", s.ID)), strings.TrimSpace(s.Date+" "+s.Time),
				stateStyle(s.State).Render(fmt.Sprintf("%-9s", s.State)), sMuted.Render(fmt.Sprintf("%d %s · %s", n, plural(n, "item"), total)))
			if r.agenda.Over {
				line += " " + sWarn.Render("over time")
			}
		case r.unplanned:
			line = fmt.Sprintf("      %-10s %s  %s", r.item.ID, r.item.Title, sWarn.Render("no sitting yet"))
		default:
			line = fmt.Sprintf("      %-10s %s  %s", r.item.ID, r.item.Title, stateStyle(r.item.State).Render(r.item.State))
		}
		if i == m.selO {
			line = selectLine(line, m.w)
			sel = len(out)
		}
		out = append(out, line)
	}
	return window(out, sel, m.h-2)
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
