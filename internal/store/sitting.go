package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aclemen1/oj-cli/internal/spec"
)

type Sitting struct {
	ID       string     `yaml:"id" json:"id"`
	Meeting  string     `yaml:"meeting" json:"meeting"`
	Date     string     `yaml:"date" json:"date"`
	Time     string     `yaml:"time,omitempty" json:"time,omitempty"`
	Place    string     `yaml:"place,omitempty" json:"place,omitempty"`
	Duration string     `yaml:"duration,omitempty" json:"duration,omitempty"`
	State    string     `yaml:"state" json:"state"`
	Order    []string   `yaml:"order,omitempty" json:"order,omitempty"`
	Present  []string   `yaml:"present,omitempty" json:"present,omitempty"`
	Excused  []string   `yaml:"excused,omitempty" json:"excused,omitempty"`
	Reason   string     `yaml:"reason,omitempty" json:"reason,omitempty"`
	Event    string     `yaml:"event,omitempty" json:"event,omitempty"`
	Log      []LogEntry `yaml:"log,omitempty" json:"log,omitempty"`
	Notes    string     `yaml:"-" json:"notes,omitempty"`
	// Virtual: a date of the recurrence with nothing written yet.
	Virtual bool `yaml:"-" json:"virtual,omitempty"`
}

var SittingStates = []string{"planned", "frozen", "held", "minuted", "cancelled"}

func (s *Store) sittingPath(alias, key string) string {
	return filepath.Join(s.meetingDir(alias), "sittings", key+".md")
}

func virtualSitting(m *Meeting, o occurrence) *Sitting {
	return &Sitting{ID: m.Alias + "-" + o.Date, Meeting: m.Alias, Date: o.Date, Time: o.Time,
		Place: m.Place, Duration: m.Duration, State: "planned", Virtual: true}
}

// Sitting loads a sitting, written or still virtual, with its meeting.
func (s *Store) Sitting(id string) (*Sitting, *Meeting, error) {
	alias, key, err := parseSittingID(id)
	if err != nil {
		return nil, nil, err
	}
	m, err := s.Meeting(alias)
	if err != nil {
		return nil, nil, err
	}
	sit := &Sitting{}
	body, err := readDoc(s.sittingPath(alias, key), sit)
	if err == nil {
		sit.Notes = body
		return sit, m, nil
	}
	if !os.IsNotExist(err) {
		return nil, nil, err
	}
	if len(key) == 10 {
		if o, ok := m.occursOn(key); ok {
			return virtualSitting(m, o), m, nil
		}
	}
	return nil, nil, spec.NotFound("no sitting %s. List them with `oj sitting ls %s --sphere %s`", alias+"-"+key, alias, s.Sphere)
}

func (s *Store) saveSitting(sit *Sitting) error {
	if sit.Virtual {
		sit.Virtual = false
		s.log(&sit.Log, "created")
	}
	return writeDoc(s.sittingPath(sit.Meeting, strings.TrimPrefix(sit.ID, sit.Meeting+"-")), sit, sit.Notes)
}

func (s *Store) sittingFiles(alias string) ([]*Sitting, error) {
	dir := filepath.Join(s.meetingDir(alias), "sittings")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Sitting
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		sit := &Sitting{}
		body, err := readDoc(filepath.Join(dir, e.Name()), sit)
		if err != nil {
			return nil, err
		}
		sit.Notes = body
		out = append(out, sit)
	}
	return out, nil
}

func sortSittings(l []*Sitting) {
	sort.Slice(l, func(i, j int) bool {
		if l[i].Date != l[j].Date {
			return l[i].Date < l[j].Date
		}
		if l[i].Time != l[j].Time {
			return l[i].Time < l[j].Time
		}
		return l[i].ID < l[j].ID
	})
}

