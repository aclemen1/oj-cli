package store

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/aclemen1/oj-cli/internal/spec"
)

// Standing is a recurring item: every sitting of the meeting has its own
// instance, accepted, at the start or the end of the agenda.
type Standing struct {
	Key      string `yaml:"key" json:"key"`
	Title    string `yaml:"title" json:"title"`
	Kind     string `yaml:"kind,omitempty" json:"kind,omitempty"`
	Duration string `yaml:"duration,omitempty" json:"duration,omitempty"`
	Expected string `yaml:"expected,omitempty" json:"expected,omitempty"`
	Notes    string `yaml:"notes,omitempty" json:"notes,omitempty"`
	Place    string `yaml:"place" json:"place"` // start or end
}

const standingRef = "standing:"

var Places = []string{"start", "end"}

var keyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// StandingKey is the key of the recurring item an item is an instance of, or "".
func (it *Item) StandingKey() string {
	if it.Standing != "" {
		return it.Standing
	}
	for _, r := range it.Refs {
		if k, ok := strings.CutPrefix(r, standingRef); ok {
			return k
		}
	}
	return ""
}

func slug(title string) string {
	r := strings.NewReplacer("à", "a", "â", "a", "ä", "a", "ç", "c", "é", "e", "è", "e", "ê", "e", "ë", "e",
		"î", "i", "ï", "i", "ô", "o", "ö", "o", "ù", "u", "û", "u", "ü", "u")
	s := r.Replace(strings.ToLower(title))
	var b strings.Builder
	dash := false
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= 24 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

// AddStanding declares a recurring item of a meeting.
func (s *Store) AddStanding(alias string, st Standing) (*Meeting, error) {
	st.Title = strings.TrimSpace(st.Title)
	if st.Title == "" {
		return nil, spec.UserError("a recurring item needs a title. Example: oj standing add RDIR \"Date de la prochaine séance\" --place end")
	}
	if st.Place == "" {
		st.Place = "end"
	}
	if !contains(Places, st.Place) {
		return nil, spec.UserError("--place takes start or end, got %q", st.Place)
	}
	if st.Kind != "" && !contains(Kinds, st.Kind) {
		return nil, spec.UserError("kind %q: expected info, discussion or decision", st.Kind)
	}
	var err error
	if st.Duration, err = NormDuration(st.Duration); err != nil {
		return nil, err
	}
	if st.Key == "" {
		st.Key = slug(st.Title)
	}
	if !keyRe.MatchString(st.Key) {
		return nil, spec.UserError("key %q: lower-case letters, digits and -, e.g. repas", st.Key)
	}
	var m *Meeting
	a, err := NormAlias(alias)
	if err != nil {
		return nil, err
	}
	err = s.Write("standing add "+a+" "+st.Key, func() error {
		if m, err = s.Meeting(a); err != nil {
			return err
		}
		for _, x := range m.Standing {
			if x.Key == st.Key {
				return spec.Conflict("meeting %s already has a recurring item %q (%s); pass another --key", a, st.Key, x.Title)
			}
		}
		m.Standing = append(m.Standing, st)
		s.log(&m.Log, "recurring item "+st.Key+" added")
		return s.saveMeeting(m)
	})
	return m, err
}

// RemoveStanding stops a recurring item; instances already written stay.
func (s *Store) RemoveStanding(alias, key string) (*Meeting, error) {
	var m *Meeting
	a, err := NormAlias(alias)
	if err != nil {
		return nil, err
	}
	err = s.Write("standing rm "+a+" "+key, func() error {
		if m, err = s.Meeting(a); err != nil {
			return err
		}
		var kept []Standing
		found := false
		for _, x := range m.Standing {
			if x.Key == key {
				found = true
				continue
			}
			kept = append(kept, x)
		}
		if !found {
			return spec.NotFound("meeting %s has no recurring item %q; see `oj standing ls %s`", a, key, a)
		}
		m.Standing = kept
		s.log(&m.Log, "recurring item "+key+" removed")
		return s.saveMeeting(m)
	})
	return m, err
}

// virtualStanding returns the recurring items of a planned sitting that have
// no instance yet, as virtual items, split by place.
func (s *Store) virtualStanding(sit *Sitting, m *Meeting, all []*Item) (start, end []*Item) {
	if sit.State != "planned" || len(m.Standing) == 0 {
		return nil, nil
	}
	have := map[string]bool{}
	for _, it := range all {
		if k := it.StandingKey(); k != "" && (it.Sitting == sit.ID || it.entry(sit.ID) != nil) {
			have[k] = true
		}
	}
	for _, st := range m.Standing {
		if have[st.Key] {
			continue
		}
		it := &Item{Sphere: s.Sphere, Meeting: m.Alias, Title: st.Title, Kind: st.Kind, Duration: st.Duration,
			Expected: st.Expected, Notes: st.Notes, Refs: []string{standingRef + st.Key}, Sitting: sit.ID,
			State: "accepted", Virtual: true, Standing: st.Key}
		if it.Kind == "" {
			it.Kind = m.ItemKind
		}
		if it.Kind == "" {
			it.Kind = "discussion"
		}
		if it.Duration == "" {
			it.Duration = m.ItemDuration
		}
		if st.Place == "start" {
			start = append(start, it)
		} else {
			end = append(end, it)
		}
	}
	return start, end
}

// withStanding is the agenda of a sitting with its virtual recurring items in place.
func (s *Store) withStanding(sit *Sitting, m *Meeting, on []*Item) ([]*Item, error) {
	all, err := s.items(m.Alias)
	if err != nil {
		return nil, err
	}
	start, end := s.virtualStanding(sit, m, all)
	if start == nil && end == nil {
		return on, nil
	}
	out := append(append(append([]*Item{}, start...), on...), end...)
	return out, nil
}

// materializeStanding writes the instances of the recurring items of a
// sitting (all, or those of keys) and places them in its order, under a lock
// the caller holds; the caller saves the sitting and the meeting.
func (s *Store) materializeStanding(sit *Sitting, m *Meeting, keys ...string) ([]*Item, error) {
	all, err := s.items(m.Alias)
	if err != nil {
		return nil, err
	}
	start, end := s.virtualStanding(sit, m, all)
	if start == nil && end == nil {
		return nil, nil
	}
	on, _, err := s.agendaItems(sit)
	if err != nil {
		return nil, err
	}
	order := ids(on)
	var made []*Item
	var head, tail []string
	for _, group := range [][]*Item{start, end} {
		for _, v := range group {
			if len(keys) > 0 && !contains(keys, v.Standing) {
				continue
			}
			it, err := s.addItem(m, sit, ItemInput{Title: v.Title, Kind: v.Kind, Duration: v.Duration, Expected: v.Expected,
				Notes: v.Notes, Refs: v.Refs}, true)
			if err != nil {
				return nil, err
			}
			made = append(made, it)
			if containsItem(start, v) {
				head = append(head, it.ID)
			} else {
				tail = append(tail, it.ID)
			}
		}
	}
	sit.Order = append(append(head, order...), tail...)
	return made, nil
}

func containsItem(l []*Item, x *Item) bool {
	for _, it := range l {
		if it == x {
			return true
		}
	}
	return false
}

// ApplyStanding writes the instances of a planned sitting's recurring items
// (all, or the one of key) and returns them; an instance already written is returned as is.
func (s *Store) ApplyStanding(sittingID, key string) ([]*Item, error) {
	var made []*Item
	err := s.writeAs(func() (string, error) {
		sit, m, err := s.Resolve(sittingID)
		if err != nil {
			return "", err
		}
		if key != "" {
			found := false
			for _, st := range m.Standing {
				found = found || st.Key == key
			}
			if !found {
				return "", spec.NotFound("meeting %s has no recurring item %q; see `oj standing ls %s`", m.Alias, key, m.Alias)
			}
		}
		if sit.State != "planned" {
			return "", requireState(sit, "apply recurring items to", "planned")
		}
		var keys []string
		if key != "" {
			keys = []string{key}
		}
		if made, err = s.materializeStanding(sit, m, keys...); err != nil {
			return "", err
		}
		if len(made) == 0 {
			all, err := s.items(m.Alias)
			if err != nil {
				return "", err
			}
			for _, it := range all {
				if k := it.StandingKey(); k != "" && it.Sitting == sit.ID && (key == "" || k == key) {
					made = append(made, it)
				}
			}
			return "", nil
		}
		if err := s.saveMeeting(m); err != nil {
			return "", err
		}
		return fmt.Sprintf("standing apply %s (%d items)", sit.ID, len(made)), s.saveSitting(sit)
	})
	return made, err
}
