package calendar

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

type prop struct {
	params map[string]string
	value  string
}

type vevent map[string][]prop

func (v vevent) get(name string) (prop, bool) {
	if l := v[name]; len(l) > 0 {
		return l[0], true
	}
	return prop{}, false
}

func (v vevent) text(name string) string {
	p, _ := v.get(name)
	r := strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`)
	return r.Replace(p.value)
}

// unfold joins the continuation lines of an ICS file.
func unfold(b []byte) []string {
	raw := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	var out []string
	for _, l := range raw {
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(out) > 0 {
			out[len(out)-1] += l[1:]
			continue
		}
		out = append(out, l)
	}
	return out
}

func parseLine(l string) (string, prop) {
	head, value, _ := strings.Cut(l, ":")
	parts := strings.Split(head, ";")
	p := prop{params: map[string]string{}, value: value}
	for _, kv := range parts[1:] {
		k, v, _ := strings.Cut(kv, "=")
		p.params[strings.ToUpper(k)] = strings.Trim(v, `"`)
	}
	return strings.ToUpper(parts[0]), p
}

func icsTime(p prop) (time.Time, error) {
	v := p.value
	if p.params["VALUE"] == "DATE" || len(v) == 8 {
		return time.ParseInLocation("20060102", v, time.Local)
	}
	if strings.HasSuffix(v, "Z") {
		return time.Parse("20060102T150405Z", v)
	}
	loc := time.Local
	if tz := p.params["TZID"]; tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	return time.ParseInLocation("20060102T150405", v, loc)
}

// ParseICS returns the events of an ICS file between from and to, with
// recurring events expanded, EXDATE removed and RECURRENCE-ID overrides applied.
func ParseICS(b []byte, from, to time.Time) ([]Event, error) {
	var events []vevent
	var cur vevent
	for _, l := range unfold(b) {
		switch strings.TrimSpace(l) {
		case "BEGIN:VEVENT":
			cur = vevent{}
			continue
		case "END:VEVENT":
			if cur != nil {
				events = append(events, cur)
			}
			cur = nil
			continue
		}
		if cur != nil && l != "" {
			name, p := parseLine(l)
			cur[name] = append(cur[name], p)
		}
	}
	type key struct {
		uid string
		at  int64
	}
	overrides := map[key]vevent{}
	var masters []vevent
	for _, ev := range events {
		if rid, ok := ev.get("RECURRENCE-ID"); ok {
			t, err := icsTime(rid)
			if err != nil {
				return nil, fmt.Errorf("RECURRENCE-ID %q: %v", rid.value, err)
			}
			overrides[key{ev.text("UID"), t.Unix()}] = ev
			continue
		}
		masters = append(masters, ev)
	}
	var out []Event
	add := func(ev vevent, start time.Time, original time.Time, recurring bool) error {
		e, err := toEvent(ev, start)
		if err != nil {
			return err
		}
		if recurring {
			e.RecurringID = ev.text("UID")
			e.UID = fmt.Sprintf("%s/%s", e.RecurringID, original.UTC().Format("20060102T150405Z"))
			e.Original = original
		}
		if !e.Start.Before(from) && e.Start.Before(to) {
			out = append(out, e)
		}
		return nil
	}
	for _, ev := range masters {
		dt, ok := ev.get("DTSTART")
		if !ok {
			continue
		}
		start, err := icsTime(dt)
		if err != nil {
			return nil, fmt.Errorf("DTSTART %q: %v", dt.value, err)
		}
		if _, ok := ev.get("RRULE"); !ok {
			if err := add(ev, start, time.Time{}, false); err != nil {
				return nil, err
			}
			continue
		}
		set, err := ruleSet(ev, start)
		if err != nil {
			return nil, err
		}
		// Look a little before the window: an occurrence moved into it starts outside.
		for _, t := range set.Between(from.AddDate(0, -1, 0), to.AddDate(0, 1, 0), true) {
			if o, ok := overrides[key{ev.text("UID"), t.Unix()}]; ok {
				odt, _ := o.get("DTSTART")
				ostart, err := icsTime(odt)
				if err != nil {
					return nil, err
				}
				if err := add(o, ostart, t, true); err != nil {
					return nil, err
				}
				continue
			}
			if err := add(ev, t, t, true); err != nil {
				return nil, err
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

func ruleSet(ev vevent, start time.Time) (*rrule.Set, error) {
	lines := []string{"DTSTART;TZID=" + start.Location().String() + ":" + start.Format("20060102T150405")}
	for _, p := range ev["RRULE"] {
		lines = append(lines, "RRULE:"+p.value)
	}
	set, err := rrule.StrToRRuleSet(strings.Join(lines, "\n"))
	if err != nil {
		return nil, fmt.Errorf("RRULE of %s: %v", ev.text("UID"), err)
	}
	for _, p := range ev["EXDATE"] {
		for _, v := range strings.Split(p.value, ",") {
			t, err := icsTime(prop{params: p.params, value: v})
			if err == nil {
				set.ExDate(t)
			}
		}
	}
	return set, nil
}

func toEvent(ev vevent, start time.Time) (Event, error) {
	e := Event{UID: ev.text("UID"), Start: start, Title: ev.text("SUMMARY"), Location: ev.text("LOCATION"),
		Status: strings.ToLower(ev.text("STATUS"))}
	dt, _ := ev.get("DTSTART")
	s0, _ := icsTime(dt)
	if de, ok := ev.get("DTEND"); ok {
		if end, err := icsTime(de); err == nil {
			e.End = start.Add(end.Sub(s0))
		}
	}
	return e, nil
}
