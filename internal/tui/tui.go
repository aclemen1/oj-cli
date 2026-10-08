// Package tui is oj's terminal interface (SPEC §11): meetings, the agenda
// of a sitting, an item, the live sitting with a timer, and open actions.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/aclemen1/oj-cli/internal/actions"
	"github.com/aclemen1/oj-cli/internal/spec"
	"github.com/aclemen1/oj-cli/internal/store"
)

func init() {
	spec.Register(&spec.Action{
		Category: "setup", Name: "tui", Top: true,
		Summary:  "Open the terminal interface: meetings, agenda, live sitting, actions, of every sphere (s filters).",
		Params:   []spec.Param{{Name: "sphere", Kind: spec.String, Help: "Sphere to show. Defaults to every sphere."}},
		Effects:  []string{"Runs until q; every change goes through the same store actions as the CLI."},
		Examples: []string{"oj tui --sphere pro"},
		Run: func(ctx *spec.Context) (any, error) {
			stores, err := actions.OpenRead(ctx)
			if err != nil {
				return nil, err
			}
			m := newModel(stores...)
			var names []string
			for _, st := range stores {
				names = append(names, st.Sphere)
			}
			m.statePath = statePath(names)
			_, err = tea.NewProgram(m).Run()
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
	vSittings
	vStanding
	vDoc
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
	pMakeStanding
	pNewStanding
)

// row is a line of the agenda: an item on it, a proposal, a dropped item, or
// an item deferred from this sitting to a later one.
type row struct {
	item     *store.Item
	outcome  *store.Outcome
	start    string
	proposed bool
	dropped  bool
	away     bool
}

// ovRow is a line of the sittings view: a meeting, a sitting, or an item.
type ovRow struct {
	sphere           string
	meeting, sitting string
	item             *store.Item
	agenda           *store.Agenda
	unplanned        bool
}

type meetingRow struct {
	sphere                       string
	alias, title, next, nextDate string
	accepted, proposed           int
	// pending is a held sitting whose minutes are not approved yet: enter opens it first.
	pending string
}

type live struct {
	cur     int
	running bool
	since   time.Time
	spent   map[string]time.Duration
}

type model struct {
	// stores are the spheres shown; st is the sphere of the open meeting, where changes go.
	stores []*store.Store
	st     *store.Store
	// filter keeps one sphere in the lists, "" every sphere.
	filter string
	now    func() time.Time

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

	ovScope string // a meeting alias, or "" for every meeting
	ovRows  []ovRow
	selO    int
	// pick is the item to select once the next agenda is loaded.
	pick string

	// moving is the item whose sitting the user is choosing among choices.
	moving  *store.Item
	choices []*store.Sitting
	selC    int

	live live

	prompt prompt
	input  textinput.Model
	target string

	w, h      int
	status    string
	statusErr bool
	warnings  warnings
	// helpOff hides the help panel, open by default.
	helpOff bool

	// statePath keeps the TUI's state across restarts ("" in tests).
	statePath, lastSaved        string
	restoreMeeting, restoreLive string
	// keepView: the next agenda loads for a restored view, which it must not replace.
	keepView bool

	// refs caches what the sphere's ref commands say, by ref.
	refs map[string]refEntry

	// stamp is the stores' fingerprint at the last look; a change reloads the view.
	stamp string

	// standing lists the recurring items of the open meeting.
	standing []store.Standing
	selR     int

	// doc is the preview of the open sitting's agenda or minutes.
	doc docMsg
}

type docMsg struct {
	kind, text string
	final      bool
	err        error
}

// docKind is the document a sitting is at: its minutes once held, else its agenda.
func docKind(s *store.Sitting) string {
	if s.State == "held" || s.State == "minuted" {
		return "minutes"
	}
	return "agenda"
}

func (m *model) loadDoc() tea.Cmd {
	if m.agenda == nil {
		return nil
	}
	st, id, kind := m.st, m.agenda.Sitting.ID, docKind(m.agenda.Sitting)
	return func() tea.Msg {
		r, err := st.RenderDoc(id, kind, "md", "")
		if err != nil {
			return docMsg{kind: kind, err: err}
		}
		b, err := os.ReadFile(r.Markdown)
		return docMsg{kind: kind, text: string(b), final: r.Final, err: err}
	}
}

// ask sends the sphere's request to the meeting's agent, e.g. outcomes from a transcript.
func (m *model) askAgent(request string) tea.Cmd {
	if m.agenda == nil {
		return nil
	}
	if !m.st.CanAsk(m.agenda.Sitting.Meeting, request) {
		m.setStatus(m.tr("rien à demander : la sphère ne déclare pas asks."+request+", ou la séance n'a pas de ref avec une commande ask",
			"nothing to ask: the sphere declares no asks."+request+", or the meeting has no ref with an ask command"), true)
		return nil
	}
	id := m.agenda.Sitting.ID
	return m.do(m.tr("demande envoyée : ", "request sent: ")+request, func() error { _, err := m.st.Ask(id, request); return err })
}

type standingMsg struct {
	list []store.Standing
	err  error
}

func (m *model) loadStanding() tea.Cmd {
	st, alias := m.st, m.meeting
	return func() tea.Msg {
		mt, err := st.Meeting(alias)
		if err != nil {
			return standingMsg{err: err}
		}
		return standingMsg{list: mt.Standing}
	}
}

// keyStanding: the recurring items of the open meeting.
func (m *model) keyStanding(k tea.KeyPressMsg) tea.Cmd {
	alias := m.meeting
	var cur *store.Standing
	if m.selR < len(m.standing) {
		cur = &m.standing[m.selR]
	}
	switch k.String() {
	case "esc":
		m.view = vAgenda
		return m.reload()
	case "j", "down":
		m.selR = min(len(m.standing)-1, m.selR+1)
	case "k", "up":
		m.selR = max(0, m.selR-1)
	case "n":
		return m.ask(pNewStanding, m.tr("nouveau point récurrent (à la fin)", "new recurring item (at the end)"), "", "")
	case "s":
		if cur != nil {
			key, place := cur.Key, "start"
			if cur.Place == "start" {
				place = "end"
			}
			return m.do(key+" → "+place, func() error { _, err := m.st.SetStandingPlace(alias, key, place); return err })
		}
	case "x":
		if cur != nil {
			key := cur.Key
			return m.do(key+m.tr(" arrêté", " stopped"), func() error { _, err := m.st.RemoveStanding(alias, key); return err })
		}
	}
	return nil
}

// watchEvery is how often the TUI looks for changes made by other processes
// (office, agents, the calendar sync); 0 turns the watch off.
var watchEvery = 2 * time.Second

type watchMsg struct{ stamp string }

// watch looks at the stores' fingerprint after watchEvery.
func (m *model) watch() tea.Cmd {
	if watchEvery == 0 {
		return nil
	}
	stores := m.stores
	return tea.Tick(watchEvery, func(time.Time) tea.Msg { return watchMsg{stamps(stores)} })
}

func stamps(stores []*store.Store) string {
	var b strings.Builder
	for _, st := range stores {
		s, _ := st.Stamp()
		b.WriteString(st.Sphere + "=" + s + ";")
	}
	return b.String()
}

type refEntry struct {
	shown   store.RefShown
	at      time.Time
	loading bool
}

const refFresh = time.Minute

func newModel(stores ...*store.Store) *model {
	in := textinput.New()
	styles := in.Styles()
	styles.Cursor.Blink = false
	in.SetStyles(styles)
	m := &model{stores: stores, st: stores[0], now: time.Now, input: in, w: 100, h: 30, live: live{spent: map[string]time.Duration{}},
		refs: map[string]refEntry{}}
	for _, st := range stores {
		st.Warn = m.warnings.add
	}
	return m
}

// multi is true when the TUI shows several spheres.
func (m *model) multi() bool { return len(m.stores) > 1 }

// shown are the stores the filter keeps.
func (m *model) shown() []*store.Store {
	if m.filter == "" {
		return m.stores
	}
	for _, st := range m.stores {
		if st.Sphere == m.filter {
			return []*store.Store{st}
		}
	}
	return m.stores
}

// storeOf is the store of a sphere, or the current one.
func (m *model) storeOf(sphere string) *store.Store {
	for _, st := range m.stores {
		if st.Sphere == sphere {
			return st
		}
	}
	return m.st
}

// enter makes a sphere the current one, for the meeting about to open.
func (m *model) enter(sphere string) {
	if sphere != "" {
		m.st = m.storeOf(sphere)
	}
}

// cycleFilter goes from every sphere to each sphere in turn, then back.
func (m *model) cycleFilter() {
	next := ""
	if m.filter == "" {
		next = m.stores[0].Sphere
	} else {
		for i, st := range m.stores {
			if st.Sphere == m.filter && i+1 < len(m.stores) {
				next = m.stores[i+1].Sphere
			}
		}
	}
	m.filter = next
	m.selM, m.selO, m.selA = 0, 0, 0
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
	stores := m.shown()
	since := m.now().AddDate(0, -3, 0).Format("2006-01-02")
	return func() tea.Msg {
		var rows []meetingRow
		for _, st := range stores {
			ms, err := st.Meetings()
			if err != nil {
				return meetingsMsg{err: err}
			}
			for _, mt := range ms {
				r := meetingRow{sphere: st.Sphere, alias: mt.Alias, title: mt.Title}
				if a, err := st.Agenda(mt.Alias); err == nil {
					r.next, r.nextDate, r.accepted, r.proposed = a.Sitting.ID, a.Sitting.Date, len(a.Items), len(a.Proposed)
				}
				if held, err := st.Sittings(mt, since, 0, "held"); err == nil && len(held) > 0 {
					r.pending = held[len(held)-1].ID
				}
				rows = append(rows, r)
			}
		}
		return meetingsMsg{rows: rows}
	}
}

func (m *model) loadAgenda(arg string) tea.Cmd {
	st, since := m.st, m.now().AddDate(-1, 0, 0).Format("2006-01-02")
	return func() tea.Msg {
		a, err := st.Agenda(arg)
		if err != nil {
			return agendaMsg{err: err}
		}
		mt, err := st.Meeting(a.Sitting.Meeting)
		if err != nil {
			return agendaMsg{err: err}
		}
		sits, err := st.Sittings(mt, since, 8, "all")
		return agendaMsg{agenda: a, sittings: sits, err: err}
	}
}

func (m *model) loadItem(id string) tea.Cmd {
	st := m.st
	return func() tea.Msg {
		it, err := st.Item(id)
		return itemMsg{it, err}
	}
}

func (m *model) loadActions() tea.Cmd {
	state := "open"
	if m.showDone {
		state = "all"
	}
	stores := m.shown()
	return func() tea.Msg {
		rows := []store.ActionRow{}
		for _, st := range stores {
			l, err := st.Actions(store.ActionFilter{State: state})
			if err != nil {
				return actionsMsg{nil, err}
			}
			rows = append(rows, l...)
		}
		store.SortActions(rows)
		return actionsMsg{rows, nil}
	}
}

type overviewMsg struct {
	ovs []store.Overview
	err error
}

func (m *model) loadOverview() tea.Cmd {
	scope := m.ovScope
	since := m.now().AddDate(0, 0, -30).Format("2006-01-02")
	stores := m.shown()
	if scope != "" {
		stores = []*store.Store{m.st}
	}
	return func() tea.Msg {
		var all []store.Overview
		for _, st := range stores {
			ovs, err := st.Overviews(scope, since, 4)
			if err != nil {
				return overviewMsg{nil, err}
			}
			all = append(all, ovs...)
		}
		return overviewMsg{all, nil}
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
	case vSittings:
		return m.loadOverview()
	case vStanding:
		return m.loadStanding()
	case vDoc:
		return m.loadDoc()
	}
	return nil
}

// do runs a store change and reports it.
func (m *model) do(status string, f func() error) tea.Cmd {
	return func() tea.Msg { return doneMsg{status: status, err: f()} }
}

func tick() tea.Cmd { return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} }) }

