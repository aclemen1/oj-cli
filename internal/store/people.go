package store

import (
	"strings"
	"sync"
	"time"
)

// Person is one name the sphere knows: the value stored (contact:JMR) and its label.
type Person struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type peopleCache struct {
	mu   sync.Mutex
	list []Person
	at   time.Time
}

// People runs the sphere's people.list command, at most once a minute.
func (s *Store) People() []Person {
	if len(s.PeopleList) == 0 {
		return nil
	}
	s.people.mu.Lock()
	defer s.people.mu.Unlock()
	if !s.people.at.IsZero() && time.Since(s.people.at) < time.Minute {
		return s.people.list
	}
	text, failure := s.runText(s.PeopleList)
	if failure != "" {
		s.warn("people.list: " + failure)
		return s.people.list
	}
	var out []Person
	for _, l := range strings.Split(text, "\n") {
		v, label, _ := strings.Cut(strings.TrimSpace(l), "\t")
		if v == "" {
			continue
		}
		if label == "" {
			label = v
		}
		out = append(out, Person{Value: v, Label: label})
	}
	s.people.list, s.people.at = out, time.Now()
	return out
}

// PersonLabel is the label of a stored value (contact:JMR → Jean-Moïse Rochat), or the value itself.
func (s *Store) PersonLabel(v string) string {
	if v == "" {
		return ""
	}
	for _, p := range s.People() {
		if strings.EqualFold(p.Value, v) {
			return p.Label
		}
	}
	return v
}

// namedOutcome is a copy of an outcome whose actions show names: an empty
// who stands for people.me when the sphere declares it.
func (s *Store) namedOutcome(o *Outcome) *Outcome {
	if o == nil || len(o.Actions) == 0 {
		return o
	}
	c := *o
	c.Actions = make([]ActionItem, len(o.Actions))
	for i, a := range o.Actions {
		if a.Who == "" {
			a.Who = s.PeopleMe
		}
		a.Who = s.PersonLabel(a.Who)
		c.Actions[i] = a
	}
	return &c
}

// labels maps PersonLabel over a list.
func (s *Store) labels(l []string) []string {
	if l == nil {
		return nil
	}
	out := make([]string, len(l))
	for i, v := range l {
		out[i] = s.PersonLabel(v)
	}
	return out
}
