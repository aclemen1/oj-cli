package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aclemen1/ordo-cli/internal/spec"
)

type Item struct {
	ID          string     `yaml:"id" json:"id"`
	Meeting     string     `yaml:"meeting" json:"meeting"`
	Title       string     `yaml:"title" json:"title"`
	Owner       string     `yaml:"owner,omitempty" json:"owner,omitempty"`
	Kind        string     `yaml:"kind" json:"kind"`
	Duration    string     `yaml:"duration,omitempty" json:"duration,omitempty"`
	Expected    string     `yaml:"expected,omitempty" json:"expected,omitempty"`
	Attachments []string   `yaml:"attachments,omitempty" json:"attachments,omitempty"`
	Refs        []string   `yaml:"refs,omitempty" json:"refs,omitempty"`
	Sitting     string     `yaml:"sitting,omitempty" json:"sitting,omitempty"`
	State       string     `yaml:"state" json:"state"`
	Reason      string     `yaml:"reason,omitempty" json:"reason,omitempty"`
	History     []Entry    `yaml:"history,omitempty" json:"history,omitempty"`
	Log         []LogEntry `yaml:"log,omitempty" json:"log,omitempty"`
	Notes       string     `yaml:"-" json:"notes,omitempty"`
}

// Entry is what happened to an item in one sitting.
type Entry struct {
	Sitting string   `yaml:"sitting" json:"sitting"`
	Outcome *Outcome `yaml:"outcome,omitempty" json:"outcome,omitempty"`
	Result  string   `yaml:"result,omitempty" json:"result,omitempty"`
}

type Outcome struct {
	Summary  string       `yaml:"summary,omitempty" json:"summary,omitempty"`
	Decision string       `yaml:"decision,omitempty" json:"decision,omitempty"`
	Actions  []ActionItem `yaml:"actions,omitempty" json:"actions,omitempty"`
	Next     string       `yaml:"next" json:"next"`
	By       string       `yaml:"by" json:"by"`
	Status   string       `yaml:"status" json:"status"`
	At       string       `yaml:"at" json:"at"`
}

type ActionItem struct {
	What string `yaml:"what" json:"what"`
	Who  string `yaml:"who,omitempty" json:"who,omitempty"`
	Due  string `yaml:"due,omitempty" json:"due,omitempty"`
	Ref  string `yaml:"ref,omitempty" json:"ref,omitempty"`
	Done bool   `yaml:"done,omitempty" json:"done,omitempty"`
}

var ItemStates = []string{"proposed", "accepted", "deferred", "done", "dropped"}

func (it *Item) num() int {
	_, n, _ := parseItemID(it.ID)
	return n
}

func (it *Item) entry(sitting string) *Entry {
	for i := range it.History {
		if it.History[i].Sitting == sitting {
			return &it.History[i]
		}
	}
	return nil
}

func (s *Store) itemPath(alias, id string) string {
	return filepath.Join(s.meetingDir(alias), "items", id+".md")
}

// Item loads an item by id.
func (s *Store) Item(id string) (*Item, error) {
	alias, n, err := parseItemID(id)
	if err != nil {
		return nil, err
	}
	full := alias + "-" + strconv.Itoa(n)
	it := &Item{}
	body, err := readDoc(s.itemPath(alias, full), it)
	if os.IsNotExist(err) {
		return nil, spec.NotFound("no item %s in sphere %s. Find it with `ordo item ls %s --sphere %s`", full, s.Sphere, alias, s.Sphere)
	}
	if err != nil {
		return nil, err
	}
	it.Notes = body
	return it, nil
}

func (s *Store) saveItem(it *Item) error {
	return writeDoc(s.itemPath(it.Meeting, it.ID), it, it.Notes)
}

func (s *Store) items(alias string) ([]*Item, error) {
	dir := filepath.Join(s.meetingDir(alias), "items")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Item
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		it := &Item{}
		body, err := readDoc(filepath.Join(dir, e.Name()), it)
		if err != nil {
			return nil, err
		}
		it.Notes = body
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].num() < out[j].num() })
	return out, nil
}

// ItemFilter selects items for Items.
type ItemFilter struct {
	Meeting, State, Owner, Ref, Search, Sitting string
}