// ------------------------------------------------------------------ update

func (m *model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, m.loadMeetings(), m.restore(), m.watch())
}

func (m *model) setStatus(s string, isErr bool) { m.status, m.statusErr = s, isErr }

// Update handles a message, then keeps the state for the next start.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	m.persist()
	return next, cmd
}

func (m *model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		if m.restoreMeeting != "" {
			for i, r := range m.meetings {
				if r.alias == m.restoreMeeting && r.sphere == m.st.Sphere {
					m.selM = i
				}
			}
			m.restoreMeeting = ""
		}
		m.selM = min(m.selM, max(0, len(m.meetings)-1))
	case agendaMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			break
		}
		if !m.keepView && (m.agenda == nil || m.agenda.Sitting.ID != msg.agenda.Sitting.ID) {
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
		for _, it := range msg.agenda.Deferred {
			m.rows = append(m.rows, row{item: it, away: true})
		}
		for _, it := range msg.agenda.Dropped {
			m.rows = append(m.rows, row{item: it, dropped: true})
		}
		if m.pick != "" {
			for i, r := range m.rows {
				if r.item.ID == m.pick {
					m.sel = i
				}
			}
			m.pick = ""
		}
		m.sel = min(m.sel, max(0, len(m.rows)-1))
		if m.restoreLive != "" {
			for i, ai := range msg.agenda.Items {
				if ai.Item.ID == m.restoreLive {
					m.live.cur = i
				}
			}
			m.restoreLive = ""
		}
		m.live.cur = min(m.live.cur, max(0, len(msg.agenda.Items)-1))
		if m.keepView {
			m.keepView = false
		} else if m.view != vLive {
			m.view = vAgenda
		}
		return m, m.fetchRefs()
	case refMsg:
		m.refs[msg.shown.Ref] = refEntry{shown: msg.shown, at: m.now()}
	case overviewMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			break
		}
		m.ovRows = nil
		for _, ov := range msg.ovs {
			sp := ov.Sphere
			m.ovRows = append(m.ovRows, ovRow{sphere: sp, meeting: ov.Meeting})
			for _, a := range ov.Sittings {
				m.ovRows = append(m.ovRows, ovRow{sphere: sp, meeting: ov.Meeting, sitting: a.Sitting.ID, agenda: a})
				for _, ai := range a.Items {
					m.ovRows = append(m.ovRows, ovRow{sphere: sp, meeting: ov.Meeting, sitting: a.Sitting.ID, item: ai.Item})
				}
				for _, it := range a.Proposed {
					m.ovRows = append(m.ovRows, ovRow{sphere: sp, meeting: ov.Meeting, sitting: a.Sitting.ID, item: it})
				}
			}
			for _, it := range ov.Unplanned {
				m.ovRows = append(m.ovRows, ovRow{sphere: sp, meeting: ov.Meeting, item: it, unplanned: true})
			}
		}
		m.selO = min(m.selO, max(0, len(m.ovRows)-1))
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
	case watchMsg:
		// A change waits while the user types or picks a sitting: the stamp
		// is kept, so the next look finds it again.
		if m.stamp == "" || msg.stamp == m.stamp {
			m.stamp = msg.stamp
			return m, m.watch()
		}
		if m.prompt != pNone || m.moving != nil {
			return m, m.watch()
		}
		m.stamp = msg.stamp
		return m, tea.Batch(m.watch(), m.reload())
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
		// Convention of the ecosystem's TUIs: q quits from any view, esc goes back.
		if msg.String() == "q" {
			if m.view == vLive {
				m.pause()
			}
			return m, tea.Quit
		}
		if msg.String() == "?" {
			m.helpOff = !m.helpOff
			return m, nil
		}
		if m.moving != nil {
			return m, m.keyPicker(msg)
		}
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
		case vSittings:
			return m, m.keySittings(msg)
		case vStanding:
			return m, m.keyStanding(msg)
		case vDoc:
			switch msg.String() {
			case "esc":
				m.view = vAgenda
				return m, m.reload()
			case "j", "down":
				m.scroll++
			case "k", "up":
				m.scroll = max(0, m.scroll-1)
			case "G":
				return m, m.askAgent("outcomes")
			}
		}
	case docMsg:
		m.doc = msg
	case standingMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			break
		}
		m.standing = msg.list
		m.selR = min(m.selR, max(0, len(m.standing)-1))
	}
	return m, nil
}