// Sittings lists the sittings of a meeting: written ones from since (or still
// open when since is empty), and the next ahead dates of the recurrence.
func (s *Store) Sittings(m *Meeting, since string, ahead int, state string) ([]*Sitting, error) {
	files, err := s.sittingFiles(m.Alias)
	if err != nil {
		return nil, err
	}
	today := s.today()
	written := map[string]bool{}
	var out []*Sitting
	for _, f := range files {
		written[f.ID] = true
		keep := f.Date >= today || f.State == "planned" || f.State == "frozen" || f.State == "held"
		if since != "" {
			keep = f.Date >= since
		}
		if keep {
			out = append(out, f)
		}
	}
	upcoming := 0
	for _, o := range m.occurrences(today, ahead+len(files)) {
		if upcoming >= ahead {
			break
		}
		v := virtualSitting(m, o)
		if written[v.ID] {
			continue
		}
		out = append(out, v)
		upcoming++
	}
	if state != "" && state != "all" {
		var kept []*Sitting
		for _, x := range out {
			if x.State == state {
				kept = append(kept, x)
			}
		}
		out = kept
	}
	sortSittings(out)
	return out, nil
}

// nextOpen is the first sitting on or after today, after the day `after`
// (on it too when inclusive), in one of the states, other than exclude.
func (s *Store) nextOpen(m *Meeting, after string, inclusive bool, exclude string, states ...string) (*Sitting, error) {
	today := s.today()
	ok := func(date string) bool {
		if date < today {
			return false
		}
		if inclusive {
			return date >= after
		}
		return date > after
	}
	files, err := s.sittingFiles(m.Alias)
	if err != nil {
		return nil, err
	}
	written := map[string]bool{}
	var cands []*Sitting
	for _, f := range files {
		written[f.ID] = true
		if f.ID != exclude && contains(states, f.State) && ok(f.Date) {
			cands = append(cands, f)
		}
	}
	if contains(states, "planned") {
		from := after
		if from < today {
			from = today
		}
		for _, o := range m.occurrences(from, 200) {
			v := virtualSitting(m, o)
			if written[v.ID] || v.ID == exclude || !ok(v.Date) {
				continue
			}
			cands = append(cands, v)
			break
		}
	}
	if len(cands) == 0 {
		return nil, nil
	}
	sortSittings(cands)
	return cands[0], nil
}

// Resolve returns the sitting named by an id, or the next open sitting of a meeting alias.
func (s *Store) Resolve(arg string) (*Sitting, *Meeting, error) {
	if IsSittingID(arg) {
		return s.Sitting(arg)
	}
	m, err := s.Meeting(arg)
	if err != nil {
		return nil, nil, err
	}
	sit, err := s.nextOpen(m, s.today(), true, "", "planned", "frozen")
	if err != nil {
		return nil, nil, err
	}
	if sit == nil {
		return nil, nil, spec.NotFound("meeting %s has no upcoming sitting. Add one: oj sitting add %s --date YYYY-MM-DD --sphere %s", m.Alias, m.Alias, s.Sphere)
	}
	return sit, m, nil
}

// ---------------------------------------------------------------- agenda

type AgendaItem struct {
	Start   string   `json:"start"`
	Item    *Item    `json:"item"`
	Outcome *Outcome `json:"outcome,omitempty"`
}

type Agenda struct {
	Sitting  *Sitting     `json:"sitting"`
	Title    string       `json:"title"`
	Items    []AgendaItem `json:"items"`
	Proposed []*Item      `json:"proposed"`
	Dropped  []*Item      `json:"dropped"`
	Planned  string       `json:"planned"`
	Duration string       `json:"duration,omitempty"`
	Over     bool         `json:"over,omitempty"`
}

// agendaItems returns the items of a sitting in agenda order, and its proposals.
func (s *Store) agendaItems(sit *Sitting) (agenda, proposed []*Item, err error) {
	all, err := s.items(sit.Meeting)
	if err != nil {
		return nil, nil, err
	}
	var on []*Item
	for _, it := range all {
		switch {
		case it.Sitting == sit.ID && it.State == "proposed":
			proposed = append(proposed, it)
		case it.Sitting == sit.ID && it.State != "dropped":
			on = append(on, it)
		case it.entry(sit.ID) != nil:
			on = append(on, it)
		}
	}
	rank := map[string]int{}
	for i, id := range sit.Order {
		rank[id] = i
	}
	sort.SliceStable(on, func(i, j int) bool {
		ri, oki := rank[on[i].ID]
		rj, okj := rank[on[j].ID]
		switch {
		case oki && okj:
			return ri < rj
		case oki != okj:
			return oki
		}
		return on[i].num() < on[j].num()
	})
	return on, proposed, nil
}