// Items lists items across the sphere's meetings or in one.
func (s *Store) Items(f ItemFilter) ([]*Item, error) {
	var aliases []string
	if f.Meeting != "" {
		m, err := s.Meeting(f.Meeting)
		if err != nil {
			return nil, err
		}
		aliases = []string{m.Alias}
	} else {
		ms, err := s.Meetings()
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			aliases = append(aliases, m.Alias)
		}
	}
	q := strings.ToLower(f.Search)
	out := []*Item{}
	for _, a := range aliases {
		l, err := s.items(a)
		if err != nil {
			return nil, err
		}
		for _, it := range l {
			switch f.State {
			case "", "open":
				if it.State != "proposed" && it.State != "accepted" && it.State != "deferred" {
					continue
				}
			case "all":
			default:
				if it.State != f.State {
					continue
				}
			}
			if f.Owner != "" && !strings.EqualFold(it.Owner, f.Owner) {
				continue
			}
			if f.Ref != "" && !contains(it.Refs, f.Ref) {
				continue
			}
			if f.Sitting != "" && !strings.EqualFold(it.Sitting, f.Sitting) {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(it.Title+"\n"+it.Expected+"\n"+it.Notes), q) {
				continue
			}
			out = append(out, it)
		}
	}
	return out, nil
}

// ItemInput carries the fields of item add and edit.
type ItemInput struct {
	Title, Owner, Kind, Duration, Expected, Notes string
	Attach, Refs                                  []string
}

func (in *ItemInput) check() error {
	if in.Kind != "" && !contains(Kinds, in.Kind) {
		return spec.UserError("kind %q: expected info, discussion or decision", in.Kind)
	}
	d, err := NormDuration(in.Duration)
	in.Duration = d
	return err
}

// target checks an explicit sitting for an item of meeting m.
func (s *Store) target(m *Meeting, id string) (*Sitting, error) {
	sit, sm, err := s.Sitting(id)
	if err != nil {
		return nil, err
	}
	if sm.Alias != m.Alias {
		return nil, spec.UserError("sitting %s belongs to meeting %s, not %s", sit.ID, sm.Alias, m.Alias)
	}
	if sit.State != "planned" {
		hint := ""
		if sit.State == "frozen" {
			hint = fmt.Sprintf("; reopen it first with `ordo sitting reopen %s`", sit.ID)
		}
		return nil, spec.Conflict("sitting %s is %s; items go only to a planned sitting%s", sit.ID, sit.State, hint)
	}
	return sit, nil
}

// AddItem proposes an item, accepted at once with accept. sitting may be
// empty: the next planned sitting of the meeting.
func (s *Store) AddItem(alias string, in ItemInput, accept bool, sitting string) (*Item, error) {
	m, err := s.Meeting(alias)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, spec.UserError("item add needs a title. Example: ordo item add %s \"Budget 2027\" --sphere %s", m.Alias, s.Sphere)
	}
	if err := in.check(); err != nil {
		return nil, err
	}
	var it *Item
	err = s.writeAs(func() (string, error) {
		m, err := s.Meeting(m.Alias)
		if err != nil {
			return "", err
		}
		var sit *Sitting
		if sitting != "" {
			if sit, err = s.target(m, sitting); err != nil {
				return "", err
			}
		} else if sit, err = s.nextOpen(m, s.today(), true, "", "planned"); err != nil {
			return "", err
		}
		if it, err = s.addItem(m, sit, in, accept); err != nil {
			return "", err
		}
		return "item add " + it.ID, s.saveMeeting(m)
	})
	return it, err
}

// addItem writes a new item of m, under a lock the caller holds; the caller saves m.
func (s *Store) addItem(m *Meeting, sit *Sitting, in ItemInput, accept bool) (*Item, error) {
	m.Counter++
	it := &Item{ID: fmt.Sprintf("%s-%d", m.Alias, m.Counter), Meeting: m.Alias, Title: strings.TrimSpace(in.Title),
		Owner: in.Owner, Kind: in.Kind, Duration: in.Duration, Expected: in.Expected,
		Attachments: in.Attach, Refs: in.Refs, State: "proposed", Notes: in.Notes}
	if it.Kind == "" {
		it.Kind = m.ItemKind
	}
	if it.Kind == "" {
		it.Kind = "discussion"
	}
	if it.Duration == "" {
		it.Duration = m.ItemDuration
	}
	if sit != nil {
		it.Sitting = sit.ID
	}
	if accept {
		it.State = "accepted"
	}
	s.log(&it.Log, it.State+" for "+orNone(it.Sitting))
	s.emit("item.added", m.Alias, map[string]any{"item": it})
	return it, s.saveItem(it)
}

// changeItem loads an item under the lock, applies fn and saves it.
func (s *Store) changeItem(id, msg string, fn func(it *Item, m *Meeting) error) (*Item, error) {
	var it *Item
	err := s.Write(msg, func() error {
		var err error
		if it, err = s.Item(id); err != nil {
			return err
		}
		m, err := s.Meeting(it.Meeting)
		if err != nil {
			return err
		}
		if err := fn(it, m); err != nil {
			return err
		}
		return s.saveItem(it)
	})
	return it, err
}

func itemConflict(it *Item, action string, states ...string) error {
	return spec.Conflict("cannot %s item %s: it is %s; %s needs %s", action, it.ID, it.State, action, strings.Join(states, " or "))
}

