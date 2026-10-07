package store

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/teambition/rrule-go"

	"github.com/aclemen1/oj-cli/internal/spec"
)

type Meeting struct {
	Sphere       string   `yaml:"-" json:"sphere,omitempty"`
	Alias        string   `yaml:"alias" json:"alias"`
	Title        string   `yaml:"title" json:"title"`
	RRule        string   `yaml:"rrule,omitempty" json:"rrule,omitempty"`
	Duration     string   `yaml:"duration,omitempty" json:"duration,omitempty"`
	Place        string   `yaml:"place,omitempty" json:"place,omitempty"`
	Chair        string   `yaml:"chair,omitempty" json:"chair,omitempty"`
	Members      []string `yaml:"members,omitempty" json:"members,omitempty"`
	Refs         []string `yaml:"refs,omitempty" json:"refs,omitempty"`
	ItemDuration string   `yaml:"item_duration,omitempty" json:"item_duration,omitempty"`
	ItemKind     string   `yaml:"item_kind,omitempty" json:"item_kind,omitempty"`
	Lang         string   `yaml:"lang,omitempty" json:"lang,omitempty"`
	Calendar     *CalLink `yaml:"calendar,omitempty" json:"calendar,omitempty"`
	// Standing: recurring items, on the agenda of every sitting.
	Standing []Standing `yaml:"standing,omitempty" json:"standing,omitempty"`
	Counter  int        `yaml:"counter" json:"-"`
	Log      []LogEntry `yaml:"log,omitempty" json:"log,omitempty"`
	Notes    string     `yaml:"-" json:"notes,omitempty"`
}

var Kinds = []string{"info", "discussion", "decision"}

func (s *Store) meetingDir(alias string) string { return filepath.Join(s.Root, alias) }

func (s *Store) meetingPath(alias string) string {
	return filepath.Join(s.meetingDir(alias), "meeting.md")
}

// Meeting loads a meeting by alias.
func (s *Store) Meeting(alias string) (*Meeting, error) {
	a, err := NormAlias(alias)
	if err != nil {
		return nil, err
	}
	m := &Meeting{}
	body, err := readDoc(s.meetingPath(a), m)
	if os.IsNotExist(err) {
		return nil, spec.NotFound("no meeting %q in sphere %s. List them with `oj meeting ls --sphere %s`", a, s.Sphere, s.Sphere)
	}
	if err != nil {
		return nil, err
	}
	m.Notes, m.Sphere = body, s.Sphere
	return m, nil
}

func (s *Store) saveMeeting(m *Meeting) error {
	return writeDoc(s.meetingPath(m.Alias), m, m.Notes)
}

