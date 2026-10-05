// Package tui is ordo's terminal interface (SPEC §11): meetings, the agenda
// of a sitting, an item, the live sitting with a timer, and open actions.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/aclemen1/ordo-cli/internal/actions"
	"github.com/aclemen1/ordo-cli/internal/spec"
	"github.com/aclemen1/ordo-cli/internal/store"
)

func init() {
	spec.Register(&spec.Action{
		Category: "setup", Name: "tui", Top: true,
		Summary:  "Open the terminal interface on a sphere: meetings, agenda, live sitting, actions.",
		Params:   []spec.Param{{Name: "sphere", Kind: spec.String, Help: "Sphere to show. Defaults to $ORDO_SPHERE."}},
		Effects:  []string{"Runs until q; every change goes through the same store actions as the CLI."},
		Examples: []string{"ordo tui --sphere pro"},
		Run: func(ctx *spec.Context) (any, error) {
			st, err := actions.Open(ctx)
			if err != nil {
				return nil, err
			}
			_, err = tea.NewProgram(newModel(st)).Run()
			return spec.Streamed{}, err
		},
	})
}

type view int

const (
	vMeetings view = iota
	vAgenda
	vItem
	vLive
	vActions
)

type prompt int

const (
	pNone prompt = iota
	pNewItem
	pDrop
	pSummary
	pDecision
	pAction
	pMinute
	pUnminute
)

// row is a line of the agenda: an item on it, a proposal, or a dropped item.
type row struct {
	item     *store.Item
	outcome  *store.Outcome
	start    string
	proposed bool
	dropped  bool
}

type meetingRow struct {
	alias, title, next string
	accepted, proposed int
}

type live struct {
	cur     int
	running bool
	since   time.Time
	spent   map[string]time.Duration
}

type model struct {
	st  *store.Store
	now func() time.Time

	view, back view

	meetings []meetingRow
	selM     int

	meeting  string
	agenda   *store.Agenda
	rows     []row
	sel      int
	sittings []*store.Sitting

	item   *store.Item
	scroll int

	actions  []store.ActionRow
	selA     int
	showDone bool

	live live

	prompt prompt
	input  textinput.Model
	target string

	w, h      int
	status    string
	statusErr bool
	warnings  warnings

	// refs caches what the sphere's ref commands say, by ref.
	refs map[string]refEntry
}

type refEntry struct {
	shown   store.RefShown
	at      time.Time
	loading bool
}

const refFresh = time.Minute

func newModel(st *store.Store) *model {
	in := textinput.New()
	styles := in.Styles()
	styles.Cursor.Blink = false
	in.SetStyles(styles)
	m := &model{st: st, now: time.Now, input: in, w: 100, h: 30, live: live{spent: map[string]time.Duration{}},
		refs: map[string]refEntry{}}
	st.Warn = m.warnings.add
	return m
}

// warnings collects what a store change reports after it succeeded; the
// changes run in commands, so the list is locked.
type warnings struct {
	mu   sync.Mutex
	list []string
}

func (w *warnings) add(m string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.list = append(w.list, m)
}

func (w *warnings) take() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	l := w.list
	w.list = nil
	return l
}

// ------------------------------------------------------------------ messages

type (
	meetingsMsg struct {
		rows []meetingRow
		err  error
	}
	agendaMsg struct {
		agenda   *store.Agenda
		sittings []*store.Sitting
		err      error
	}
	itemMsg struct {
		item *store.Item
		err  error
	}
	actionsMsg struct {
		rows []store.ActionRow
		err  error
	}
	doneMsg struct {
		status string
		err    error
	}
	tickMsg struct{}
	refMsg  struct{ shown store.RefShown }
)

// shownItem is the item whose pane is on screen.
func (m *model) shownItem() *store.Item {
	switch m.view {
	case vAgenda:
		if r := m.current(); r != nil {
			return r.item
		}
	case vLive:
		return m.liveItem()
	case vItem:
		return m.item
	}
	return nil
}

