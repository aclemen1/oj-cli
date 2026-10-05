package store

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/aclemen1/oj-cli/internal/spec"
)

// UnholdSitting takes back a hold: frozen again, or planned when the agenda
// was never frozen.
func (s *Store) UnholdSitting(id string) (*Sitting, error) {
	return s.change(id, "sitting unhold", func(sit *Sitting, _ *Meeting) error {
		if err := requireState(sit, "unhold", "held"); err != nil {
			return err
		}
		sit.State = "planned"
		if _, v := s.latestAgenda(sit); v > 0 {
			sit.State = "frozen"
		}
		s.log(&sit.Log, "unheld, back to "+sit.State)
		s.emit("sitting.unheld", sit.Meeting, map[string]any{"sitting": sit})
		return nil
	})
}

type Unminuted struct {
	Sitting  *Sitting `json:"sitting"`
	Reopened []string `json:"reopened"`
	Drafts   int      `json:"drafts"`
	Removed  []string `json:"removed"`
}

// UnminuteSitting takes back approved minutes: the sitting is held again, its
// items are back on its agenda as before the minutes, outcomes written by an
// agent are drafts again, and the rendered minutes are removed (the VCS keeps
// them). It refuses when an item has changed since the minutes.
func (s *Store) UnminuteSitting(id string) (*Unminuted, error) {
	out := &Unminuted{Reopened: []string{}, Removed: []string{}}
	sit, err := s.change(id, "sitting unminute", func(sit *Sitting, m *Meeting) error {
		if err := requireState(sit, "unminute", "minuted"); err != nil {
			return err
		}
		all, err := s.items(m.Alias)
		if err != nil {
			return err
		}
		var touched, changed []*Item
		for _, it := range all {
			e := it.entry(sit.ID)
			if e == nil {
				continue
			}
			ok := true
			switch e.Result {
			case "done":
				ok = it.State == "done" && it.Sitting == sit.ID
			case "deferred":
				ok = it.State == "deferred" && it.DeferredFrom == sit.ID && it.History[len(it.History)-1].Sitting == sit.ID
			}
			if !ok {
				changed = append(changed, it)
				continue
			}
			touched = append(touched, it)
		}
		if len(changed) > 0 {
			return spec.Conflict("cannot unminute %s: items changed since the minutes: %s", sit.ID, strings.Join(ids(changed), ", "))
		}
		for _, it := range touched {
			e := it.entry(sit.ID)
			if e.Result != "" {
				it.State = "accepted"
				for _, h := range it.History {
					if h.Sitting != sit.ID && h.Result == "deferred" {
						it.State = "deferred"
					}
				}
				it.Sitting, it.DeferredFrom = sit.ID, ""
				e.Result = ""
				out.Reopened = append(out.Reopened, it.ID)
			}
			if e.Outcome != nil && strings.HasPrefix(e.Outcome.By, "agent:") && e.Outcome.Status == "approved" {
				e.Outcome.Status = "draft"
				out.Drafts++
			}
			s.log(&it.Log, "minutes of "+sit.ID+" taken back")
			if err := s.saveItem(it); err != nil {
				return err
			}
		}
		base := filepath.Join(s.renderedDir(sit.Meeting), sittingKey(sit)+"-minutes")
		for _, ext := range Formats {
			if err := os.Remove(base + "." + ext); err == nil {
				out.Removed = append(out.Removed, base+"."+ext)
			}
		}
		sit.State = "held"
		s.log(&sit.Log, "unminuted")
		s.emit("sitting.unminuted", sit.Meeting, map[string]any{"sitting": sit, "reopened": out.Reopened})
		return nil
	})
	out.Sitting = sit
	return out, err
}

// RestoreSitting takes back a cancellation. Items moved away by it stay where they went.
func (s *Store) RestoreSitting(id string) (*Sitting, error) {
	return s.change(id, "sitting restore", func(sit *Sitting, _ *Meeting) error {
		if err := requireState(sit, "restore", "cancelled"); err != nil {
			return err
		}
		sit.State, sit.Reason = "planned", ""
		s.log(&sit.Log, "restored")
		s.emit("sitting.restored", sit.Meeting, map[string]any{"sitting": sit})
		return nil
	})
}

// RestoreItem takes back a drop (proposed again, or accepted with accept) or
// a done (on the agenda again). The item goes to its sitting when that one
// is still planned, else to the next planned sitting.
func (s *Store) RestoreItem(id string, accept bool) (*Item, error) {
	return s.changeItem(id, "item restore", func(it *Item, m *Meeting) error {
		from := it.State
		switch from {
		case "dropped":
			it.State, it.Reason = "proposed", ""
			if accept {
				it.State = "accepted"
			}
		case "done":
			it.State = "accepted"
		default:
			return itemConflict(it, "restore", "dropped", "done")
		}
		keep := false
		if it.Sitting != "" {
			if cur, _, err := s.Sitting(it.Sitting); err == nil && cur.State == "planned" {
				keep = true
			}
		}
		if !keep {
			next, err := s.nextOpen(m, s.today(), true, "", "planned")
			if err != nil {
				return err
			}
			it.Sitting = ""
			if next != nil {
				it.Sitting = next.ID
			}
		}
		s.log(&it.Log, "restored from "+from+", "+it.State+" for "+orNone(it.Sitting))
		s.emit("item.restored", it.Meeting, map[string]any{"item": it, "from": from})
		return nil
	})
}

// UndeferItem takes back a deferral: the item goes back to the sitting it was
// deferred from when that one is still open, else it stays and is accepted.
func (s *Store) UndeferItem(id string) (*Item, error) {
	return s.changeItem(id, "item undefer", func(it *Item, _ *Meeting) error {
		if it.State != "deferred" {
			return itemConflict(it, "undefer", "deferred")
		}
		from := it.Sitting
		if it.DeferredFrom != "" {
			if back, _, err := s.Sitting(it.DeferredFrom); err == nil && contains([]string{"planned", "frozen", "held"}, back.State) {
				it.Sitting = back.ID
			}
		}
		it.State, it.DeferredFrom = "accepted", ""
		s.log(&it.Log, "undeferred from "+orNone(from)+" to "+orNone(it.Sitting))
		s.emit("item.undeferred", it.Meeting, map[string]any{"item": it, "from": from, "to": it.Sitting})
		return nil
	})
}
