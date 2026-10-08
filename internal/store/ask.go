package store

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aclemen1/oj-cli/internal/spec"
)

type Asked struct {
	Sitting string `json:"sitting"`
	Request string `json:"request"`
	Ref     string `json:"ref"`
	Text    string `json:"text"`
}

// AskNames lists the sphere's named requests, sorted.
func (s *Store) AskNames() []string {
	var out []string
	for k := range s.Asks {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// askRef is the meeting's first ref whose scheme can receive a request.
func (s *Store) askRef(m *Meeting) (string, string, string) {
	for _, r := range m.Refs {
		scheme, id, ok := strings.Cut(r, ":")
		if ok && len(s.Refs[scheme].Ask) > 0 {
			return r, scheme, id
		}
	}
	return "", "", ""
}

// CanAsk tells whether a sitting of this meeting can send the request.
func (s *Store) CanAsk(alias, request string) bool {
	if _, ok := s.Asks[request]; !ok {
		return false
	}
	m, err := s.Meeting(alias)
	if err != nil {
		return false
	}
	r, _, _ := s.askRef(m)
	return r != ""
}

// Ask sends a named request about a sitting to the target of its meeting's ref
// (refs.<scheme>.ask of the sphere), e.g. "write the outcomes from the transcript".
func (s *Store) Ask(arg, request string) (*Asked, error) {
	tmpl, ok := s.Asks[request]
	if !ok {
		names := s.AskNames()
		if len(names) == 0 {
			return nil, spec.UserError("no request %q: the sphere declares no asks in the configuration", request)
		}
		return nil, spec.UserError("no request %q; the sphere declares: %s", request, strings.Join(names, ", "))
	}
	sit, m, err := s.Resolve(arg)
	if err != nil {
		return nil, err
	}
	ref, scheme, id := s.askRef(m)
	if ref == "" {
		return nil, spec.UserError("meeting %s has no ref whose scheme has an ask command (refs.<scheme>.ask); add one, e.g. oj meeting edit %s --ref office:U-0006", m.Alias, m.Alias)
	}
	on, _, err := s.agendaItems(sit)
	if err != nil {
		return nil, err
	}
	if on, err = s.withStanding(sit, m, on); err != nil {
		return nil, err
	}
	var items []string
	for _, it := range on {
		id := it.ID
		if it.Virtual {
			id = "↻ " + it.Standing + " (recurring, not written yet: oj standing apply " + sit.ID + " --key " + it.Standing + ")"
		}
		items = append(items, fmt.Sprintf("- %s · %s", id, it.Title))
	}
	text := strings.NewReplacer("{sitting}", sit.ID, "{meeting}", m.Alias, "{meeting_title}", m.Title,
		"{date}", sit.Date, "{time}", sit.Time, "{sphere}", s.Sphere, "{items}", strings.Join(items, "\n")).Replace(tmpl)
	if _, err := s.run(s.Refs[scheme].Ask, map[string]string{"{id}": id, "{text}": text}); err != nil {
		return nil, fmt.Errorf("ask %s: %w", ref, err)
	}
	return &Asked{Sitting: sit.ID, Request: request, Ref: ref, Text: text}, nil
}