// fetchRefs asks for the refs of the shown item that are missing or stale.
func (m *model) fetchRefs() tea.Cmd {
	it := m.shownItem()
	if it == nil {
		return nil
	}
	var cmds []tea.Cmd
	for _, ref := range it.Refs {
		e, ok := m.refs[ref]
		if !m.st.CanShowRef(ref) || e.loading || (ok && m.now().Sub(e.at) < refFresh) {
			continue
		}
		e.loading = true
		m.refs[ref] = e
		ref := ref
		cmds = append(cmds, func() tea.Msg { return refMsg{m.st.ShowRef(ref)} })
	}
	return tea.Batch(cmds...)
}

func (m *model) loadMeetings() tea.Cmd {
	return func() tea.Msg {
		ms, err := m.st.Meetings()
		if err != nil {
			return meetingsMsg{err: err}
		}
		var rows []meetingRow
		for _, mt := range ms {
			r := meetingRow{alias: mt.Alias, title: mt.Title}
			if a, err := m.st.Agenda(mt.Alias); err == nil {
				r.next, r.accepted, r.proposed = a.Sitting.ID, len(a.Items), len(a.Proposed)
			}
			rows = append(rows, r)
		}
		return meetingsMsg{rows: rows}
	}
}

func (m *model) loadAgenda(arg string) tea.Cmd {
	return func() tea.Msg {
		a, err := m.st.Agenda(arg)
		if err != nil {
			return agendaMsg{err: err}
		}
		mt, err := m.st.Meeting(a.Sitting.Meeting)
		if err != nil {
			return agendaMsg{err: err}
		}
		since := m.now().AddDate(-1, 0, 0).Format("2006-01-02")
		sits, err := m.st.Sittings(mt, since, 8, "all")
		return agendaMsg{agenda: a, sittings: sits, err: err}
	}
}

func (m *model) loadItem(id string) tea.Cmd {
	return func() tea.Msg {
		it, err := m.st.Item(id)
		return itemMsg{it, err}
	}
}

func (m *model) loadActions() tea.Cmd {
	state := "open"
	if m.showDone {
		state = "all"
	}
	return func() tea.Msg {
		rows, err := m.st.Actions(store.ActionFilter{State: state})
		return actionsMsg{rows, err}
	}
}

// reload refreshes what the current view shows.
func (m *model) reload() tea.Cmd {
	switch m.view {
	case vMeetings:
		return m.loadMeetings()
	case vAgenda, vLive:
		if m.agenda != nil {
			return m.loadAgenda(m.agenda.Sitting.ID)
		}
	case vItem:
		if m.item != nil {
			return m.loadItem(m.item.ID)
		}
	case vActions:
		return m.loadActions()
	}
	return nil
}

// do runs a store change and reports it.
func (m *model) do(status string, f func() error) tea.Cmd {
	return func() tea.Msg { return doneMsg{status: status, err: f()} }
}

func tick() tea.Cmd { return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} }) }

// ------------------------------------------------------------------ update

func (m *model) Init() tea.Cmd { return tea.Batch(tea.RequestBackgroundColor, m.loadMeetings()) }