// Agenda shows a sitting with its ordered items, slots and proposals.
func (s *Store) Agenda(arg string) (*Agenda, error) {
	sit, m, err := s.Resolve(arg)
	if err != nil {
		return nil, err
	}
	on, proposed, err := s.agendaItems(sit)
	if err != nil {
		return nil, err
	}
	a := &Agenda{Sitting: sit, Title: m.Title, Proposed: proposed, Duration: sit.Duration, Items: []AgendaItem{}, Dropped: []*Item{}}
	if a.Proposed == nil {
		a.Proposed = []*Item{}
	}
	all, err := s.items(sit.Meeting)
	if err != nil {
		return nil, err
	}
	for _, it := range all {
		if it.Sitting == sit.ID && it.State == "dropped" {
			a.Dropped = append(a.Dropped, it)
		}
	}
	var clock time.Time
	if sit.Time != "" {
		clock, _ = time.Parse("15:04", sit.Time)
	}
	var total time.Duration
	for _, it := range on {
		start := FormatDuration(total)
		if sit.Time != "" {
			start = clock.Add(total).Format("15:04")
		}
		ai := AgendaItem{Start: start, Item: it}
		if e := it.entry(sit.ID); e != nil {
			ai.Outcome = e.Outcome
		}
		a.Items = append(a.Items, ai)
		total += minutes(it.Duration)
	}
	a.Planned = FormatDuration(total)
	a.Over = sit.Duration != "" && total > minutes(sit.Duration)
	return a, nil
}

// ---------------------------------------------------------------- changes

func requireState(sit *Sitting, action string, states ...string) error {
	if contains(states, sit.State) {
		return nil
	}
	return spec.Conflict("cannot %s sitting %s: it is %s; %s needs %s", action, sit.ID, sit.State, action, strings.Join(states, " or "))
}

// AddSitting adds a one-off sitting to a meeting.
func (s *Store) AddSitting(alias, date, at, place string) (*Sitting, error) {
	var sit *Sitting
	m, err := s.Meeting(alias)
	if err != nil {
		return nil, err
	}
	if date, err = normDate(date); err != nil {
		return nil, err
	}
	if at, err = normTime(at); err != nil {
		return nil, err
	}
	err = s.Write("sitting add "+m.Alias+"-"+date, func() error {
		key := date
		for n := 2; ; n++ {
			_, statErr := os.Stat(s.sittingPath(m.Alias, key))
			_, recurring := m.occursOn(key)
			if os.IsNotExist(statErr) && !(len(key) == 10 && recurring) {
				break
			}
			key = fmt.Sprintf("%s-%d", date, n)
		}
		if place == "" {
			place = m.Place
		}
		sit = &Sitting{ID: m.Alias + "-" + key, Meeting: m.Alias, Date: date, Time: at, Place: place,
			Duration: m.Duration, State: "planned", Virtual: true}
		return s.saveSitting(sit)
	})
	return sit, err
}

// change loads a sitting under the lock, applies fn and saves it.
func (s *Store) change(id, msg string, fn func(sit *Sitting, m *Meeting) error) (*Sitting, error) {
	var sit *Sitting
	err := s.writeAs(func() (string, error) {
		var m *Meeting
		var err error
		if sit, m, err = s.Resolve(id); err != nil {
			return "", err
		}
		if err := fn(sit, m); err != nil {
			return "", err
		}
		return msg + " " + sit.ID, s.saveSitting(sit)
	})
	return sit, err
}

