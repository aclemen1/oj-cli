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
	head := sTitle.Render("oj") + " " + m.headSphere()
	if m.query != "" {
		head += " " + sWarn.Render("/"+m.query)
	}
	if m.build != "" {
		head += " " + sMuted.Render(m.build)
	}
	if m.newBin {
		head += " " + sWarn.Render(m.tr("● nouvelle version", "● new version"))
	}
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

// headSphere names the sphere of the open meeting, or the filter of the lists.
func (m *model) headSphere() string {
	list := m.view == vMeetings || m.view == vActions || (m.view == vSittings && m.ovScope == "")
	if !m.multi() || !list {
		return sphereTag(m.st.Sphere)
	}
	if m.filter != "" {
		return sphereTag(m.filter)
	}
	var tags []string
	for _, st := range m.stores {
		tags = append(tags, sphereTag(st.Sphere))
	}
	return strings.Join(tags, sMuted.Render("+"))
}

// tagged puts the sphere before a name when several spheres are shown.
func (m *model) tagged(sphere, name string) string {
	if !m.multi() || m.filter != "" {
		return name
	}
	return sphereTag(sphere) + sMuted.Render(":") + name
}

// renderMain renders the view at m.w × m.h and returns its lines and footer.
func (m *model) renderMain() ([]string, string) {
	var lines []string
	var help string
	if m.moving != nil {
		lines = []string{sBold.Render(m.tr("Déplacer ", "Move ") + m.moving.ID + " · " + m.moving.Title),
			sMuted.Render(m.tr("vers la séance :", "to the sitting:")), ""}
		for i, s := range m.choices {
			line := fmt.Sprintf("  %-20s %s", s.ID, m.dayTime(s.Date, s.Time))
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
		lines, help = m.renderMeetings(), helpLine(m.filterPairs("enter", "agenda", "/", "filter", "1/2/3", "meetings/actions/sittings", "r", "refresh", "q", "quit")...)
	case vAgenda:
		lines, help = m.renderAgenda(), helpLine(m.agendaPairs()...)
	case vSittings:
		pairs := []string{"enter", "open", "j/k", "move", "/", "filter", "esc", "back"}
		if m.ovScope == "" {
			pairs = m.filterPairs(pairs...)
		}
		lines, help = m.renderOverview(), helpLine(pairs...)
	case vStanding:
		lines, help = m.renderStanding(), helpLine("c", "new", "p", "start/end", "x", "stop", "j/k", "move", "esc", "agenda")
	case vDoc:
		lines, help = m.renderDoc(), helpLine("j/k", "scroll", "R", "ask outcomes", "gg/G", "top/end", "esc", "agenda")
	case vItem:
		lines, help = m.renderItem(), helpLine("E", "edit", "N", "add note", "o", "open ref", "j/k", "scroll", "esc", "back")
	case vLive:
		lines, help = m.renderLive(), helpLine("space", "timer", "n/p", "next/previous", "E", "summary", "D", "decision",
			"c", "action", "z", "defer", "H", "hold", "J/K", "scroll pane", "esc", "agenda")
	case vActions:
		lines, help = m.renderActions(), helpLine(m.filterPairs("space", "done/open", "e", "done", "enter", "item", "o", "open ref", "f", "show done", "/", "filter", "esc", "back")...)
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

// filterPairs adds the sphere filter to a footer when several spheres are shown.
func (m *model) filterPairs(pairs ...string) []string {
	if !m.multi() {
		return pairs
	}
	return append([]string{"s", "sphere"}, pairs...)
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
			next = fmt.Sprintf("%s  %s  %s on the agenda", r.next, sMuted.Render(m.day(r.nextDate)), sBold.Render(fmt.Sprint(r.accepted)))
			if r.proposed > 0 {
				next += "  " + sWarn.Render(fmt.Sprintf("%d proposed", r.proposed))
			}
		}
		if r.pending != "" {
			next = sWarn.Render(m.tr("PV à faire : ", "minutes to do: ")+r.pending) + "  " + next
		}
		line := fmt.Sprintf("  %s %-40s %s", pad(sBold.Render(m.tagged(r.sphere, r.alias)), 16), r.title, next)
		if i == m.selM {
			line = selectLine(line, m.w)
		}
		out = append(out, line)
	}
	return out
}

func (m *model) sittingHeader() []string {
	s := m.agenda.Sitting
	when := m.dayTime(s.Date, s.Time)
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
			line = fmt.Sprintf("%s %5s %-10s %5s  %s%s  %s", outcomeMark(r.outcome), r.start, idLabel(it), it.Duration, it.Title, owner,
				sMuted.Render(it.Kind)+" "+stateStyle(it.State).Render(it.State))
		}
		if i == m.sel {
			line = selectLine(line, m.w)
		}
		rows = append(rows, line)
	}
	if len(m.agenda.Items) == 0 {
		rows = append([]string{sMuted.Render("  (no item on the agenda — c adds one)")}, rows...)
	}
	// The list keeps at least half of the room when it needs it; the pane takes the rest.
	room := m.h - 2 - len(out)
	listH := max(3, min(len(rows), room/2))
	var pane []string
	if r := m.current(); r != nil && !m.paneOff {
		pane = m.pane(r.item, r.outcome, room-listH-1)
	}
	rows = window(rows, m.sel+2, listH)
	if len(pane) > 0 {
		rows = append(rows, "")
	}
	return append(append(out, rows...), pane...)
}

