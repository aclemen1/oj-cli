package store

import (
	"sort"
	"strconv"
	"strings"

	"github.com/aclemen1/oj-cli/internal/spec"
)

// ActionRow is one action decided in a sitting, with where it comes from.
type ActionRow struct {
	Sphere  string `json:"sphere,omitempty"`
	Item    string `json:"item"`
	Title   string `json:"title"`
	Sitting string `json:"sitting"`
	N       int    `json:"n"`
	Status  string `json:"status"`
	ActionItem
}

// ActionFilter selects actions: State is open, done or all.
type ActionFilter struct {
	Meeting, Who, State, DueBefore string
}

// Actions lists the actions of the outcomes, open first by due date.
func (s *Store) Actions(f ActionFilter) ([]ActionRow, error) {
	items, err := s.Items(ItemFilter{Meeting: f.Meeting, State: "all"})
	if err != nil {
		return nil, err
	}
	out := []ActionRow{}
	for _, it := range items {
		for _, e := range it.History {
			if e.Outcome == nil {
				continue
			}
			for i, a := range e.Outcome.Actions {
				switch {
				case (f.State == "" || f.State == "open") && a.Done,
					f.State == "done" && !a.Done,
					f.Who != "" && !strings.EqualFold(a.Who, f.Who),
					f.DueBefore != "" && (a.Due == "" || a.Due >= f.DueBefore):
					continue
				}
				out = append(out, ActionRow{Sphere: s.Sphere, Item: it.ID, Title: it.Title, Sitting: e.Sitting, N: i + 1, Status: e.Outcome.Status, ActionItem: a})
			}
		}
	}
	SortActions(out)
	return out, nil
}

// SortActions puts open actions first, then by due date, undated last.
func SortActions(out []ActionRow) {
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Done != b.Done {
			return !a.Done
		}
		if (a.Due == "") != (b.Due == "") {
			return a.Due != ""
		}
		return a.Due < b.Due
	})
}

// SetActionDone marks action n (from 1) of an item's outcome done or open.
// sitting defaults to the latest outcome that has actions. An action already
// in that state is left alone: no commit, no event.
func (s *Store) SetActionDone(id, sitting string, n int, done bool) (*Item, error) {
	cur, err := s.Item(id)
	if err != nil {
		return nil, err
	}
	if e, err := actionEntry(cur, sitting, n); err != nil {
		return nil, err
	} else if e.Outcome.Actions[n-1].Done == done {
		return cur, nil
	}
	verb, event := "done", "action.done"
	if !done {
		verb, event = "open", "action.reopened"
	}
	return s.changeItem(id, "actions "+verb+" "+strings.ToUpper(id), func(it *Item, _ *Meeting) error {
		e, err := actionEntry(it, sitting, n)
		if err != nil {
			return err
		}
		e.Outcome.Actions[n-1].Done = done
		s.log(&it.Log, "action "+e.Sitting+"#"+strconv.Itoa(n)+" "+verb)
		s.emit(event, it.Meeting, map[string]any{"item": it, "sitting": e.Sitting, "n": n, "action": e.Outcome.Actions[n-1]})
		return nil
	})
}

// actionEntry finds the history entry holding action n of an item.
func actionEntry(it *Item, sitting string, n int) (*Entry, error) {
	var e *Entry
	if sitting != "" {
		e = it.entry(strings.ToUpper(sitting))
	} else {
		for i := len(it.History) - 1; i >= 0; i-- {
			if o := it.History[i].Outcome; o != nil && len(o.Actions) > 0 {
				e = &it.History[i]
				break
			}
		}
	}
	if e == nil || e.Outcome == nil || n < 1 || n > len(e.Outcome.Actions) {
		return nil, spec.NotFound("item %s has no action %d%s. List them with `oj actions ls %s`", it.ID, n, inSitting(sitting), it.Meeting)
	}
	return e, nil
}

func inSitting(s string) string {
	if s == "" {
		return ""
	}
	return " in " + strings.ToUpper(s)
}

// ItemPath is the file of an item, for an editor.
func (s *Store) ItemPath(id string) (string, error) {
	it, err := s.Item(id)
	if err != nil {
		return "", err
	}
	return s.itemPath(it.Meeting, it.ID), nil
}

// CommitHandEdit commits a file changed by hand, under the lock.
func (s *Store) CommitHandEdit(what string) error {
	return s.Write(what+" (by hand)", func() error { return nil })
}
