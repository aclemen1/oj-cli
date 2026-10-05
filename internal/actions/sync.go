package actions

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aclemen1/ordo-cli/internal/config"
	"github.com/aclemen1/ordo-cli/internal/spec"
	"github.com/aclemen1/ordo-cli/internal/store"
)

func init() {
	spec.Register(&spec.Action{
		Category: "meeting", Name: "calendar",
		Summary: "Link a meeting to the events of a calendar source.",
		Discussion: "The source is a name under calendars: in the configuration (type ics with path or url, or type command with run). " +
			"Events whose title matches --match, or that belong to the recurring event --recurring-id, are the meeting's sittings.",
		Params: []spec.Param{
			{Name: "alias", Kind: spec.String, Positional: true, Required: true, Help: "Meeting alias."},
			sphereParam(),
			{Name: "source", Kind: spec.String, Required: true, Help: "Calendar source named in the configuration, e.g. work."},
			{Name: "match", Kind: spec.String, Help: "Regular expression on the event title, e.g. ^Séance de direction."},
			{Name: "recurring-id", Kind: spec.String, Help: "Id of the recurring event in the calendar."},
		},
		Effects:  []string{"Writes the calendar link in meeting.md."},
		Examples: []string{`ordo meeting calendar RDIR --source work --match "^Séance de direction" --sphere pro`},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			cfg, err := config.Load(ctx.Config)
			if err != nil {
				return nil, err
			}
			src := ctx.Str("source")
			if _, ok := cfg.Calendars[src]; !ok {
				return nil, spec.UserError("no calendar source %q under calendars: in %s; sources: %s", src, cfg.File(), strings.Join(sourceNames(cfg), ", "))
			}
			return st.SetCalendar(ctx.Str("alias"), store.CalLink{Source: src, Match: ctx.Str("match"), RecurringID: ctx.Str("recurring-id")})
		}),
	})
	spec.Register(&spec.Action{
		Category: "sitting", Name: "sync",
		Summary: "Reconcile the sittings with the calendar: moved, cancelled or added events win over the recurrence.",
		Discussion: "Reads the events of the next --days days from each meeting's calendar source. A held or minuted sitting never changes. " +
			"A date of the recurrence with no event is reported as unconfirmed and kept. A routine can run this every hour.",
		Params: []spec.Param{
			{Name: "meeting", Kind: spec.String, Positional: true, Help: "Meeting alias. Defaults to every meeting with a calendar."},
			sphereParam(),
			{Name: "days", Kind: spec.String, Default: "90", Help: "How many days ahead to read."},
		},
		Effects:  []string{"Moves, cancels or adds sittings to match the calendar; one commit per meeting that changed."},
		Examples: []string{"ordo sitting sync --sphere pro", "ordo sitting sync RDIR --days 30 --sphere pro"},
		Run: with(func(ctx *spec.Context, st *store.Store) (any, error) {
			days, err := strconv.Atoi(ctx.Str("days"))
			if err != nil || days < 1 {
				return nil, spec.UserError("--days takes a positive number, got %q", ctx.Str("days"))
			}
			cfg, err := config.Load(ctx.Config)
			if err != nil {
				return nil, err
			}
			var ms []*store.Meeting
			if a := ctx.Str("meeting"); a != "" {
				m, err := st.Meeting(a)
				if err != nil {
					return nil, err
				}
				ms = []*store.Meeting{m}
			} else {
				all, err := st.Meetings()
				if err != nil {
					return nil, err
				}
				for _, m := range all {
					if m.Calendar != nil {
						ms = append(ms, m)
					}
				}
			}
			from := st.Now()
			from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.Local)
			to := from.AddDate(0, 0, days)
			out := []*store.Synced{}
			for _, m := range ms {
				if m.Calendar == nil {
					return nil, spec.UserError("meeting %s has no calendar. Link one: ordo meeting calendar %s --source <name> --match <pattern>", m.Alias, m.Alias)
				}
				src, ok := cfg.Calendars[m.Calendar.Source]
				if !ok {
					return nil, spec.UserError("meeting %s names calendar source %q, absent from %s", m.Alias, m.Calendar.Source, cfg.File())
				}
				c, cancel := context.WithTimeout(context.Background(), time.Minute)
				events, err := src.Events(c, from, to)
				cancel()
				if err != nil {
					return nil, err
				}
				r, err := st.Sync(m.Alias, events, from, to)
				if err != nil {
					return nil, err
				}
				out = append(out, r)
			}
			return out, nil
		}),
		Text: func(w io.Writer, r any) {
			for _, s := range r.([]*store.Synced) {
				fmt.Fprintf(w, "%s: %d events, %d changes\n", s.Meeting, s.Events, len(s.Changes))
				for _, c := range s.Changes {
					fmt.Fprintf(w, "  %-20s %s\n", c.Sitting, c.What)
				}
				if len(s.Unconfirmed) > 0 {
					fmt.Fprintf(w, "  unconfirmed: %s\n", strings.Join(s.Unconfirmed, ", "))
				}
			}
		},
	})
}

func sourceNames(cfg *config.Config) []string {
	var out []string
	for n := range cfg.Calendars {
		out = append(out, n)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return []string{"(none)"}
	}
	return out
}