func (m *model) setStatus(s string, isErr bool) { m.status, m.statusErr = s, isErr }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		if m.w < 20 || m.h < 6 {
			m.w, m.h = max(m.w, 80), max(m.h, 24)
		}
		m.input.SetWidth(max(10, m.w-30))
	case tea.BackgroundColorMsg:
		darkBackground = msg.IsDark()
	case meetingsMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			break
		}
		m.meetings = msg.rows
		m.selM = min(m.selM, max(0, len(m.meetings)-1))
	case agendaMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			break
		}
		if m.agenda == nil || m.agenda.Sitting.ID != msg.agenda.Sitting.ID {
			m.sel = 0
			m.live = live{spent: map[string]time.Duration{}}
		}
		m.agenda, m.sittings = msg.agenda, msg.sittings
		m.meeting = msg.agenda.Sitting.Meeting
		m.rows = nil
		for _, ai := range msg.agenda.Items {
			m.rows = append(m.rows, row{item: ai.Item, outcome: ai.Outcome, start: ai.Start})
		}
		for _, it := range msg.agenda.Proposed {
			m.rows = append(m.rows, row{item: it, proposed: true})
		}
		for _, it := range msg.agenda.Dropped {
			m.rows = append(m.rows, row{item: it, dropped: true})
		}
		m.sel = min(m.sel, max(0, len(m.rows)-1))
		m.live.cur = min(m.live.cur, max(0, len(msg.agenda.Items)-1))
		if m.view != vLive {
			m.view = vAgenda
		}
		return m, m.fetchRefs()
	case refMsg:
		m.refs[msg.shown.Ref] = refEntry{shown: msg.shown, at: m.now()}
	case itemMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			break
		}
		m.item = msg.item
		return m, m.fetchRefs()
	case actionsMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			break
		}
		m.actions = msg.rows
		m.selA = min(m.selA, max(0, len(m.actions)-1))
	case doneMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
		} else if w := m.warnings.take(); len(w) > 0 {
			m.setStatus(msg.status+" — "+strings.Join(w, "; "), true)
		} else {
			m.setStatus(msg.status, false)
		}
		return m, m.reload()
	case tickMsg:
		if m.view == vLive && m.live.running {
			return m, tick()
		}
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.prompt != pNone {
			return m, m.keyPrompt(msg)
		}
		m.status = ""
		switch m.view {
		case vMeetings:
			return m, m.keyMeetings(msg)
		case vAgenda:
			cmd := m.keyAgenda(msg)
			return m, tea.Batch(cmd, m.fetchRefs())
		case vItem:
			return m, m.keyItem(msg)
		case vLive:
			cmd := m.keyLive(msg)
			return m, tea.Batch(cmd, m.fetchRefs())
		case vActions:
			return m, m.keyActions(msg)
		}
	}
	return m, nil
}

func (m *model) ask(p prompt, label, target, value string) tea.Cmd {
	m.prompt, m.target = p, target
	m.input.Reset()
	m.input.Prompt = label + " › "
	m.input.SetValue(value)
	m.input.CursorEnd()
	return m.input.Focus()
}

func (m *model) keyPrompt(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.prompt = pNone
		m.input.Blur()
		return nil
	case "enter":
		p, v, target := m.prompt, strings.TrimSpace(m.input.Value()), m.target
		m.prompt = pNone
		m.input.Blur()
		return m.answer(p, v, target)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return cmd
}

func (m *model) answer(p prompt, v, target string) tea.Cmd {
	switch p {
	case pNewItem:
		if v == "" {
			return nil
		}
		sitting := ""
		if m.agenda != nil && m.agenda.Sitting.State == "planned" {
			sitting = m.agenda.Sitting.ID
		}
		return m.do("item added", func() error {
			_, err := m.st.AddItem(m.meeting, store.ItemInput{Title: v}, true, sitting)
			return err
		})
	case pDrop:
		if v == "" {
			m.setStatus("a drop needs a reason", true)
			return nil
		}
		return m.do(target+" dropped", func() error { _, err := m.st.DropItem(target, v); return err })
	case pSummary:
		return m.outcome(target, func(in *store.OutcomeInput) { in.Summary = v })
	case pDecision:
		return m.outcome(target, func(in *store.OutcomeInput) { in.Decision = v })
	case pAction:
		if v == "" {
			return nil
		}
		if _, err := store.ParseAction(v); err != nil {
			m.setStatus(err.Error(), true)
			return nil
		}
		return m.outcome(target, func(in *store.OutcomeInput) { in.Actions = append(in.Actions, v) })
	case pMinute:
		if v != "yes" {
			m.setStatus("minutes not approved", false)
			return nil
		}
		return m.do("minutes approved", func() error { _, err := m.st.MinuteSitting(target); return err })
	case pUnminute:
		if v != "yes" {
			m.setStatus("minutes kept", false)
			return nil
		}
		return m.do("minutes taken back", func() error { _, err := m.st.UnminuteSitting(target); return err })
	}
	return nil
}

