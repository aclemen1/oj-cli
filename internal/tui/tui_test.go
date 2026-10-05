package tui

import (
	"os"
	"path/filepath"
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
	case meetingsMsg, agendaMsg, itemMsg, actionsMsg, doneMsg, refMsg, overviewMsg:
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
	m.helpOff = true
	drive(t, m, m.Init())
	return m, st, &now
}

func TestMeetingsAndAgenda(t *testing.T) {
	m, st, _ := setup(t)
	if s := screen(m); !strings.Contains(s, "RDIR") || !strings.Contains(s, "RDIR-2026-10-08  Thu 2026-10-08  2 on the agenda") || !strings.Contains(s, "1 proposed") {
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

func TestDeferredSectionAndUndefer(t *testing.T) {
	m, st, _ := setup(t)
	press(t, m, "enter")
	press(t, m, "d") // defer RDIR-1 from the 8th
	s := screen(m)
	if !strings.Contains(s, "Deferred") || !strings.Contains(s, "→ RDIR-2026-10-15") {
		t.Fatalf("deferred section:\n%s", s)
	}
	for i, r := range m.rows {
		if r.away {
			m.sel = i
		}
	}
	m.status = ""
	if !strings.Contains(screen(m), "u undefer") {
		t.Fatalf("help should name undefer:\n%s", screen(m))
	}
	press(t, m, "u")
	if it, _ := st.Item("RDIR-1"); it.State != "accepted" || it.Sitting != "RDIR-2026-10-08" {
		t.Fatalf("undefer from the TUI: %+v", it)
	}
}

func TestSittingsOverview(t *testing.T) {
	m, st, _ := setup(t)
	st.AddItem("RDIR", store.ItemInput{Title: "Plus tard"}, true, "RDIR-2026-10-22")
	press(t, m, "S")
	s := screen(m)
	for _, want := range []string{"RDIR", "RDIR-2026-10-08", "Budget 2027", "Audit S3", "RDIR-2026-10-22", "Plus tard", "RDIR-2026-10-15"} {
		if !strings.Contains(s, want) {
			t.Fatalf("overview lacks %q:\n%s", want, s)
		}
	}
	// Go to "Plus tard" and open its sitting with it selected.
	for i, r := range m.ovRows {
		if r.item != nil && r.item.Title == "Plus tard" {
			m.selO = i
		}
	}
	press(t, m, "enter")
	if m.view != vAgenda || m.agenda.Sitting.ID != "RDIR-2026-10-22" || m.current().item.Title != "Plus tard" {
		t.Fatalf("open from overview: view %d, %s", m.view, m.agenda.Sitting.ID)
	}
	press(t, m, "S")
	if m.ovScope != "RDIR" {
		t.Fatalf("scope from an agenda: %q", m.ovScope)
	}
	press(t, m, "esc")
	if m.view != vAgenda {
		t.Fatal("esc goes back to the agenda")
	}
}

func TestHelpPanel(t *testing.T) {
	m, st, _ := setup(t)
	m.helpOff = false
	st.Render.Lang = "fr"
	press(t, m, "enter")
	s := screen(m)
	for _, want := range []string{"OÙ VOUS EN ÊTES", "● planned", "frozen", "ÉTAPE SUIVANTE", "Trancher les points proposés",
		"POINT · RDIR-1", "Retenu à l'ordre du jour", "le reporter", "NAVIGUER"} {
		if !strings.Contains(s, want) {
			t.Fatalf("help lacks %q:\n%s", want, s)
		}
	}
	// The help follows the selection and the sitting's state.
	for i, r := range m.rows {
		if r.proposed {
			m.sel = i
		}
	}
	if s := screen(m); !strings.Contains(s, "Proposé : quelqu'un le demande") || !strings.Contains(s, "le retenir") {
		t.Fatalf("help for a proposal:\n%s", s)
	}
	press(t, m, "a")
	press(t, m, "f")
	if s := screen(m); !strings.Contains(s, "● frozen") || !strings.Contains(s, "Le jour de la séance") {
		t.Fatalf("help once frozen:\n%s", s)
	}
	press(t, m, "?")
	if s := screen(m); strings.Contains(s, "OÙ VOUS EN ÊTES") || !strings.Contains(s, "? aide") {
		t.Fatalf("help hidden:\n%s", s)
	}
	// Narrow screen: the help goes below.
	press(t, m, "?")
	m.w = 90
	if s := screen(m); !strings.Contains(s, "OÙ VOUS EN ÊTES") {
		t.Fatalf("help below on a narrow screen:\n%s", s)
	}
}

func TestMovePicker(t *testing.T) {
	m, st, _ := setup(t)
	press(t, m, "enter")
	press(t, m, "M")
	if m.moving == nil || len(m.choices) == 0 || m.choices[0].ID != "RDIR-2026-10-15" {
		t.Fatalf("picker %+v", m.choices)
	}
	if s := screen(m); !strings.Contains(s, "Move RDIR-1") || !strings.Contains(s, "RDIR-2026-10-22") {
		t.Fatalf("picker screen:\n%s", s)
	}
	press(t, m, "j")
	press(t, m, "enter")
	if it, _ := st.Item("RDIR-1"); it.Sitting != "RDIR-2026-10-22" || it.State != "accepted" {
		t.Fatalf("moved %+v", it)
	}
	press(t, m, "M")
	press(t, m, "esc")
	if m.moving != nil {
		t.Fatal("esc cancels")
	}
}

func TestStateSurvivesRestart(t *testing.T) {
	m, st, now := setup(t)
	path := filepath.Join(t.TempDir(), "tui-pro.json")
	m.statePath = path
	press(t, m, "enter")
	press(t, m, "j")
	press(t, m, "?")
	press(t, m, "l")
	press(t, m, "space")
	*now = now.Add(5 * time.Minute)
	press(t, m, "n") // timer moves to RDIR-2 after 5 min on RDIR-1

	m2 := newModel(st)
	m2.now = m.now
	m2.w, m2.h = 140, 40
	m2.statePath = path
	drive(t, m2, m2.Init())
	if m2.view != vLive || m2.agenda == nil || m2.agenda.Sitting.ID != "RDIR-2026-10-08" {
		t.Fatalf("restored view %d, agenda %v", m2.view, m2.agenda)
	}
	if m2.helpOff != m.helpOff {
		t.Fatal("help setting lost")
	}
	if it := m2.liveItem(); it == nil || it.ID != "RDIR-2" || !m2.live.running {
		t.Fatalf("live item %v running %v", it, m2.live.running)
	}
	if m2.live.spent["RDIR-1"] != 5*time.Minute {
		t.Fatalf("timer of RDIR-1: %v", m2.live.spent["RDIR-1"])
	}
	press(t, m2, "esc")
	if m2.view != vAgenda || m2.current().item.ID != "RDIR-2" {
		t.Fatalf("agenda selection after restart: %s", m2.current().item.ID)
	}
}

func TestJumpToRef(t *testing.T) {
	m, st, _ := setup(t)
	out := filepath.Join(t.TempDir(), "jumped")
	st.Refs = map[string]config.RefSource{"office": {Show: []string{"echo", "dossier {id}"},
		Open: []string{"sh", "-c", "echo $0 > " + out, "{id}"}}}
	st.EditItem("RDIR-1", store.ItemInput{Refs: []string{"office:U-0042"}})
	m.helpOff = false
	press(t, m, "enter")
	if s := screen(m); !strings.Contains(s, "go to office:U-0042") {
		t.Fatalf("help names the jump:\n%s", s)
	}
	press(t, m, "o")
	if b, _ := os.ReadFile(out); strings.TrimSpace(string(b)) != "U-0042" {
		t.Fatalf("open command got %q", b)
	}
	press(t, m, "j") // RDIR-2 has no ref
	press(t, m, "o")
	if !m.statusErr {
		t.Fatal("no ref: an error in the status line")
	}
}

func TestWeekdays(t *testing.T) {
	m, st, _ := setup(t)
	if s := screen(m); !strings.Contains(s, "Thu 2026-10-08") {
		t.Fatalf("english weekday:\n%s", s)
	}
	st.Render.Lang = "fr"
	press(t, m, "enter")
	if s := screen(m); !strings.Contains(s, "jeu. 2026-10-08 09:00") {
		t.Fatalf("french weekday in the sitting header:\n%s", s)
	}
	press(t, m, "S")
	if s := screen(m); !strings.Contains(s, "jeu. 2026-10-15") {
		t.Fatalf("weekday in the sittings view:\n%s", s)
	}
}