// MoveSitting gives a sitting another date or time; its id keeps the original date.
func (s *Store) MoveSitting(id, date, at string) (*Sitting, error) {
	var err error
	if date != "" {
		if date, err = normDate(date); err != nil {
			return nil, err
		}
	}
	if at, err = normTime(at); err != nil {
		return nil, err
	}
	if date == "" && at == "" {
		return nil, spec.UserError("sitting move needs --date or --time. Example: oj sitting move %s --date 2026-10-09", strings.ToUpper(id))
	}
	return s.change(id, "sitting move", func(sit *Sitting, _ *Meeting) error {
		if err := requireState(sit, "move", "planned", "frozen"); err != nil {
			return err
		}
		if date != "" {
			sit.Date = date
		}
		if at != "" {
			sit.Time = at
		}
		s.log(&sit.Log, "moved to "+strings.TrimSpace(sit.Date+" "+sit.Time))
		s.emit("sitting.moved", sit.Meeting, map[string]any{"sitting": sit})
		return nil
	})
}

// moveOn plans the open items of a sitting for the next planned sitting.
func (s *Store) moveOn(m *Meeting, from *Sitting, states ...string) ([]string, error) {
	all, err := s.items(m.Alias)
	if err != nil {
		return nil, err
	}
	next, err := s.nextOpen(m, from.Date, false, from.ID, "planned")
	if err != nil {
		return nil, err
	}
	target := ""
	if next != nil {
		target = next.ID
	}
	var moved []string
	for _, it := range all {
		if it.Sitting != from.ID || !contains(states, it.State) {
			continue
		}
		it.Sitting = target
		s.log(&it.Log, "moved from "+from.ID+" to "+orNone(target))
		if err := s.saveItem(it); err != nil {
			return nil, err
		}
		s.emit("item.moved", m.Alias, map[string]any{"item": it, "from": from.ID, "to": target})
		moved = append(moved, it.ID)
	}
	return moved, nil
}

func orNone(id string) string {
	if id == "" {
		return "no sitting yet"
	}
	return id
}

type Changed struct {
	Sitting  *Sitting `json:"sitting"`
	Moved    []string `json:"moved,omitempty"`
	Rendered string   `json:"rendered,omitempty"`
}

// CancelSitting cancels a sitting; its items move to the next sitting.
func (s *Store) CancelSitting(id, reason string) (*Changed, error) {
	out := &Changed{}
	sit, err := s.change(id, "sitting cancel", func(sit *Sitting, m *Meeting) error {
		if err := requireState(sit, "cancel", "planned", "frozen"); err != nil {
			return err
		}
		sit.State, sit.Reason = "cancelled", reason
		s.log(&sit.Log, "cancelled "+reason)
		var err error
		out.Moved, err = s.moveOn(m, sit, "proposed", "accepted", "deferred")
		s.emit("sitting.cancelled", sit.Meeting, map[string]any{"sitting": sit, "moved": out.Moved})
		return err
	})
	out.Sitting = sit
	return out, err
}

// FreezeSitting closes the agenda.
func (s *Store) FreezeSitting(id string, leaveProposed bool) (*Changed, error) {
	out := &Changed{}
	sit, err := s.change(id, "sitting freeze", func(sit *Sitting, m *Meeting) error {
		if err := requireState(sit, "freeze", "planned"); err != nil {
			return err
		}
		on, proposed, err := s.agendaItems(sit)
		if err != nil {
			return err
		}
		if len(proposed) > 0 {
			var ids []string
			for _, it := range proposed {
				ids = append(ids, it.ID)
			}
			if !leaveProposed {
				return spec.Conflict("sitting %s still has proposed items: %s. Accept them (oj item accept %s) or pass --leave-proposed to move them to the next sitting",
					sit.ID, strings.Join(ids, ", "), strings.Join(ids, " "))
			}
			if out.Moved, err = s.moveOn(m, sit, "proposed"); err != nil {
				return err
			}
		}
		sit.Order = ids(on)
		sit.State = "frozen"
		s.log(&sit.Log, "frozen")
		if out.Rendered, err = s.renderFinal(sit, m, "agenda"); err != nil {
			return err
		}
		s.emit("sitting.frozen", sit.Meeting, map[string]any{"sitting": sit, "rendered": out.Rendered, "items": ids(on)})
		return nil
	})
	out.Sitting = sit
	return out, err
}