// outcome edits the outcome of an item in the shown sitting, keeping its other fields.
func (m *model) outcome(id string, edit func(*store.OutcomeInput)) tea.Cmd {
	sitting := m.agenda.Sitting.ID
	return m.do("outcome recorded for "+id, func() error {
		it, err := m.st.Item(id)
		if err != nil {
			return err
		}
		in := store.OutcomeInput{Next: "done"}
		for _, e := range it.History {
			if e.Sitting == sitting && e.Outcome != nil {
				o := e.Outcome
				in.Summary, in.Decision, in.Next = o.Summary, o.Decision, o.Next
				for _, a := range o.Actions {
					in.Actions = append(in.Actions, a.What+"|"+a.Who+"|"+a.Due)
				}
			}
		}
		edit(&in)
		if in.Summary == "" && in.Decision == "" && len(in.Actions) == 0 {
			return nil
		}
		_, err = m.st.SetOutcome(id, sitting, in)
		return err
	})
}

func (m *model) keyMeetings(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "q", "esc":
		return tea.Quit
	case "j", "down":
		m.selM = min(len(m.meetings)-1, m.selM+1)
	case "k", "up":
		m.selM = max(0, m.selM-1)
	case "enter":
		if m.selM < len(m.meetings) {
			m.agenda = nil
			return m.loadAgenda(m.meetings[m.selM].alias)
		}
	case "A":
		m.back, m.view = vMeetings, vActions
		return m.loadActions()
	case "R":
		return m.loadMeetings()
	}
	return nil
}

func (m *model) current() *row {
	if m.sel < 0 || m.sel >= len(m.rows) {
		return nil
	}
	return &m.rows[m.sel]
}

func (m *model) keyAgenda(k tea.KeyPressMsg) tea.Cmd {
	r := m.current()
	sit := m.agenda.Sitting
	switch k.String() {
	case "q", "esc":
		m.view = vMeetings
		return m.loadMeetings()
	case "j", "down":
		m.sel = min(len(m.rows)-1, m.sel+1)
	case "k", "up":
		m.sel = max(0, m.sel-1)
	case "enter":
		if r != nil {
			m.back, m.view, m.scroll, m.item = vAgenda, vItem, 0, r.item
			return m.loadItem(r.item.ID)
		}
	case "n":
		return m.ask(pNewItem, "new item", "", "")
	case "a":
		if r != nil && r.proposed {
			id := r.item.ID
			return m.do(id+" accepted", func() error { _, err := m.st.AcceptItems([]string{id}); return err })
		}
	case "d":
		if r != nil {
			id := r.item.ID
			return m.do(id+" deferred", func() error { _, err := m.st.DeferItem(id, ""); return err })
		}
	case "x":
		if r != nil {
			return m.ask(pDrop, "reason to drop "+r.item.ID, r.item.ID, "")
		}
	case "K", "J":
		if r != nil && !r.proposed {
			return m.reorder(k.String() == "K")
		}
	case "+", "-":
		if r != nil {
			return m.nudge(r.item, k.String() == "+")
		}
	case "e":
		if r != nil {
			return m.edit(r.item.ID)
		}
	case "f":
		return m.do(sit.ID+" frozen", func() error { _, err := m.st.FreezeSitting(sit.ID, false); return err })
	case "r":
		return m.do(sit.ID+" reopened", func() error { _, err := m.st.ReopenSitting(sit.ID); return err })
	case "h":
		return m.do(sit.ID+" held", func() error { _, err := m.st.HoldSitting(sit.ID, nil, nil); return err })
	case "m":
		return m.ask(pMinute, "approve the minutes of "+sit.ID+"? type yes", sit.ID, "")
	case "u":
		if r != nil {
			return m.undoItem(r.item)
		}
	case "U":
		return m.undoSitting(sit)
	case "c":
		if r != nil {
			if !m.st.CanCreateRef("office") {
				m.setStatus("no refs.office.create in the sphere's configuration", true)
				return nil
			}
			id := r.item.ID
			return m.do("dossier for "+id, func() error {
				c, err := m.st.CreateRef(id, "office")
				if err == nil && c.Known {
					return fmt.Errorf("%s already has %s", id, c.Ref)
				}
				return err
			})
		}
	case "[", "]":
		return m.step(k.String() == "]")
	case "l":
		m.view = vLive
		return nil
	case "A":
		m.back, m.view = vAgenda, vActions
		return m.loadActions()
	}
	return nil
}