// AcceptItems puts proposed items on the agenda of their sitting.
func (s *Store) AcceptItems(ids []string) ([]*Item, error) {
	var out []*Item
	err := s.Write("item accept "+strings.ToUpper(strings.Join(ids, " ")), func() error {
		for _, id := range ids {
			it, err := s.Item(id)
			if err != nil {
				return err
			}
			switch it.State {
			case "accepted", "deferred":
				out = append(out, it)
				continue
			case "proposed":
			default:
				return itemConflict(it, "accept", "proposed")
			}
			m, err := s.Meeting(it.Meeting)
			if err != nil {
				return err
			}
			if it.Sitting == "" {
				sit, err := s.nextOpen(m, s.today(), true, "", "planned")
				if err != nil {
					return err
				}
				if sit != nil {
					it.Sitting = sit.ID
				}
			} else if _, err := s.target(m, it.Sitting); err != nil {
				return err
			}
			it.State = "accepted"
			s.log(&it.Log, "accepted for "+orNone(it.Sitting))
			if err := s.saveItem(it); err != nil {
				return err
			}
			s.emit("item.accepted", it.Meeting, map[string]any{"item": it})
			out = append(out, it)
		}
		return nil
	})
	return out, err
}

// DeferItem plans an item for a later sitting: to, or the next planned one.
func (s *Store) DeferItem(id, to string) (*Item, error) {
	return s.changeItem(id, "item defer "+strings.ToUpper(id), func(it *Item, m *Meeting) error {
		if it.State != "proposed" && it.State != "accepted" && it.State != "deferred" {
			return itemConflict(it, "defer", "proposed", "accepted", "deferred")
		}
		var sit *Sitting
		var err error
		if to != "" {
			if sit, err = s.target(m, to); err != nil {
				return err
			}
		} else {
			after, inclusive := s.today(), true
			if it.Sitting != "" {
				if cur, _, err := s.Sitting(it.Sitting); err == nil {
					after, inclusive = cur.Date, false
				}
			}
			if sit, err = s.nextOpen(m, after, inclusive, it.Sitting, "planned"); err != nil {
				return err
			}
		}
		from := it.Sitting
		it.Sitting = ""
		if sit != nil {
			it.Sitting = sit.ID
		}
		if it.State == "accepted" {
			it.State = "deferred"
		}
		s.log(&it.Log, "deferred from "+orNone(from)+" to "+orNone(it.Sitting))
		s.emit("item.deferred", it.Meeting, map[string]any{"item": it, "from": from, "to": it.Sitting})
		return nil
	})
}

// DropItem withdraws an item.
func (s *Store) DropItem(id, reason string) (*Item, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, spec.UserError("item drop needs --reason. Example: ordo item drop %s --reason \"settled by e-mail\"", strings.ToUpper(id))
	}
	return s.changeItem(id, "item drop "+strings.ToUpper(id), func(it *Item, _ *Meeting) error {
		if it.State == "done" || it.State == "dropped" {
			return itemConflict(it, "drop", "proposed", "accepted", "deferred")
		}
		it.State, it.Reason = "dropped", reason
		s.log(&it.Log, "dropped: "+reason)
		s.emit("item.dropped", it.Meeting, map[string]any{"item": it, "reason": reason})
		return nil
	})
}

// EditItem changes the fields given and appends attachments and refs.
func (s *Store) EditItem(id string, in ItemInput) (*Item, error) {
	if err := in.check(); err != nil {
		return nil, err
	}
	return s.changeItem(id, "item edit "+strings.ToUpper(id), func(it *Item, _ *Meeting) error {
		var changed []string
		set := func(name string, dst *string, v string) {
			if v != "" {
				*dst = v
				changed = append(changed, name)
			}
		}
		set("title", &it.Title, in.Title)
		set("owner", &it.Owner, in.Owner)
		set("kind", &it.Kind, in.Kind)
		set("duration", &it.Duration, in.Duration)
		set("expected", &it.Expected, in.Expected)
		set("notes", &it.Notes, in.Notes)
		for _, a := range in.Attach {
			if !contains(it.Attachments, a) {
				it.Attachments = append(it.Attachments, a)
			}
			changed = append(changed, "attachments")
		}
		for _, r := range in.Refs {
			if !contains(it.Refs, r) {
				it.Refs = append(it.Refs, r)
			}
			changed = append(changed, "refs")
		}
		if len(changed) == 0 {
			return spec.UserError("item edit changes nothing. Example: ordo item edit %s --duration 20m", it.ID)
		}
		s.log(&it.Log, "edited "+strings.Join(dedupe(changed), ", "))
		return nil
	})
}