// agendaPairs are the footer keys of the agenda: the sitting's next steps
// first, so they fit on screen, then the item's gestures and the navigation.
func (m *model) agendaPairs() []string {
	var p []string
	if m.agenda != nil {
		switch m.agenda.Sitting.State {
		case "planned":
			p = append(p, "c", "new", "f", "freeze", "L", "live", "P", "preview")
		case "frozen":
			p = append(p, "L", "live", "H", "hold", "P", "preview", "F", "reopen")
		case "held":
			p = append(p, "L", "live")
			if m.st.CanAsk(m.agenda.Sitting.Meeting, "outcomes") {
				p = append(p, "R", "ask outcomes")
			}
			p = append(p, "P", "minutes preview", "m", "approve minutes")
		case "minuted":
			p = append(p, "P", "minutes")
		}
	}
	if r := m.current(); r != nil && r.proposed {
		p = append(p, "a", "accept")
	}
	p = append(p, "z", "defer", "x", "drop")
	if u := m.undoLabel(); u != "" {
		p = append(p, "u", u)
	}
	if u := undoSittingLabel(m.agenda); u != "" {
		p = append(p, "U", u)
	}
	p = append(p, "*", "recurring", "g r", "recurring list", "E", "edit", "N", "add note", "M", "move to", "ctrl+j/k", "order", "+/-", "5 min",
		"o", "open ref", "O", "create ref", "J/K", "scroll pane", "tab", "pane", "/", "filter", "2/3", "actions/sittings", "[/]", "sitting", "esc", "back")
	return p
}

// renderDoc shows the agenda or the minutes of the open sitting as they would be produced.
func (m *model) renderDoc() []string {
	title := m.tr("Projet d'ordre du jour", "Draft agenda")
	if m.doc.kind == "minutes" {
		title = m.tr("Projet de PV", "Draft minutes")
	}
	if m.doc.final {
		title = m.tr("Document produit", "Produced document")
	}
	id := ""
	if m.agenda != nil {
		id = m.agenda.Sitting.ID
	}
	out := []string{sBold.Render(title) + "  " + sMuted.Render(id), ""}
	switch {
	case m.doc.err != nil:
		return append(out, sErr.Render("  "+m.doc.err.Error()))
	case m.doc.text == "":
		return append(out, sMuted.Render("  loading…"))
	}
	body := markdown(m.doc.text, max(20, m.w-4))
	m.scroll = min(m.scroll, max(0, len(body)-(m.h-4)))
	return append(out, body[m.scroll:]...)
}