// ReopenSitting reopens a frozen agenda.
func (s *Store) ReopenSitting(id string) (*Sitting, error) {
	return s.change(id, "sitting reopen", func(sit *Sitting, _ *Meeting) error {
		if err := requireState(sit, "reopen", "frozen"); err != nil {
			return err
		}
		sit.State = "planned"
		s.log(&sit.Log, "reopened")
		s.emit("sitting.reopened", sit.Meeting, map[string]any{"sitting": sit})
		return nil
	})
}

// HoldSitting records that the sitting took place.
func (s *Store) HoldSitting(id string, present, excused []string) (*Sitting, error) {
	return s.change(id, "sitting hold", func(sit *Sitting, _ *Meeting) error {
		if err := requireState(sit, "hold", "planned", "frozen"); err != nil {
			return err
		}
		on, _, err := s.agendaItems(sit)
		if err != nil {
			return err
		}
		sit.Order = ids(on)
		sit.State = "held"
		if present != nil {
			sit.Present = present
		}
		if excused != nil {
			sit.Excused = excused
		}
		s.log(&sit.Log, "held")
		s.emit("sitting.held", sit.Meeting, map[string]any{"sitting": sit})
		return nil
	})
}

type Minuted struct {
	Sitting  *Sitting `json:"sitting"`
	Done     []string `json:"done"`
	Deferred []string `json:"deferred"`
	Rendered string   `json:"rendered,omitempty"`
	Moved    []string `json:"moved,omitempty"`
	Approved int      `json:"approved"`
}

// MinuteSitting approves the outcomes and defers what was not dealt with.
func (s *Store) MinuteSitting(id string) (*Minuted, error) {
	out := &Minuted{Done: []string{}, Deferred: []string{}}
	sit, err := s.change(id, "sitting minute", func(sit *Sitting, m *Meeting) error {
		if err := requireState(sit, "minute", "held"); err != nil {
			return err
		}
		on, _, err := s.agendaItems(sit)
		if err != nil {
			return err
		}
		next, err := s.nextOpen(m, sit.Date, false, sit.ID, "planned")
		if err != nil {
			return err
		}
		target := ""
		if next != nil {
			target = next.ID
		}
		for _, it := range on {
			if it.Sitting != sit.ID || (it.State != "accepted" && it.State != "deferred") {
				continue
			}
			e := it.entry(sit.ID)
			if e == nil {
				it.History = append(it.History, Entry{Sitting: sit.ID})
				e = &it.History[len(it.History)-1]
			}
			if e.Outcome != nil && e.Outcome.Status == "draft" {
				e.Outcome.Status = "approved"
				out.Approved++
			}
			if e.Outcome != nil && e.Outcome.Next == "done" {
				e.Result, it.State, it.DeferredFrom = "done", "done", ""
				s.log(&it.Log, "done at "+sit.ID)
				out.Done = append(out.Done, it.ID)
			} else {
				e.Result, it.State, it.Sitting, it.DeferredFrom = "deferred", "deferred", target, sit.ID
				s.log(&it.Log, "deferred from "+sit.ID+" to "+orNone(target))
				out.Deferred = append(out.Deferred, it.ID)
				s.emit("item.deferred", it.Meeting, map[string]any{"item": it, "from": sit.ID, "to": target})
			}
			if err := s.saveItem(it); err != nil {
				return err
			}
		}
		if out.Moved, err = s.moveOn(m, sit, "proposed"); err != nil {
			return err
		}
		sit.State = "minuted"
		s.log(&sit.Log, "minuted")
		if out.Rendered, err = s.renderFinal(sit, m, "minutes"); err != nil {
			return err
		}
		s.emit("sitting.minuted", sit.Meeting, map[string]any{"sitting": sit, "rendered": out.Rendered,
			"done": out.Done, "deferred": out.Deferred})
		return nil
	})
	out.Sitting = sit
	return out, err
}

func ids(l []*Item) []string {
	var out []string
	for _, it := range l {
		out = append(out, it.ID)
	}
	return out
}
