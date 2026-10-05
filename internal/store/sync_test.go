package store

import (
	"strings"
	"testing"
	"time"

	"github.com/aclemen1/oj-cli/internal/calendar"
)

func zurich(day, at string) time.Time {
	loc, _ := time.LoadLocation("Europe/Zurich")
	t, _ := time.ParseInLocation("2006-01-02 15:04", day+" "+at, loc)
	return t
}

func ev(uid, day, at string, mod func(*calendar.Event)) calendar.Event {
	e := calendar.Event{UID: uid, Title: "Séance de direction", Start: zurich(day, at), End: zurich(day, at).Add(90 * time.Minute), RecurringID: "rdir"}
	if mod != nil {
		mod(&e)
	}
	return e
}

func TestSync(t *testing.T) {
	s := withRDIR(t)
	if _, err := s.Sync("RDIR", nil, today, today.AddDate(0, 0, 40)); kind(err) != "user_error" {
		t.Fatalf("sync without calendar: %v", err)
	}
	if _, err := s.SetCalendar("RDIR", CalLink{Source: "work"}); kind(err) != "user_error" {
		t.Fatal("a link needs a match")
	}
	must[*Meeting](t)(s.SetCalendar("RDIR", CalLink{Source: "work", RecurringID: "rdir", Match: "^Séance de direction"}))
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Sur le 15"}, true, "RDIR-2026-10-15"))

	events := []calendar.Event{
		ev("e8", "2026-10-08", "09:00", nil), // as planned
		ev("e15", "2026-10-15", "09:00", func(e *calendar.Event) { e.Status = "cancelled" }),
		ev("e22", "2026-10-23", "14:00", func(e *calendar.Event) { e.Original = zurich("2026-10-22", "09:00"); e.Location = "Salle 2" }),
		ev("x1", "2026-11-03", "08:00", func(e *calendar.Event) { e.RecurringID = ""; e.End = e.Start.Add(3 * time.Hour) }),
		{UID: "other", Title: "Autre chose", Start: zurich("2026-10-09", "10:00")},
	}
	r := must[*Synced](t)(s.Sync("RDIR", events, today, today.AddDate(0, 0, 40)))
	changes := map[string]string{}
	for _, c := range r.Changes {
		changes[c.Sitting] = c.What
	}
	if r.Events != 4 {
		t.Fatalf("matched %d events", r.Events)
	}
	if !strings.HasPrefix(changes["RDIR-2026-10-15"], "cancelled, items moved: RDIR-1") {
		t.Fatalf("cancel: %v", changes)
	}
	if changes["RDIR-2026-10-22"] != "moved to 2026-10-23, at 14:00, in Salle 2" {
		t.Fatalf("move: %v", changes)
	}
	if changes["RDIR-2026-11-03"] != "added from the calendar" {
		t.Fatalf("one-off: %v", changes)
	}
	if _, ok := changes["RDIR-2026-10-08"]; ok {
		t.Fatal("an unchanged sitting is not written")
	}
	if strings.Join(r.Unconfirmed, ",") != "RDIR-2026-10-29,RDIR-2026-11-05,RDIR-2026-11-12" {
		t.Fatalf("unconfirmed %v", r.Unconfirmed)
	}
	moved, _ := must[*Item](t)(s.Item("RDIR-1")), 0
	if moved.Sitting != "RDIR-2026-10-22" {
		t.Fatalf("item of the cancelled sitting went to %s", moved.Sitting)
	}
	oneOff, _, _ := s.Sitting("RDIR-2026-11-03")
	if oneOff.Duration != "3h" || oneOff.Event != "x1" || oneOff.Time != "08:00" {
		t.Fatalf("one-off %+v", oneOff)
	}
	// Run again: nothing changes, nothing is added twice.
	again := must[*Synced](t)(s.Sync("RDIR", events, today, today.AddDate(0, 0, 40)))
	if len(again.Changes) != 0 {
		t.Fatalf("second sync changed %v", again.Changes)
	}
	// The event moves again: found by its uid.
	events[2] = ev("e22", "2026-10-21", "16:00", func(e *calendar.Event) { e.Original = zurich("2026-10-22", "09:00") })
	third := must[*Synced](t)(s.Sync("RDIR", events, today, today.AddDate(0, 0, 40)))
	if len(third.Changes) != 1 || third.Changes[0].What != "moved to 2026-10-21, at 16:00" {
		t.Fatalf("third sync %v", third.Changes)
	}
	// A held sitting never changes.
	must[*Sitting](t)(s.HoldSitting("RDIR-2026-10-08", nil, nil))
	events[0] = ev("e8", "2026-10-08", "11:00", nil)
	if fourth := must[*Synced](t)(s.Sync("RDIR", events, today, today.AddDate(0, 0, 40))); len(fourth.Changes) != 0 {
		t.Fatalf("held sitting changed: %v", fourth.Changes)
	}
	// The cancelled event comes back.
	events[1] = ev("e15", "2026-10-15", "09:00", nil)
	fifth := must[*Synced](t)(s.Sync("RDIR", events, today, today.AddDate(0, 0, 40)))
	if len(fifth.Changes) != 1 || fifth.Changes[0].What != "back in the calendar" {
		t.Fatalf("reinstated: %v", fifth.Changes)
	}
}