// startMove opens the choice of a planned sitting for an item.
func (m *model) startMove(it *store.Item) tea.Cmd {
	today := m.now().Format("2006-01-02")
	m.choices = nil
	for _, s := range m.sittings {
		if s.State == "planned" && s.Date >= today && s.ID != it.Sitting {
			m.choices = append(m.choices, s)
		}
	}
	if len(m.choices) == 0 {
		m.setStatus(m.tr("aucune autre séance planifiée", "no other planned sitting"), true)
		return nil
	}
	m.moving, m.selC = it, 0
	return nil
}

func (m *model) keyPicker(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.moving = nil
	case "j", "down":
		m.selC = min(len(m.choices)-1, m.selC+1)
	case "k", "up":
		m.selC = max(0, m.selC-1)
	case "enter":
		id, to := m.moving.ID, m.choices[m.selC].ID
		m.moving = nil
		return m.do(id+" → "+to, func() error { _, err := m.st.MoveItem(id, to); return err })
	}
	return nil
}

// openOverview shows the sittings of a meeting, or of every meeting.
func (m *model) openOverview(scope string, back view) tea.Cmd {
	if m.ovScope != scope {
		m.selO = 0
	}
	m.ovScope, m.back, m.view = scope, back, vSittings
	return m.loadOverview()
}

