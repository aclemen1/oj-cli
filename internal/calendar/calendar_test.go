package calendar

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const sample = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" +
	"BEGIN:VEVENT\r\nUID:rdir@x\r\nSUMMARY:Séance de direction\r\nLOCATION:Room 4\r\n" +
	"DTSTART;TZID=Europe/Zurich:20261001T090000\r\nDTEND;TZID=Europe/Zurich:20261001T103000\r\n" +
	"RRULE:FREQ=WEEKLY;BYDAY=TH\r\nEXDATE;TZID=Europe/Zurich:20261015T090000\r\nEND:VEVENT\r\n" +
	// The 22nd moves to Friday 23rd at 14:00.
	"BEGIN:VEVENT\r\nUID:rdir@x\r\nRECURRENCE-ID;TZID=Europe/Zurich:20261022T090000\r\nSUMMARY:Séance de direction\r\n" +
	"DTSTART;TZID=Europe/Zurich:20261023T140000\r\nDTEND;TZID=Europe/Zurich:20261023T153000\r\nEND:VEVENT\r\n" +
	// The 29th is cancelled.
	"BEGIN:VEVENT\r\nUID:rdir@x\r\nRECURRENCE-ID;TZID=Europe/Zurich:20261029T090000\r\nSTATUS:CANCELLED\r\n" +
	"SUMMARY:Séance de direction\r\nDTSTART;TZID=Europe/Zurich:20261029T090000\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:retraite@x\r\nSUMMARY:Retraite\\, annuelle\r\nDTSTART:20261120T073000Z\r\n" +
	"DTEND:20261120T160000Z\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func TestParseICS(t *testing.T) {
	zh, _ := time.LoadLocation("Europe/Zurich")
	from, to := time.Date(2026, 10, 5, 0, 0, 0, 0, zh), time.Date(2026, 12, 1, 0, 0, 0, 0, zh)
	l, err := ParseICS([]byte(sample), from, to)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range l {
		s := e.Start.In(zh).Format("01-02 15:04") + " " + e.Title
		if e.Cancelled() {
			s += " cancelled"
		}
		if !e.Original.IsZero() && !e.Original.Equal(e.Start) {
			s += " (from " + e.Original.In(zh).Format("01-02") + ")"
		}
		got = append(got, s)
	}
	want := []string{
		"10-08 09:00 Séance de direction",
		"10-23 14:00 Séance de direction (from 10-22)",
		"10-29 09:00 Séance de direction cancelled",
		"11-05 09:00 Séance de direction",
		"11-12 09:00 Séance de direction",
		"11-19 09:00 Séance de direction",
		"11-20 08:30 Retraite, annuelle",
		"11-26 09:00 Séance de direction",
	}
	if len(got) != len(want) {
		t.Fatalf("events:\n%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d: %q, want %q", i, got[i], want[i])
		}
	}
	if l[0].RecurringID != "rdir@x" || l[0].Location != "Room 4" || l[0].End.Sub(l[0].Start) != 90*time.Minute {
		t.Fatalf("first event %+v", l[0])
	}
}

func TestCommandSource(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "events.sh")
	os.WriteFile(script, []byte(`#!/bin/sh
test -n "$ORDO_FROM" || exit 3
cat <<'EOF'
[{"uid": "e1", "start": "2026-10-09T09:00:00+02:00", "end": "2026-10-09T10:30:00+02:00", "title": "Séance de direction",
  "recurring_id": "r1", "original_start": "2026-10-08T09:00:00+02:00"},
 {"uid": "e2", "start": "2026-11-20", "title": "Retraite", "status": "cancelled"}]
EOF
`), 0o755)
	l, err := Source{Type: "command", Run: []string{script}}.Events(context.Background(), time.Now(), time.Now().AddDate(0, 1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(l) != 2 || l[0].Original.Day() != 8 || l[0].RecurringID != "r1" || !l[1].Cancelled() {
		t.Fatalf("events %+v", l)
	}
	if _, err := ParseJSON([]byte(`[{"start": "2026-10-09"}]`)); err == nil {
		t.Fatal("an event without uid should be refused")
	}
	if _, err := (Source{Type: "command", Run: []string{"false"}}).Events(context.Background(), time.Now(), time.Now()); err == nil {
		t.Fatal("a failing command should be an error")
	}
}
