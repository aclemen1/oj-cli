package store

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/teambition/rrule-go"

	"github.com/aclemen1/oj-cli/internal/calendar"
	"github.com/aclemen1/oj-cli/internal/spec"
)

// CalLink says which events of which calendar are the sittings of a meeting.
type CalLink struct {
	Source      string `yaml:"source" json:"source"`
	Match       string `yaml:"match,omitempty" json:"match,omitempty"`
	RecurringID string `yaml:"recurring_id,omitempty" json:"recurring_id,omitempty"`
}

type SyncChange struct {
	Sitting string `json:"sitting"`
	What    string `json:"what"`
}

type Synced struct {
	Meeting     string       `json:"meeting"`
	Events      int          `json:"events"`
	Changes     []SyncChange `json:"changes"`
	Unconfirmed []string     `json:"unconfirmed"`
}

// SetCalendar links a meeting to a calendar source.
func (s *Store) SetCalendar(alias string, link CalLink) (*Meeting, error) {
	if link.Match == "" && link.RecurringID == "" {
		return nil, spec.UserError("a calendar link needs --match (a pattern on the event title) or --recurring-id. Example: oj meeting calendar %s --source work --match \"^Séance de direction\"", strings.ToUpper(alias))
	}
	if link.Match != "" {
		if _, err := regexp.Compile(link.Match); err != nil {
			return nil, spec.UserError("--match %q: %v", link.Match, err)
		}
	}
	var m *Meeting
	a, err := NormAlias(alias)
	if err != nil {
		return nil, err
	}
	err = s.Write("meeting calendar "+a, func() error {
		if m, err = s.Meeting(a); err != nil {
			return err
		}
		m.Calendar = &link
		s.log(&m.Log, "calendar "+link.Source)
		return s.saveMeeting(m)
	})
	return m, err
}

func (l *CalLink) matches(e calendar.Event) bool {
	if l.RecurringID != "" && (e.RecurringID == l.RecurringID || e.UID == l.RecurringID) {
		return true
	}
	if l.Match != "" {
		ok, _ := regexp.MatchString(l.Match, e.Title)
		return ok
	}
	return false
}

func (m *Meeting) location() *time.Location {
	if set, err := rrule.StrToRRuleSet(m.RRule); err == nil && m.RRule != "" {
		return set.GetDTStart().Location()
	}
	return time.Local
}