func dedupe(l []string) []string {
	var out []string
	for _, x := range l {
		if !contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

// OrderItems sets the agenda order of a sitting: the ids given first, the others after.
func (s *Store) OrderItems(sittingID string, order []string) (*Agenda, error) {
	var norm []string
	for _, id := range order {
		alias, n, err := parseItemID(id)
		if err != nil {
			return nil, err
		}
		norm = append(norm, fmt.Sprintf("%s-%d", alias, n))
	}
	_, err := s.change(sittingID, "item order", func(sit *Sitting, _ *Meeting) error {
		if err := requireState(sit, "order", "planned"); err != nil {
			return err
		}
		on, _, err := s.agendaItems(sit)
		if err != nil {
			return err
		}
		current := ids(on)
		for _, id := range norm {
			if !contains(current, id) {
				return spec.UserError("item %s is not on the agenda of %s; its items: %s", id, sit.ID, strings.Join(current, ", "))
			}
		}
		next := append([]string{}, norm...)
		for _, id := range current {
			if !contains(next, id) {
				next = append(next, id)
			}
		}
		sit.Order = next
		s.log(&sit.Log, "ordered "+strings.Join(next, ", "))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Agenda(sittingID)
}

// OutcomeInput carries the fields of outcome set.
type OutcomeInput struct {
	Summary, Decision, Next, By string
	Actions                     []string
}

// ParseAction reads "what|who|due".
func ParseAction(v string) (ActionItem, error) {
	parts := strings.Split(v, "|")
	if len(parts) > 3 || strings.TrimSpace(parts[0]) == "" {
		return ActionItem{}, spec.UserError("action %q: expected \"what|who|due\", e.g. \"Draft the budget|Marie|2026-10-20\"", v)
	}
	a := ActionItem{What: strings.TrimSpace(parts[0])}
	if len(parts) > 1 {
		a.Who = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 && strings.TrimSpace(parts[2]) != "" {
		d, err := normDate(parts[2])
		if err != nil {
			return ActionItem{}, err
		}
		a.Due = d
	}
	return a, nil
}

// SetOutcome records what came out of an item in a sitting (its own by default).
// An outcome by agent:<name> is a draft until the minutes are approved.
func (s *Store) SetOutcome(id, sitting string, in OutcomeInput) (*Item, error) {
	var actions []ActionItem
	for _, v := range in.Actions {
		a, err := ParseAction(v)
		if err != nil {
			return nil, err
		}
		actions = append(actions, a)
	}
	if in.Next == "" {
		in.Next = "done"
	}
	if in.By == "" {
		in.By = s.By
	}
	if in.Summary == "" && in.Decision == "" && len(actions) == 0 {
		return nil, spec.UserError("outcome set needs --summary, --decision or --action. Example: ordo outcome set %s --summary \"Presented; questions answered\"", strings.ToUpper(id))
	}
	return s.changeItem(id, "outcome set "+strings.ToUpper(id), func(it *Item, _ *Meeting) error {
		sid := it.Sitting
		if sitting != "" {
			sid = strings.ToUpper(sitting)
		}
		if sid == "" || sid != it.Sitting {
			return spec.Conflict("item %s is planned for %s, not %s; an outcome is recorded in the item's own sitting", it.ID, orNone(it.Sitting), orNone(sid))
		}
		if it.State != "accepted" && it.State != "deferred" {
			return itemConflict(it, "record an outcome for", "accepted", "deferred")
		}
		sit, _, err := s.Sitting(sid)
		if err != nil {
			return err
		}
		if err := requireState(sit, "record outcomes in", "planned", "frozen", "held"); err != nil {
			return err
		}
		status := "approved"
		if strings.HasPrefix(in.By, "agent:") {
			status = "draft"
		}
		o := &Outcome{Summary: in.Summary, Decision: in.Decision, Actions: actions, Next: in.Next,
			By: in.By, Status: status, At: s.Now().Format(time.RFC3339)}
		done := map[string]bool{}
		known := map[string]bool{}
		if e := it.entry(sid); e != nil {
			if e.Outcome != nil {
				for _, a := range e.Outcome.Actions {
					done[a.What] = a.Done
					known[a.What] = true
				}
				for i := range o.Actions {
					o.Actions[i].Done = done[o.Actions[i].What]
				}
			}
			e.Outcome = o
		} else {
			it.History = append(it.History, Entry{Sitting: sid, Outcome: o})
		}
		s.log(&it.Log, "outcome "+status+" for "+sid)
		s.emit("outcome.set", it.Meeting, map[string]any{"item": it, "sitting": sid, "outcome": o})
		for _, a := range o.Actions {
			if !known[a.What] {
				s.emit("action.added", it.Meeting, map[string]any{"item": it, "sitting": sid, "action": a})
			}
		}
		return nil
	})
}
