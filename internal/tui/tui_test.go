package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/aclemen1/oj-cli/internal/config"
	"github.com/aclemen1/oj-cli/internal/store"
)

func drive(t *testing.T, m *model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			drive(t, m, c)
		}
	case nil, tickMsg:
	default:
		if _, isBG := msg.(tea.BackgroundColorMsg); isBG {
			return
		}
		if strings.HasPrefix(typeName(msg), "tea.") {
			return
		}
		_, next := m.Update(msg)
		drive(t, m, next)
	}
}

func typeName(v any) string {
	switch v.(type) {
	case meetingsMsg, agendaMsg, itemMsg, actionsMsg, doneMsg, refMsg:
		return "oj"
	}
	return "tea.other"
}

func press(t *testing.T, m *model, s string) {
	t.Helper()
	var k tea.KeyPressMsg
	switch s {
	case "enter":
		k = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		k = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "space":
		k = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	default:
		k = tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
	}
	_, cmd := m.Update(k)
	drive(t, m, cmd)
}

func typeText(t *testing.T, m *model, s string) {
	for _, r := range s {
		press(t, m, string(r))
	}
}

func screen(m *model) string { return ansi.Strip(m.render()) }

func setup(t *testing.T) (*model, *store.Store, *time.Time) {
	root := t.TempDir()
	if err := store.Init(root, "none"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	st := &store.Store{Sphere: "pro", Root: root, VCS: "none", By: "alain", Now: func() time.Time { return now }}
	if _, err := st.AddMeeting("RDIR", store.MeetingInput{Title: "Séance de direction", RRule: "FREQ=WEEKLY;BYDAY=TH",
		Start: "2026-10-01T09:00", TZ: "Europe/Zurich", Duration: "1h", ItemDuration: "10m"}); err != nil {
		t.Fatal(err)
	}
	st.AddItem("RDIR", store.ItemInput{Title: "Budget 2027", Owner: "Marie", Kind: "decision", Duration: "20m"}, true, "")
	st.AddItem("RDIR", store.ItemInput{Title: "Point RH"}, true, "")
	st.AddItem("RDIR", store.ItemInput{Title: "Audit S3"}, false, "")
	m := newModel(st)
	m.now = func() time.Time { return now }
	m.w, m.h = 140, 40
	drive(t, m, m.Init())
	return m, st, &now
}

func TestMeetingsAndAgenda(t *testing.T) {
	m, st, _ := setup(t)
	if s := screen(m); !strings.Contains(s, "RDIR") || !strings.Contains(s, "RDIR-2026-10-08  2 on the agenda") || !strings.Contains(s, "1 proposed") {
		t.Fatalf("meetings:\n%s", s)
	}
	press(t, m, "enter")
	s := screen(m)
	for _, want := range []string{"Séance de direction", "09:00 RDIR-1", "09:20 RDIR-2", "Proposed", "Audit S3", "planned 30m of 1h"} {
		if !strings.Contains(s, want) {
			t.Fatalf("agenda lacks %q:\n%s", want, s)
		}
	}
	// Move Budget down, then lengthen Point RH, accept the proposal.
	press(t, m, "J")
	if m.agenda.Items[0].Item.ID != "RDIR-2" || m.sel != 1 {
		t.Fatalf("reorder: %s first, sel %d", m.agenda.Items[0].Item.ID, m.sel)
	}
	press(t, m, "k")
	press(t, m, "+")
	if it, _ := st.Item("RDIR-2"); it.Duration != "15m" {
		t.Fatalf("nudge: %s", it.Duration)
	}
	press(t, m, "G")
	m.sel = 2
	press(t, m, "a")
	if it, _ := st.Item("RDIR-3"); it.State != "accepted" {
		t.Fatalf("accept: %s", it.State)
	}
	// A new item through the prompt.
	press(t, m, "n")
	typeText(t, m, "Divers")
	press(t, m, "enter")
	if len(m.agenda.Items) != 4 || m.agenda.Items[3].Item.Title != "Divers" {
		t.Fatalf("new item: %+v", m.agenda.Items)
	}
	// Drop needs a reason.
	m.sel = 3
	press(t, m, "x")
	typeText(t, m, "doublon")
	press(t, m, "enter")
	if it, _ := st.Item("RDIR-4"); it.State != "dropped" || it.Reason != "doublon" {
		t.Fatalf("drop: %+v", it)
	}
	// Next sitting and back.
	press(t, m, "]")
	if m.agenda.Sitting.ID != "RDIR-2026-10-15" {
		t.Fatalf("next sitting %s", m.agenda.Sitting.ID)
	}
	press(t, m, "[")
	if m.agenda.Sitting.ID != "RDIR-2026-10-08" {
		t.Fatalf("previous sitting %s", m.agenda.Sitting.ID)
	}
	// Item detail.
	m.sel = 0
	press(t, m, "enter")
	if s := screen(m); m.view != vItem || !strings.Contains(s, "Point RH") || !strings.Contains(s, "Log") {
		t.Fatalf("item:\n%s", s)
	}
	press(t, m, "esc")
	if m.view != vAgenda {
		t.Fatal("esc should return to the agenda")
	}
}

func TestItemPane(t *testing.T) {
	m, st, _ := setup(t)
	st.EditItem("RDIR-1", store.ItemInput{Expected: "Approuver le projet de budget ?", Notes: "Le rectorat attend la version finale.",
		Attach: []string{"artefact://pro/01JB"}, Refs: []string{"office:U-0042"}})
	press(t, m, "enter")
	s := screen(m)
	for _, want := range []string{"── RDIR-1 · Budget 2027", "Owner     Marie · decision · 20m", "Question  Approuver le projet de budget ?",
		"Attached  artefact://pro/01JB", "Refs      office:U-0042", "Notes", "Le rectorat attend la version finale."} {
		if !strings.Contains(s, want) {
			t.Fatalf("pane lacks %q:\n%s", want, s)
		}
	}
	press(t, m, "j")
	if s := screen(m); !strings.Contains(s, "── RDIR-2 · Point RH") || strings.Contains(s, "Approuver le projet") {
		t.Fatalf("pane follows the selection:\n%s", s)
	}
	press(t, m, "l")
	if s := screen(m); !strings.Contains(s, "── RDIR-1 · Budget 2027") {
		t.Fatalf("live pane:\n%s", s)
	}
}

func TestPaneShowsRefTarget(t *testing.T) {
	m, st, _ := setup(t)
	st.Refs = map[string]config.RefSource{"office": {Show: []string{"printf", "%s · open · Budget 2027\n\n## Instruction\n\nPréparer le budget avec la **direction**.", "{id}"}}}
	st.EditItem("RDIR-1", store.ItemInput{Refs: []string{"office:U-0042", "gmail:thread/abc"}})
	press(t, m, "enter")
	s := screen(m)
	for _, want := range []string{"·· office:U-0042", "U-0042 · open · Budget 2027", "Préparer le budget avec la direction."} {
		if !strings.Contains(s, want) {
			t.Fatalf("ref pane lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "·· gmail:thread/abc") {
		t.Fatal("a ref without command gets no section")
	}
	if strings.Contains(s, "## Instruction") || strings.Contains(s, "**direction**") || !strings.Contains(s, "Instruction") {
		t.Fatalf("the ref text is not rendered as Markdown:\n%s", s)
	}
	press(t, m, "enter")
	if s := screen(m); m.view != vItem || !strings.Contains(s, "Préparer le budget avec la direction.") {
		t.Fatalf("item view lacks the ref text:\n%s", s)
	}
}

func TestLiveSittingAndMinutes(t *testing.T) {
	m, st, now := setup(t)
	press(t, m, "enter")
	press(t, m, "f") // a proposal is still there
	if !m.statusErr || !strings.Contains(m.status, "proposed") {
		t.Fatalf("freeze with proposal: %q", m.status)
	}
	press(t, m, "l")
	press(t, m, "space")
	*now = now.Add(12 * time.Minute)
	if s := screen(m); !strings.Contains(s, "12:00") || !strings.Contains(s, "timer running") {
		t.Fatalf("timer:\n%s", s)
	}
	press(t, m, "D")
	typeText(t, m, "Approuvé")
	press(t, m, "enter")
	press(t, m, "t")
	typeText(t, m, "Envoyer au rectorat|Marie|2026-10-20")
	press(t, m, "enter")
	press(t, m, "s")
	typeText(t, m, "Présenté")
	press(t, m, "enter")
	it, _ := st.Item("RDIR-1")
	o := it.History[0].Outcome
	if o.Decision != "Approuvé" || o.Summary != "Présenté" || len(o.Actions) != 1 || o.Status != "approved" {
		t.Fatalf("outcome %+v", o)
	}
	press(t, m, "n")
	*now = now.Add(3 * time.Minute)
	press(t, m, "-")
	if it, _ := st.Item("RDIR-2"); it.History[0].Outcome.Next != "deferred" {
		t.Fatal("defer in live")
	}
	if s := screen(m); !strings.Contains(s, "elapsed 15:00") {
		t.Fatalf("elapsed:\n%s", s)
	}
	press(t, m, "h")
	press(t, m, "esc")
	press(t, m, "m")
	typeText(t, m, "yes")
	press(t, m, "enter")
	if m.agenda.Sitting.State != "minuted" {
		t.Fatalf("minute: %s (%s)", m.agenda.Sitting.State, m.status)
	}
	// Actions view: mark the action done.
	press(t, m, "A")
	if s := screen(m); !strings.Contains(s, "Envoyer au rectorat") || !strings.Contains(s, "RDIR-1#1") {
		t.Fatalf("actions:\n%s", s)
	}
	press(t, m, "space")
	if len(m.actions) != 0 {
		t.Fatal("a done action leaves the open list")
	}
	press(t, m, "o")
	if len(m.actions) != 1 || !m.actions[0].Done {
		t.Fatalf("show done: %+v", m.actions)
	}
}

func TestMarkdownKeepsLineBreaks(t *testing.T) {
	var trimmed []string
	for _, l := range markdown("Première ligne.\nDeuxième ligne.\n\n- un\n- deux\n\n```\ncode a\ncode b\n```", 80) {
		trimmed = append(trimmed, strings.TrimRight(ansi.Strip(l), " "))
	}
	got := strings.Join(trimmed, "\n")
	for _, want := range []string{"Première ligne.\n", "Deuxième ligne.", "code a", "code b"} {
		if !strings.Contains(got, want) {
			t.Fatalf("lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Première ligne. Deuxième") {
		t.Fatalf("lines joined:\n%s", got)
	}
}