// renderStanding lists the recurring items of the open meeting.
func (m *model) renderStanding() []string {
	out := []string{sBold.Render(m.tr("Points récurrents de ", "Recurring items of ") + m.meeting), ""}
	if len(m.standing) == 0 {
		return append(out, sMuted.Render(m.tr("  Aucun. c en ajoute un ; * dans l'ordre du jour rend un point récurrent.",
			"  None. c adds one; * in the agenda makes an item recurring.")))
	}
	for i, s := range m.standing {
		place := m.tr("fin", "end")
		if s.Place == "start" {
			place = m.tr("début", "start")
		}
		line := fmt.Sprintf("  ↻ %-16s %-6s %5s  %s", s.Key, place, s.Duration, s.Title)
		if i == m.selR {
			line = selectLine(line, m.w)
		}
		out = append(out, line)
	}
	return out
}

// idLabel is the item's id, or ↻ and its key for a recurring item not written yet.
func idLabel(it *store.Item) string {
	if it.Virtual {
		return "↻ " + it.Standing
	}
	return it.ID
}

// pane shows the content of one item under the list: owner, deferrals,
// question, attachments, refs, notes, its outcome in this sitting, and what
// the sphere's ref commands say about its refs (the dossier behind office:…).
func (m *model) pane(it *store.Item, o *store.Outcome, maxLines int) []string {
	w := max(20, m.w-2)
	maxLines = max(4, maxLines)
	head := fmt.Sprintf("── %s · %s ", idLabel(it), it.Title)
	lines := []string{sTitle.Render(pad(head+strings.Repeat("─", max(0, w-ansi.StringWidth(head))), w))}
	if k := it.StandingKey(); k != "" {
		note := "recurring item " + k + ", one per sitting"
		if it.Virtual {
			note += "; written at the first change or at freeze"
		}
		lines = append(lines, sMuted.Render(note))
	}
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
			field("Action", strings.Join(nonEmpty(a.What, a.Who, m.dayOrEmpty(a.Due)), " · "))
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
	lines = append(lines, m.citedLines(it, w, func(t string) string {
		h := fmt.Sprintf("·· %s ", t)
		return sMuted.Render(h + strings.Repeat("·", max(0, w-ansi.StringWidth(h))))
	})...)
	if m.paneScroll > 0 {
		m.paneScroll = min(m.paneScroll, max(0, len(lines)-maxLines))
		lines = append(lines[:1], lines[1+m.paneScroll:]...)
	}
	if len(lines) > maxLines {
		lines = append(lines[:maxLines-1], sMuted.Render("          … J K, enter shows the whole item"))
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
					lines = append(lines, fmt.Sprintf("    %s %s (%s, %s)", mark, a.What, a.Who, m.dayOrEmpty(a.Due)))
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
	for _, l := range m.citedLines(it, max(20, m.w-2), func(t string) string { return "\n" + sSection.Render(t) }) {
		lines = append(lines, strings.Split(l, "\n")...)
	}
	lines = append(lines, "", sSection.Render("Log"))
	for _, l := range it.Log {
		lines = append(lines, sMuted.Render(fmt.Sprintf("  %s  %s  %s", l.At, l.By, l.What)))
	}
	m.scroll = min(m.scroll, max(0, len(lines)-(m.h-2)))
	return lines[m.scroll:]
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
		spent := m.spent(it)
		total += spent
		planned += plan
		timer := clock(spent) + sMuted.Render(" / "+clock(plan))
		if plan > 0 && spent > plan {
			timer = sWarn.Render(clock(spent)) + sMuted.Render(" / "+clock(plan))
		}
		line := fmt.Sprintf("  %s %-10s %s  %s", outcomeMark(ai.Outcome), idLabel(it), pad(timer, 14), it.Title)
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
				out = append(out, fmt.Sprintf("      > %s (%s, %s)", a.What, a.Who, m.dayOrEmpty(a.Due)))
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
		if !m.paneOff {
			out = append(out, m.pane(ai.Item, ai.Outcome, m.h-2-len(out))...)
		}
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
		due := pad(m.dayOrEmpty(a.Due), 30)
		if a.Due != "" && a.Due < m.now().Format("2006-01-02") && !a.Done {
			due = sErr.Render(due)
		}
		line := fmt.Sprintf("  %s %s %-14s %s  %s", mark, due, a.Who, a.What, sMuted.Render(m.tagged(a.Sphere, fmt.Sprintf("%s#%d", a.Item, a.N))))
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
			if m.multi() && m.filter == "" {
				line = " " + sphereTag(r.sphere) + sMuted.Render(":") + sTitle.Render(r.meeting)
			}
		case r.agenda != nil:
			s := r.agenda.Sitting
			total := r.agenda.Planned
			if r.agenda.Duration != "" {
				total += " of " + r.agenda.Duration
			}
			n := len(r.agenda.Items)
			line = fmt.Sprintf("  %s  %s  %s  %s", sBold.Render(fmt.Sprintf("%-20s", s.ID)), pad(m.dayTime(s.Date, s.Time), 36),
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

var weekdaysFR = []string{"dim.", "lun.", "mar.", "mer.", "jeu.", "ven.", "sam."}

// day puts the weekday before a YYYY-MM-DD date and how far it is after it:
// "jeu. 2026-10-08 · dans 2 j", "Thu 2026-10-08 · in 2 days".
func (m *model) day(d string) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return d
	}
	wd := t.Format("Mon")
	if m.fr() {
		wd = weekdaysFR[t.Weekday()]
	}
	return wd + " " + d + sMuted.Render(" · "+m.relative(t))
}

// relative says how many days or weeks a date is from today.
func (m *model) relative(t time.Time) string {
	now := m.now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	n := int(t.Sub(today).Hours() / 24)
	fr := m.fr()
	switch {
	case n == 0:
		return m.tr("aujourd'hui", "today")
	case n == 1:
		return m.tr("demain", "tomorrow")
	case n == -1:
		return m.tr("hier", "yesterday")
	}
	abs := n
	if abs < 0 {
		abs = -abs
	}
	var amount string
	switch {
	case abs < 14 && fr:
		amount = fmt.Sprintf("%d j", abs)
	case abs < 14:
		amount = fmt.Sprintf("%d days", abs)
	case fr:
		amount = fmt.Sprintf("%d sem.", (abs+3)/7)
	default:
		amount = fmt.Sprintf("%d weeks", (abs+3)/7)
	}
	if n > 0 {
		return m.tr("dans "+amount, "in "+amount)
	}
	return m.tr("il y a "+amount, amount+" ago")
}

func (m *model) dayOrEmpty(d string) string {
	if d == "" {
		return ""
	}
	return m.day(d)
}

// dayTime is day with the time between the date and the relative part:
// "jeu. 2026-10-08 09:00 · dans 2 j".
func (m *model) dayTime(d, at string) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil || at == "" {
		return m.day(d)
	}
	wd := t.Format("Mon")
	if m.fr() {
		wd = weekdaysFR[t.Weekday()]
	}
	return wd + " " + d + " " + at + sMuted.Render(" · "+m.relative(t))
}

// citedLines are the sections of the cited block for an item: what other
// tools hold about it. An empty section is left out; head draws a title.
func (m *model) citedLines(it *store.Item, w int, head func(string) string) []string {
	if it == nil || it.Virtual {
		return nil
	}
	var lines []string
	for _, s := range m.cited[it.ID].sections {
		switch {
		case s.Error != "":
			lines = append(lines, head(s.Title), sMuted.Render("  "+s.Error))
		case s.Text != "":
			lines = append(lines, head(s.Title))
			lines = append(lines, markdown(s.Text, w)...)
		}
	}
	return lines
}