func (m *model) keySittings(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.view = m.back
		return m.reload()
	case "s":
		if m.multi() && m.ovScope == "" {
			m.cycleFilter()
			return m.loadOverview()
		}
	case "j", "down":
		m.selO = min(len(m.ovRows)-1, m.selO+1)
	case "k", "up":
		m.selO = max(0, m.selO-1)
	case "enter":
		if m.selO >= len(m.ovRows) {
			return nil
		}
		r := m.ovRows[m.selO]
		m.enter(r.sphere)
		switch {
		case r.item != nil && r.unplanned:
			m.back, m.view, m.scroll, m.item = vSittings, vItem, 0, r.item
			return m.loadItem(r.item.ID)
		case r.sitting != "":
			if r.item != nil {
				m.pick = r.item.ID
			}
			m.agenda = nil
			return m.loadAgenda(r.sitting)
		case r.meeting != "":
			m.agenda = nil
			return m.loadAgenda(r.meeting)
		}
	}
	return nil
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
	case pMakeStanding:
		place := map[string]string{"s": "start", "start": "start", "d": "start", "début": "start", "e": "end", "end": "end", "f": "end", "fin": "end", "": "end"}[strings.ToLower(v)]
		if place == "" {
			m.setStatus(m.tr("répondre début ou fin", "answer start or end"), true)
			return nil
		}
		return m.do(target+m.tr(" devient récurrent", " made recurring"), func() error { _, err := m.st.MakeStanding(target, place, ""); return err })
	case pNewStanding:
		if v == "" {
			return nil
		}
		alias := m.meeting
		return m.do(m.tr("point récurrent ajouté", "recurring item added"), func() error {
			_, err := m.st.AddStanding(alias, store.Standing{Title: v, Place: "end"})
			return err
		})
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
	case "j", "down":
		m.selM = min(len(m.meetings)-1, m.selM+1)
	case "k", "up":
		m.selM = max(0, m.selM-1)
	case "enter":
		if m.selM < len(m.meetings) {
			m.agenda = nil
			r := m.meetings[m.selM]
			m.enter(r.sphere)
			if r.pending != "" {
				return m.loadAgenda(r.pending)
			}
			return m.loadAgenda(r.alias)
		}
	case "s":
		if m.multi() {
			m.cycleFilter()
			return m.loadMeetings()
		}
	case "A":
		m.back, m.view = vMeetings, vActions
		return m.loadActions()
	case "S":
		return m.openOverview("", vMeetings)
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
	// A gesture on a recurring item still virtual writes it first.
	switch k.String() {
	case "enter", "a", "d", "x", "+", "-", "e", "c", "u", "o":
		if r != nil && r.item.Virtual {
			if _, err := m.real(r.item); err != nil {
				m.setStatus(err.Error(), true)
				return nil
			}
			r = m.current()
		}
	case "K", "J":
		if err := m.realAll(); err != nil {
			m.setStatus(err.Error(), true)
			return nil
		}
		r = m.current()
	}
	switch k.String() {
	case "esc":
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
	case "*":
		if r != nil {
			if k := r.item.StandingKey(); k != "" {
				m.setStatus(m.tr("déjà récurrent ("+k+") : R pour la liste", "already recurring ("+k+"): R for the list"), true)
				return nil
			}
			return m.ask(pMakeStanding, m.tr(r.item.ID+" récurrent, au début ou à la fin ?", r.item.ID+" recurring, at the start or the end?"), r.item.ID, m.tr("fin", "end"))
		}
	case "R":
		m.view, m.selR = vStanding, 0
		return m.loadStanding()
	case "P":
		m.view, m.scroll, m.doc = vDoc, 0, docMsg{}
		return m.loadDoc()
	case "G":
		return m.askAgent("outcomes")
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
		id := sit.ID
		return func() tea.Msg {
			ch, err := m.st.FreezeSitting(id, false)
			if err != nil {
				return doneMsg{err: err}
			}
			var names []string
			for _, f := range ch.Files {
				names = append(names, filepath.Base(f))
			}
			status := m.tr(id+" figée : ", id+" frozen: ") + strings.Join(names, ", ")
			if ch.Unchanged {
				status += m.tr(" (ordre du jour inchangé, version précédente gardée)", " (agenda unchanged, previous version kept)")
			}
			return doneMsg{status: status}
		}
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
	case "o":
		if r != nil {
			return m.jump(r.item)
		}
	case "M":
		if r != nil && (r.item.State == "proposed" || r.item.State == "accepted" || r.item.State == "deferred") {
			return m.startMove(r.item)
		}
	case "c":
		if r != nil {
			scheme, err := m.st.CreateScheme("")
			if err != nil {
				m.setStatus(err.Error(), true)
				return nil
			}
			id := r.item.ID
			return m.do(scheme+" target created for "+id, func() error {
				c, err := m.st.CreateRef(id, scheme)
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
	case "S":
		return m.openOverview(m.meeting, vAgenda)
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
	case "esc":
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
	case "o":
		if m.item != nil {
			return m.jump(m.item)
		}
	}
	return nil
}

// jumpRef is the first ref of the item the sphere can open, or "".
func (m *model) jumpRef(it *store.Item) string {
	for _, r := range it.Refs {
		if m.st.CanOpenRef(r) {
			return r
		}
	}
	return ""
}

// jump opens the item's ref with the sphere's open command (e.g. the dossier's session).
func (m *model) jump(it *store.Item) tea.Cmd {
	ref := m.jumpRef(it)
	if ref == "" {
		m.setStatus(m.tr("aucune ref à ouvrir pour ce point (refs.<schéma>.open)", "no ref to open for this item (refs.<scheme>.open)"), true)
		return nil
	}
	return m.do("→ "+ref, func() error { return m.st.OpenRef(ref) })
}

// real writes a recurring item that is still virtual and puts the written
// item in its place on screen; the live timer follows it.
func (m *model) real(it *store.Item) (*store.Item, error) {
	if it == nil || !it.Virtual {
		return it, nil
	}
	made, err := m.st.ApplyStanding(m.agenda.Sitting.ID, it.Standing)
	if err != nil {
		return nil, err
	}
	if len(made) == 0 {
		return nil, fmt.Errorf("recurring item %s was not written", it.Standing)
	}
	w := made[0]
	if d, ok := m.live.spent[spentKey(it)]; ok {
		m.live.spent[w.ID] += d
		delete(m.live.spent, spentKey(it))
	}
	for i := range m.rows {
		if m.rows[i].item == it {
			m.rows[i].item = w
		}
	}
	for i := range m.agenda.Items {
		if m.agenda.Items[i].Item == it {
			m.agenda.Items[i].Item = w
		}
	}
	return w, nil
}

// realAll writes every recurring item of the sitting still virtual, and reloads the agenda.
func (m *model) realAll() error {
	virtual := false
	for _, ai := range m.agenda.Items {
		virtual = virtual || ai.Item.Virtual
	}
	if !virtual {
		return nil
	}
	if _, err := m.st.ApplyStanding(m.agenda.Sitting.ID, ""); err != nil {
		return err
	}
	m.update(m.loadAgenda(m.agenda.Sitting.ID)())
	return nil
}

// spentKey names an item in the live timer, written or not.
func spentKey(it *store.Item) string {
	if it.Virtual {
		return "standing:" + it.Standing
	}
	return it.ID
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
		m.live.spent[spentKey(it)] += m.now().Sub(m.live.since)
	}
	m.live.since = m.now()
}

func (m *model) spent(of *store.Item) time.Duration {
	d := m.live.spent[spentKey(of)]
	if it := m.liveItem(); it != nil && it == of && m.live.running {
		d += m.now().Sub(m.live.since)
	}
	return d
}

func (m *model) keyLive(k tea.KeyPressMsg) tea.Cmd {
	it := m.liveItem()
	switch k.String() {
	case "s", "D", "t", "o", "-":
		if it != nil && it.Virtual {
			m.pause()
			w, err := m.real(it)
			if err != nil {
				m.setStatus(err.Error(), true)
				return nil
			}
			it = w
		}
	}
	switch k.String() {
	case "esc":
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
	case "o":
		if it != nil {
			return m.jump(it)
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
	case "esc":
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
			st := m.storeOf(a.Sphere)
			return m.do(fmt.Sprintf("%s#%d %s", a.Item, a.N, verb), func() error {
				_, err := st.SetActionDone(a.Item, a.Sitting, a.N, !a.Done)
				return err
			})
		}
	case "o":
		m.showDone = !m.showDone
		return m.loadActions()
	case "s":
		if m.multi() {
			m.cycleFilter()
			return m.loadActions()
		}
	}
	return nil
}