// undoItem takes back the item's last step: a drop or a done (restore), a deferral (undefer).
func (m *model) undoItem(it *store.Item) tea.Cmd {
	id := it.ID
	switch it.State {
	case "dropped", "done":
		return m.do(id+" restored", func() error { _, err := m.st.RestoreItem(id, false); return err })
	case "deferred":
		return m.do(id+" undeferred", func() error { _, err := m.st.UndeferItem(id); return err })
	}
	m.setStatus(id+" is "+it.State+": nothing to take back", true)
	return nil
}

// undoSitting takes back the sitting's last step.
func (m *model) undoSitting(sit *store.Sitting) tea.Cmd {
	id := sit.ID
	switch sit.State {
	case "frozen":
		return m.do(id+" reopened", func() error { _, err := m.st.ReopenSitting(id); return err })
	case "held":
		return m.do(id+" unheld", func() error { _, err := m.st.UnholdSitting(id); return err })
	case "minuted":
		return m.ask(pUnminute, "take back the minutes of "+id+"? type yes", id, "")
	case "cancelled":
		return m.do(id+" restored", func() error { _, err := m.st.RestoreSitting(id); return err })
	}
	m.setStatus(id+" is "+sit.State+": nothing to take back", true)
	return nil
}

// reorder moves the selected item up or down the agenda.
func (m *model) reorder(up bool) tea.Cmd {
	n := len(m.agenda.Items)
	i := m.sel
	j := i + 1
	if up {
		j = i - 1
	}
	if j < 0 || j >= n {
		return nil
	}
	ids := make([]string, n)
	for x, ai := range m.agenda.Items {
		ids[x] = ai.Item.ID
	}
	ids[i], ids[j] = ids[j], ids[i]
	m.sel = j
	sit := m.agenda.Sitting.ID
	return m.do("order changed", func() error { _, err := m.st.OrderItems(sit, ids); return err })
}

// nudge adds or removes five minutes.
func (m *model) nudge(it *store.Item, more bool) tea.Cmd {
	d, _ := time.ParseDuration(it.Duration)
	if more {
		d += 5 * time.Minute
	} else if d >= 10*time.Minute {
		d -= 5 * time.Minute
	} else {
		return nil
	}
	id, v := it.ID, store.FormatDuration(d)
	return m.do(id+" "+v, func() error { _, err := m.st.EditItem(id, store.ItemInput{Duration: v}); return err })
}

// step shows the previous or next sitting of the meeting.
func (m *model) step(next bool) tea.Cmd {
	for i, s := range m.sittings {
		if s.ID != m.agenda.Sitting.ID {
			continue
		}
		j := i - 1
		if next {
			j = i + 1
		}
		if j >= 0 && j < len(m.sittings) {
			return m.loadAgenda(m.sittings[j].ID)
		}
		return nil
	}
	return nil
}