// Meetings lists the meetings of the sphere, by alias.
func (s *Store) Meetings() ([]*Meeting, error) {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return nil, err
	}
	var out []*Meeting
	for _, e := range entries {
		if !e.IsDir() || !aliasRe.MatchString(e.Name()) {
			continue
		}
		if _, err := os.Stat(s.meetingPath(e.Name())); err != nil {
			continue
		}
		m, err := s.Meeting(e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	return out, nil
}

// MeetingInput carries the fields of meeting add and edit.
type MeetingInput struct {
	Title, RRule, Start, TZ, Duration, Place, Chair, ItemDuration, ItemKind, Lang string
	Members, Refs                                                                 []string
}

var Langs = []string{"en", "fr"}

// AddMeeting creates a meeting.
func (s *Store) AddMeeting(alias string, in MeetingInput) (*Meeting, error) {
	a, err := NormAlias(alias)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, spec.UserError("meeting add needs --title. Example: oj meeting add RDIR --title \"Séance de direction\" --sphere pro")
	}
	m := &Meeting{Sphere: s.Sphere, Alias: a, Title: strings.TrimSpace(in.Title), Place: in.Place, Chair: in.Chair, Members: in.Members, Refs: in.Refs}
	if err := applyMeeting(m, in); err != nil {
		return nil, err
	}
	err = s.Write("meeting add "+a, func() error {
		if _, err := os.Stat(s.meetingPath(a)); err == nil {
			return spec.Conflict("meeting %s already exists in sphere %s; change it with `oj meeting edit %s`", a, s.Sphere, a)
		}
		s.log(&m.Log, "created")
		return s.saveMeeting(m)
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

func applyMeeting(m *Meeting, in MeetingInput) error {
	var err error
	if in.RRule != "" || in.Start != "" {
		if m.RRule, err = BuildRRule(in.RRule, in.Start, in.TZ); err != nil {
			return err
		}
	}
	if in.Duration != "" {
		if m.Duration, err = NormDuration(in.Duration); err != nil {
			return err
		}
	}
	if in.ItemDuration != "" {
		if m.ItemDuration, err = NormDuration(in.ItemDuration); err != nil {
			return err
		}
	}
	if in.ItemKind != "" {
		if !contains(Kinds, in.ItemKind) {
			return spec.UserError("kind %q: expected info, discussion or decision", in.ItemKind)
		}
		m.ItemKind = in.ItemKind
	}
	if in.Lang != "" {
		if !contains(Langs, in.Lang) {
			return spec.UserError("lang %q: expected en or fr", in.Lang)
		}
		m.Lang = in.Lang
	}
	return nil
}

// EditMeeting changes the fields given; empty fields stay as they are.
func (s *Store) EditMeeting(alias string, in MeetingInput) (*Meeting, error) {
	var m *Meeting
	a, err := NormAlias(alias)
	if err != nil {
		return nil, err
	}
	err = s.Write("meeting edit "+a, func() error {
		var err error
		if m, err = s.Meeting(a); err != nil {
			return err
		}
		if in.Start == "" && in.RRule != "" && m.RRule != "" {
			in.Start, in.TZ = dtstartOf(m.RRule, in.TZ)
		}
		if err := applyMeeting(m, in); err != nil {
			return err
		}
		var changed []string
		set := func(name string, dst *string, v string) {
			if v != "" {
				*dst = v
				changed = append(changed, name)
			}
		}
		set("title", &m.Title, in.Title)
		set("place", &m.Place, in.Place)
		set("chair", &m.Chair, in.Chair)
		if in.Members != nil {
			m.Members = in.Members
			changed = append(changed, "members")
		}
		if in.Refs != nil {
			m.Refs = in.Refs
			changed = append(changed, "refs")
		}
		for name, v := range map[string]string{"rrule": in.RRule + in.Start, "duration": in.Duration, "item_duration": in.ItemDuration, "item_kind": in.ItemKind, "lang": in.Lang} {
			if v != "" {
				changed = append(changed, name)
			}
		}
		if len(changed) == 0 {
			return spec.UserError("meeting edit changes nothing. Example: oj meeting edit %s --duration 2h --sphere %s", a, s.Sphere)
		}
		sort.Strings(changed)
		s.log(&m.Log, "edited "+strings.Join(changed, ", "))
		return s.saveMeeting(m)
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

// ---------------------------------------------------------------- recurrence

// BuildRRule returns the stored form "DTSTART;TZID=…:…\nRRULE:…". A rule
// that already carries DTSTART is checked and kept.
func BuildRRule(rule, start, tz string) (string, error) {
	rule = strings.TrimSpace(rule)
	if strings.Contains(strings.ToUpper(rule), "DTSTART") {
		if _, err := rrule.StrToRRuleSet(rule); err != nil {
			return "", spec.UserError("rrule %q: %v", rule, err)
		}
		return rule, nil
	}
	if rule == "" || start == "" {
		return "", spec.UserError("a recurrence needs --rrule and --start. Example: --rrule FREQ=WEEKLY;BYDAY=TH --start 2026-10-08T09:00")
	}
	rule = strings.TrimPrefix(strings.ToUpper(rule), "RRULE:")
	if tz == "" {
		tz = LocalZone()
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "", spec.UserError("time zone %q: use an IANA name, e.g. Europe/Zurich", tz)
	}
	var t time.Time
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"} {
		if t, err = time.ParseInLocation(layout, strings.TrimSpace(start), loc); err == nil {
			break
		}
	}
	if err != nil {
		return "", spec.UserError("start %q: expected YYYY-MM-DDTHH:MM, e.g. 2026-10-08T09:00", start)
	}
	out := "DTSTART;TZID=" + tz + ":" + t.Format("20060102T150405") + "\nRRULE:" + rule
	if _, err := rrule.StrToRRuleSet(out); err != nil {
		return "", spec.UserError("rrule %q: %v. Example: FREQ=WEEKLY;BYDAY=TH", rule, err)
	}
	return out, nil
}

func dtstartOf(stored, tz string) (start, zone string) {
	set, err := rrule.StrToRRuleSet(stored)
	if err != nil {
		return "", tz
	}
	d := set.GetDTStart()
	if tz == "" {
		tz = d.Location().String()
	}
	return d.Format("2006-01-02T15:04"), tz
}

// LocalZone is the IANA name of this machine's time zone.
func LocalZone() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if _, after, ok := strings.Cut(target, "zoneinfo/"); ok {
			return after
		}
	}
	return "UTC"
}

// occurrence is one date of a meeting's recurrence.
type occurrence struct {
	Date, Time string
}

// occurrences returns up to n dates of the recurrence on or after from (YYYY-MM-DD).
func (m *Meeting) occurrences(from string, n int) []occurrence {
	if m.RRule == "" || n <= 0 {
		return nil
	}
	set, err := rrule.StrToRRuleSet(m.RRule)
	if err != nil {
		return nil
	}
	loc := set.GetDTStart().Location()
	t, err := time.ParseInLocation("2006-01-02", from, loc)
	if err != nil {
		return nil
	}
	var out []occurrence
	inc := true
	for len(out) < n {
		next := set.After(t, inc)
		if next.IsZero() {
			break
		}
		next = next.In(loc)
		out = append(out, occurrence{next.Format("2006-01-02"), next.Format("15:04")})
		t, inc = next, false
	}
	return out
}

// occursOn tells whether the recurrence has a date on day.
func (m *Meeting) occursOn(day string) (occurrence, bool) {
	l := m.occurrences(day, 1)
	if len(l) == 1 && l[0].Date == day {
		return l[0], true
	}
	return occurrence{}, false
}