// Sync reconciles the sittings of a meeting with calendar events between
// from and to: the calendar says when a sitting really takes place.
func (s *Store) Sync(alias string, events []calendar.Event, from, to time.Time) (*Synced, error) {
	m, err := s.Meeting(alias)
	if err != nil {
		return nil, err
	}
	if m.Calendar == nil {
		return nil, spec.UserError("meeting %s has no calendar. Link one: oj meeting calendar %s --source <name> --match <pattern>", m.Alias, m.Alias)
	}
	out := &Synced{Meeting: m.Alias, Changes: []SyncChange{}, Unconfirmed: []string{}}
	var mine []calendar.Event
	for _, e := range events {
		if m.Calendar.matches(e) {
			mine = append(mine, e)
		}
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].Start.Before(mine[j].Start) })
	out.Events = len(mine)
	loc := m.location()
	fromDay, toDay := from.In(loc).Format("2006-01-02"), to.In(loc).Format("2006-01-02")
	err = s.writeAs(func() (string, error) {
		files, err := s.sittingFiles(m.Alias)
		if err != nil {
			return "", err
		}
		byEvent, byID := map[string]*Sitting{}, map[string]*Sitting{}
		for _, f := range files {
			byID[f.ID] = f
			if f.Event != "" {
				byEvent[f.Event] = f
			}
		}
		recurring := map[string]occurrence{}
		for _, o := range m.occurrences(fromDay, 1000) {
			if o.Date >= toDay {
				break
			}
			recurring[o.Date] = o
		}
		lookup := func(day string) *Sitting {
			if f := byID[m.Alias+"-"+day]; f != nil {
				return f
			}
			if o, ok := recurring[day]; ok {
				return virtualSitting(m, o)
			}
			if o, ok := m.occursOn(day); ok {
				return virtualSitting(m, o)
			}
			return nil
		}
		seen := map[string]bool{}
		note := func(sit *Sitting, what string) {
			out.Changes = append(out.Changes, SyncChange{sit.ID, what})
			s.log(&sit.Log, "calendar: "+what)
		}
		for _, e := range mine {
			sit := byEvent[e.UID]
			if sit == nil && !e.Original.IsZero() {
				sit = lookup(e.Original.In(loc).Format("2006-01-02"))
			}
			if sit == nil {
				sit = lookup(e.Start.In(loc).Format("2006-01-02"))
			}
			if sit == nil {
				if e.Cancelled() {
					continue
				}
				if sit, err = s.oneOffFromEvent(m, e, loc); err != nil {
					return "", err
				}
				byID[sit.ID] = sit
				out.Changes = append(out.Changes, SyncChange{sit.ID, "added from the calendar"})
				seen[sit.ID] = true
				continue
			}
			seen[sit.ID] = true
			if sit.State == "held" || sit.State == "minuted" {
				continue
			}
			if e.Cancelled() {
				if sit.State == "planned" || sit.State == "frozen" {
					sit.State, sit.Reason = "cancelled", "calendar"
					note(sit, "cancelled")
					moved, err := s.moveOn(m, sit, "proposed", "accepted", "deferred")
					if err != nil {
						return "", err
					}
					if len(moved) > 0 {
						out.Changes[len(out.Changes)-1].What += ", items moved: " + strings.Join(moved, ", ")
					}
					s.emit("sitting.cancelled", m.Alias, map[string]any{"sitting": sit, "moved": moved, "source": "calendar"})
					if err := s.saveSitting(sit); err != nil {
						return "", err
					}
				}
				continue
			}
			var what []string
			if sit.State == "cancelled" && sit.Reason == "calendar" {
				sit.State, sit.Reason = "planned", ""
				what = append(what, "back in the calendar")
			}
			if sit.State == "cancelled" {
				continue
			}
			day, at := e.Start.In(loc).Format("2006-01-02"), e.Start.In(loc).Format("15:04")
			if sit.Date != day {
				what = append(what, "moved to "+day)
				sit.Date = day
			}
			if sit.Time != at {
				what = append(what, "at "+at)
				sit.Time = at
			}
			if e.Location != "" && sit.Place != e.Location {
				what = append(what, "in "+e.Location)
				sit.Place = e.Location
			}
			if len(what) == 0 {
				continue
			}
			sit.Event = e.UID
			note(sit, strings.Join(what, ", "))
			s.emit("sitting.moved", m.Alias, map[string]any{"sitting": sit, "source": "calendar"})
			if err := s.saveSitting(sit); err != nil {
				return "", err
			}
		}
		today := s.today()
		for day := range recurring {
			id := m.Alias + "-" + day
			if day < today || seen[id] {
				continue
			}
			if f := byID[id]; f != nil && f.State == "cancelled" {
				continue
			}
			out.Unconfirmed = append(out.Unconfirmed, id)
		}
		sort.Strings(out.Unconfirmed)
		if len(out.Changes) == 0 {
			return "", nil
		}
		return fmt.Sprintf("sync %s (%d changes)", m.Alias, len(out.Changes)), nil
	})
	return out, err
}

// oneOffFromEvent writes a sitting for an event on a day the recurrence does not know.
func (s *Store) oneOffFromEvent(m *Meeting, e calendar.Event, loc *time.Location) (*Sitting, error) {
	day := e.Start.In(loc).Format("2006-01-02")
	key := day
	for n := 2; ; n++ {
		_, statErr := os.Stat(s.sittingPath(m.Alias, key))
		if os.IsNotExist(statErr) {
			break
		}
		key = fmt.Sprintf("%s-%d", day, n)
	}
	place := e.Location
	if place == "" {
		place = m.Place
	}
	sit := &Sitting{Sphere: s.Sphere, ID: m.Alias + "-" + key, Meeting: m.Alias, Date: day, Time: e.Start.In(loc).Format("15:04"),
		Place: place, Duration: m.Duration, State: "planned", Event: e.UID, Virtual: true}
	if !e.End.IsZero() && e.End.After(e.Start) {
		sit.Duration = FormatDuration(e.End.Sub(e.Start).Truncate(time.Minute))
	}
	return sit, s.saveSitting(sit)
}