// edit opens an item's file in $EDITOR, then commits it.
func (m *model) edit(id string) tea.Cmd {
	path, err := m.st.ItemPath(id)
	if err != nil {
		m.setStatus(err.Error(), true)
		return nil
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor)
	cmd := exec.Command(parts[0], append(parts[1:], path)...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{status: id + " edited", err: m.st.CommitHandEdit("item edit " + id)}
	})
}

func (m *model) keyItem(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "q", "esc":
		m.view = m.back
		return m.reload()
	case "j", "down":
		m.scroll++
	case "k", "up":
		m.scroll = max(0, m.scroll-1)
	case "e":
		if m.item != nil {
			return m.edit(m.item.ID)
		}
	}
	return nil
}

func (m *model) liveItem() *store.Item {
	if m.agenda == nil || m.live.cur >= len(m.agenda.Items) {
		return nil
	}
	return m.agenda.Items[m.live.cur].Item
}

// pause adds the running time to the current item.
func (m *model) pause() {
	if it := m.liveItem(); it != nil && m.live.running {
		m.live.spent[it.ID] += m.now().Sub(m.live.since)
	}
	m.live.since = m.now()
}

func (m *model) spent(id string) time.Duration {
	d := m.live.spent[id]
	if it := m.liveItem(); it != nil && it.ID == id && m.live.running {
		d += m.now().Sub(m.live.since)
	}
	return d
}

func (m *model) keyLive(k tea.KeyPressMsg) tea.Cmd {
	it := m.liveItem()
	switch k.String() {
	case "esc", "q":
		m.pause()
		m.live.running = false
		m.view = vAgenda
	case "space", " ":
		m.pause()
		m.live.running = !m.live.running
		if m.live.running {
			return tick()
		}
	case "n", "j", "down":
		if m.live.cur+1 < len(m.agenda.Items) {
			m.pause()
			m.live.cur++
		}
	case "p", "k", "up":
		if m.live.cur > 0 {
			m.pause()
			m.live.cur--
		}
	case "s":
		if it != nil {
			return m.ask(pSummary, "summary of "+it.ID, it.ID, m.outcomeField(it.ID, "summary"))
		}
	case "D":
		if it != nil {
			return m.ask(pDecision, "decision on "+it.ID, it.ID, m.outcomeField(it.ID, "decision"))
		}
	case "t":
		if it != nil {
			return m.ask(pAction, "action what|who|YYYY-MM-DD", it.ID, "")
		}
	case "-":
		if it != nil {
			return m.outcome(it.ID, func(in *store.OutcomeInput) {
				in.Next = "deferred"
				if in.Summary == "" {
					in.Summary = "Not reached."
				}
			})
		}
	case "h":
		m.pause()
		m.live.running = false
		sit := m.agenda.Sitting.ID
		return m.do(sit+" held", func() error { _, err := m.st.HoldSitting(sit, nil, nil); return err })
	}
	return nil
}

func (m *model) outcomeField(id, field string) string {
	for _, r := range m.rows {
		if r.item.ID == id && r.outcome != nil {
			if field == "summary" {
				return r.outcome.Summary
			}
			return r.outcome.Decision
		}
	}
	return ""
}

func (m *model) keyActions(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "q", "esc":
		m.view = m.back
		return m.reload()
	case "j", "down":
		m.selA = min(len(m.actions)-1, m.selA+1)
	case "k", "up":
		m.selA = max(0, m.selA-1)
	case "space", " ", "enter":
		if m.selA < len(m.actions) {
			a := m.actions[m.selA]
			verb := "done"
			if a.Done {
				verb = "open again"
			}
			return m.do(fmt.Sprintf("%s#%d %s", a.Item, a.N, verb), func() error {
				_, err := m.st.SetActionDone(a.Item, a.Sitting, a.N, !a.Done)
				return err
			})
		}
	case "o":
		m.showDone = !m.showDone
		return m.loadActions()
	}
	return nil
}
