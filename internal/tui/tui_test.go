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

// The watch sleeps between looks; tests send its messages themselves.
func init() { watchEvery = 0 }

func TestStandingInTUI(t *testing.T) {
	m, st, now := setup(t)
	if _, err := st.AddStanding("RDIR", store.Standing{Key: "suite", Title: "Date de la prochaine séance", Duration: "5m"}); err != nil {
		t.Fatal(err)
	}
	press(t, m, "enter")
	last := len(m.agenda.Items) - 1
	if !m.agenda.Items[last].Item.Virtual {
		t.Fatal("the recurring item goes at the end")
	}
	m.sel = last
	if s := screen(m); !strings.Contains(s, "↻ suite") || !strings.Contains(s, "recurring item suite") {
		t.Fatalf("virtual recurring item:\n%s", s)
	}
	// Moving an item writes the recurring ones first, then moves.
	m.sel = 0
	press(t, m, "ctrl+j")
	for _, ai := range m.agenda.Items {
		if ai.Item.Virtual {
			t.Fatal("J should have written the recurring item")
		}
	}
	if m.agenda.Items[1].Item.Title != "Budget 2027" || m.agenda.Items[2].Item.StandingKey() != "suite" {
		t.Fatalf("order after J: %v", agendaIDs(m))
	}
	// The next sitting shows its own instance; the live timer follows it when written.
	press(t, m, "]")
	press(t, m, "L")
	press(t, m, "space")
	*now = now.Add(2 * time.Minute)
	press(t, m, "E")
	typeText(t, m, "Le 22")
	press(t, m, "enter")
	it := m.agenda.Items[0].Item
	if it.Virtual || it.StandingKey() != "suite" || it.History[0].Outcome.Summary != "Le 22" {
		t.Fatalf("live outcome on a recurring item %+v", it)
	}
	if s := screen(m); !strings.Contains(s, "2:00") {
		t.Fatalf("timer lost when the item was written:\n%s", s)
	}
}

func TestHeldSittingPreviewAndAsk(t *testing.T) {
	m, st, _ := setup(t)
	log := filepath.Join(t.TempDir(), "asked")
	st.Refs = map[string]config.RefSource{"office": {Ask: []string{"sh", "-c", `printf '%s\n%s' "$0" "$1" > ` + log, "{id}", "{text}"}}}
	st.Asks = map[string]string{"outcomes": "Issues de {sitting} ({meeting_title}) :\n{items}"}
	st.EditMeeting("RDIR", store.MeetingInput{Refs: []string{"office:U-0006"}})
	st.HoldSitting("RDIR-2026-10-08", nil, nil)
	if _, err := st.AddStanding("RDIR", store.Standing{Key: "suite", Title: "Date de la prochaine séance"}); err != nil {
		t.Fatal(err)
	}
	// The meetings list shows the sitting to finish, and enter opens it.
	drive(t, m, m.loadMeetings())
	if s := screen(m); !strings.Contains(s, "minutes to do: RDIR-2026-10-08") {
		t.Fatalf("meetings:\n%s", s)
	}
	press(t, m, "enter")
	if m.agenda.Sitting.ID != "RDIR-2026-10-08" {
		t.Fatalf("enter opened %s", m.agenda.Sitting.ID)
	}
	// A recurring item declared after the hold shows, and can be noted.
	last := m.agenda.Items[len(m.agenda.Items)-1].Item
	if !last.Virtual || last.Standing != "suite" {
		t.Fatalf("missed recurring item: %+v", last)
	}
	// G sends the sphere's request to the meeting's ref target.
	press(t, m, "R")
	b, _ := os.ReadFile(log)
	got := string(b)
	if !strings.HasPrefix(got, "U-0006\nIssues de RDIR-2026-10-08 (Séance de direction) :") || !strings.Contains(got, "- RDIR-1 · Budget 2027") {
		t.Fatalf("ask received:\n%s (%s)", got, m.status)
	}
	// P shows the draft minutes.
	st.SetOutcome("RDIR-1", "", store.OutcomeInput{Decision: "Budget approuvé"})
	press(t, m, "P")
	if s := screen(m); m.view != vDoc || !strings.Contains(s, "Draft minutes") || !strings.Contains(s, "Budget approuvé") {
		t.Fatalf("preview:\n%s", s)
	}
	press(t, m, "esc")
	if m.view != vAgenda {
		t.Fatal("esc goes back to the agenda")
	}
}

func TestStandingFromTUI(t *testing.T) {
	m, st, _ := setup(t)
	press(t, m, "enter")
	// * on Point RH: recurring, at the start.
	m.sel = 1
	press(t, m, "*")
	m.input.SetValue("")
	typeText(t, m, "start")
	press(t, m, "enter")
	mt, _ := st.Meeting("RDIR")
	if len(mt.Standing) != 1 || mt.Standing[0].Place != "start" || mt.Standing[0].Title != "Point RH" {
		t.Fatalf("standing %+v (%s)", mt.Standing, m.status)
	}
	if it, _ := st.Item("RDIR-2"); it.StandingKey() != mt.Standing[0].Key {
		t.Fatalf("the item is the instance: %+v", it.Refs)
	}
	press(t, m, "]")
	if !m.agenda.Items[0].Item.Virtual || m.agenda.Items[0].Item.Title != "Point RH" {
		t.Fatalf("next sitting starts with the recurring item: %v", agendaIDs(m))
	}
	// R: the list; n adds, s moves, x stops.
	press(t, m, "g")
	press(t, m, "r")
	if m.view != vStanding || !strings.Contains(screen(m), "↻ "+mt.Standing[0].Key) {
		t.Fatalf("standing view:\n%s", screen(m))
	}
	press(t, m, "c")
	typeText(t, m, "Repas")
	press(t, m, "enter")
	if len(m.standing) != 2 || m.standing[1].Key != "repas" || m.standing[1].Place != "end" {
		t.Fatalf("added %+v", m.standing)
	}
	m.selR = 1
	press(t, m, "p")
	if m.standing[1].Place != "start" {
		t.Fatalf("place %+v", m.standing[1])
	}
	press(t, m, "x")
	if len(m.standing) != 1 {
		t.Fatalf("stopped %+v", m.standing)
	}
	press(t, m, "esc")
	if m.view != vAgenda {
		t.Fatal("esc goes back to the agenda")
	}
}

func agendaIDs(m *model) []string {
	var l []string
	for _, ai := range m.agenda.Items {
		l = append(l, idLabel(ai.Item))
	}
	return l
}

func TestWatchReloadsOnOutsideChange(t *testing.T) {
	m, st, _ := setup(t)
	press(t, m, "enter")
	if len(m.agenda.Items) != 2 {
		t.Fatalf("agenda %d items", len(m.agenda.Items))
	}
	send := func() {
		_, cmd := m.Update(watchMsg{stamp: stamps(m.stores)})
		drive(t, m, cmd)
	}
	send() // first look: remembers the stamp
	time.Sleep(10 * time.Millisecond)
	st.AddItem("RDIR", store.ItemInput{Title: "Ajouté ailleurs"}, true, "")
	m.sel = 1
	send()
	if len(m.agenda.Items) != 3 || m.sel != 1 {
		t.Fatalf("after an outside change: %d items, sel %d", len(m.agenda.Items), m.sel)
	}
	// While the user types, the change waits.
	press(t, m, "n")
	time.Sleep(10 * time.Millisecond)
	st.AddItem("RDIR", store.ItemInput{Title: "Encore"}, true, "")
	send()
	if len(m.agenda.Items) != 3 {
		t.Fatal("no reload during a prompt")
	}
	press(t, m, "esc")
	send()
	if len(m.agenda.Items) != 4 {
		t.Fatalf("the waiting change is applied after the prompt: %d items", len(m.agenda.Items))
	}
}

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
	case meetingsMsg, agendaMsg, itemMsg, actionsMsg, doneMsg, refMsg, citedMsg, overviewMsg, standingMsg, docMsg:
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
	case "tab":
		k = tea.KeyPressMsg{Code: tea.KeyTab}
	case "ctrl+j", "ctrl+k":
		k = tea.KeyPressMsg{Code: rune(s[5]), Mod: tea.ModCtrl}
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
	if s := screen(m); !strings.Contains(s, "RDIR") || !strings.Contains(s, "Thu 2026-10-08 · in 3 days") || !strings.Contains(s, "1 proposed") {
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
	press(t, m, "ctrl+j")
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
	press(t, m, "L")
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
	press(t, m, "L")
	press(t, m, "space")
	*now = now.Add(12 * time.Minute)
	if s := screen(m); !strings.Contains(s, "12:00") || !strings.Contains(s, "timer running") {
		t.Fatalf("timer:\n%s", s)
	}
	press(t, m, "D")
	typeText(t, m, "Approuvé")
	press(t, m, "enter")
	press(t, m, "c")
	typeText(t, m, "Envoyer au rectorat|Marie|2026-10-20")
	press(t, m, "enter")
	press(t, m, "E")
	typeText(t, m, "Présenté")
	press(t, m, "enter")
	it, _ := st.Item("RDIR-1")
	o := it.History[0].Outcome
	if o.Decision != "Approuvé" || o.Summary != "Présenté" || len(o.Actions) != 1 || o.Status != "approved" {
		t.Fatalf("outcome %+v", o)
	}
	press(t, m, "n")
	*now = now.Add(3 * time.Minute)
	press(t, m, "z")
	if it, _ := st.Item("RDIR-2"); it.History[0].Outcome.Next != "deferred" {
		t.Fatal("defer in live")
	}
	if s := screen(m); !strings.Contains(s, "elapsed 15:00") {
		t.Fatalf("elapsed:\n%s", s)
	}
	press(t, m, "H")
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
	press(t, m, "f")
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
	press(t, m, "L")
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
	if s := screen(m); !strings.Contains(s, "Thu 2026-10-08 · in 3 days") {
		t.Fatalf("english weekday:\n%s", s)
	}
	st.Render.Lang = "fr"
	press(t, m, "enter")
	if s := screen(m); !strings.Contains(s, "jeu. 2026-10-08 09:00 · dans 3 j") {
		t.Fatalf("french weekday in the sitting header:\n%s", s)
	}
	press(t, m, "S")
	if s := screen(m); !strings.Contains(s, "jeu. 2026-10-15 09:00 · dans 10 j") {
		t.Fatalf("weekday in the sittings view:\n%s", s)
	}
}

func TestRelativeDates(t *testing.T) {
	m, st, _ := setup(t) // today is Monday 2026-10-05
	cases := map[string][2]string{
		"2026-10-05": {"aujourd'hui", "today"},
		"2026-10-06": {"demain", "tomorrow"},
		"2026-10-04": {"hier", "yesterday"},
		"2026-10-15": {"dans 10 j", "in 10 days"},
		"2026-09-30": {"il y a 5 j", "5 days ago"},
		"2026-10-26": {"dans 3 sem.", "in 3 weeks"},
		"2026-08-31": {"il y a 5 sem.", "5 weeks ago"},
	}
	for d, want := range cases {
		day, _ := time.Parse("2006-01-02", d)
		st.Render.Lang = "fr"
		if got := m.relative(day); got != want[0] {
			t.Errorf("%s fr: %q, want %q", d, got, want[0])
		}
		st.Render.Lang = ""
		if got := m.relative(day); got != want[1] {
			t.Errorf("%s en: %q, want %q", d, got, want[1])
		}
	}
}

func TestEscBackQQuits(t *testing.T) {
	m, _, _ := setup(t)
	// esc on the meetings list does not quit.
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("esc must never quit")
		}
	}
	press(t, m, "enter")
	press(t, m, "L")
	press(t, m, "esc")
	if m.view != vAgenda {
		t.Fatalf("esc from live: view %d", m.view)
	}
	press(t, m, "esc")
	if m.view != vMeetings {
		t.Fatalf("esc from agenda: view %d", m.view)
	}
	// q quits from a deep view, but is typed in a prompt.
	press(t, m, "enter")
	press(t, m, "n")
	press(t, m, "q")
	if m.prompt == pNone || m.input.Value() != "q" {
		t.Fatalf("q in a prompt: prompt %d, value %q", m.prompt, m.input.Value())
	}
	press(t, m, "esc")
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("q should quit")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Fatal("q should quit from the agenda")
	}
}

func TestSeveralSpheres(t *testing.T) {
	_, pro, now := setup(t)
	root := t.TempDir()
	if err := store.Init(root, "none"); err != nil {
		t.Fatal(err)
	}
	perso := &store.Store{Sphere: "perso", Root: root, VCS: "none", By: "alain", Now: func() time.Time { return *now }}
	if _, err := perso.AddMeeting("FAM", store.MeetingInput{Title: "Conseil de famille", TZ: "Europe/Zurich"}); err != nil {
		t.Fatal(err)
	}
	if _, err := perso.AddSitting("FAM", "2026-10-10", "18:00", ""); err != nil {
		t.Fatal(err)
	}
	m := newModel(perso, pro)
	m.now = func() time.Time { return *now }
	m.w, m.h, m.helpOff = 140, 40, true
	drive(t, m, m.Init())
	s := screen(m)
	if !strings.Contains(s, "perso:FAM") || !strings.Contains(s, "pro:RDIR") || !strings.Contains(s, "s sphere") {
		t.Fatalf("both spheres:\n%s", s)
	}
	press(t, m, "s")
	if s := screen(m); m.filter != "perso" || strings.Contains(s, "RDIR") {
		t.Fatalf("filter perso (%q):\n%s", m.filter, s)
	}
	press(t, m, "s")
	press(t, m, "s")
	if m.filter != "" || len(m.meetings) != 2 {
		t.Fatalf("filter back to all: %q %d", m.filter, len(m.meetings))
	}
	// Opening a meeting makes its sphere the one changes go to.
	for i, r := range m.meetings {
		if r.alias == "RDIR" {
			m.selM = i
		}
	}
	press(t, m, "enter")
	if m.st != pro || m.agenda == nil || m.agenda.Sitting.Meeting != "RDIR" {
		t.Fatalf("open RDIR in pro: %s", m.st.Sphere)
	}
	press(t, m, "n")
	typeText(t, m, "Divers")
	press(t, m, "enter")
	if _, err := pro.Item("RDIR-4"); err != nil {
		t.Fatalf("item added in pro: %v", err)
	}
	press(t, m, "esc")
	press(t, m, "S")
	if s := screen(m); !strings.Contains(s, "perso:FAM") || !strings.Contains(s, "pro:RDIR") {
		t.Fatalf("overview of both spheres:\n%s", s)
	}
}

func TestEditorArgs(t *testing.T) {
	cases := []struct {
		editor string
		add    bool
		want   string
	}{
		{"nvim", true, "nvim|+normal! Go|+startinsert|f.md"},
		{"/opt/homebrew/bin/vim -u NONE", true, "/opt/homebrew/bin/vim|-u|NONE|+normal! Go|+startinsert|f.md"},
		{"nvim", false, "nvim|f.md"},
		{"code --wait", true, "code|--wait|f.md"},
		{"", false, "vi|f.md"},
	}
	for _, c := range cases {
		if got := strings.Join(editorArgs(c.editor, "f.md", c.add), "|"); got != c.want {
			t.Errorf("editorArgs(%q, %v) = %q, want %q", c.editor, c.add, got, c.want)
		}
	}
}

func TestReloadAfterRebuild(t *testing.T) {
	m, _, _ := setup(t)
	bin := filepath.Join(t.TempDir(), "oj")
	os.WriteFile(bin, []byte("v1"), 0o755)
	m.exe, m.exeStamp = bin, binStamp(bin)
	quits := func(cmd tea.Cmd) bool {
		if cmd == nil {
			return false
		}
		_, ok := cmd().(tea.QuitMsg)
		return ok
	}
	if _, cmd := m.Update(watchMsg{bin: binStamp(bin)}); quits(cmd) || m.newBin {
		t.Fatal("same binary: no reload")
	}
	// A rebuild replaces the file (new inode) while the user types: a badge, no reload.
	press(t, m, "enter")
	press(t, m, "n")
	os.Remove(bin)
	os.WriteFile(bin, []byte("v2"), 0o755)
	if _, cmd := m.Update(watchMsg{bin: binStamp(bin)}); quits(cmd) || !m.newBin || m.reexec {
		t.Fatal("no reload during a prompt")
	}
	if !strings.Contains(m.render(), "nouvelle version") && !strings.Contains(m.render(), "new version") {
		t.Fatal("badge missing")
	}
	press(t, m, "esc")
	if _, cmd := m.Update(watchMsg{bin: binStamp(bin)}); !quits(cmd) || !m.reexec {
		t.Fatal("reload once idle")
	}
	// SIGUSR1 follows the same rules.
	m2, _, _ := setup(t)
	m2.exe = bin
	press(t, m2, "enter")
	press(t, m2, "n")
	if _, cmd := m2.Update(reloadMsg{}); quits(cmd) {
		t.Fatal("SIGUSR1 waits for the prompt")
	}
	press(t, m2, "esc")
	if _, cmd := m2.Update(watchMsg{}); !quits(cmd) {
		t.Fatal("SIGUSR1 reloads once idle")
	}
}

func TestCommonKeys(t *testing.T) {
	m, st, _ := setup(t)
	press(t, m, "l") // l opens, like enter
	if m.view != vAgenda || len(m.rows) != 3 {
		t.Fatalf("l opens the agenda: view %d, %d rows", m.view, len(m.rows))
	}
	press(t, m, "G")
	if m.sel != 2 {
		t.Fatalf("G goes to the end: sel %d", m.sel)
	}
	press(t, m, "g")
	press(t, m, "g")
	if m.sel != 0 {
		t.Fatalf("gg goes to the top: sel %d", m.sel)
	}
	// / filters the list; esc clears the filter before going back.
	press(t, m, "/")
	for _, r := range "audit" {
		press(t, m, string(r))
	}
	press(t, m, "enter")
	if len(m.rows) != 1 || m.rows[0].item.ID != "RDIR-3" || !strings.Contains(screen(m), "/audit") {
		t.Fatalf("filter: %d rows\n%s", len(m.rows), screen(m))
	}
	press(t, m, "esc")
	if m.view != vAgenda || m.query != "" || len(m.rows) != 3 {
		t.Fatalf("esc clears the filter first: view %d, query %q, %d rows", m.view, m.query, len(m.rows))
	}
	// tab hides the item pane.
	if !strings.Contains(screen(m), "── RDIR-1") {
		t.Fatal("pane shown by default")
	}
	press(t, m, "tab")
	if strings.Contains(screen(m), "── RDIR-1") {
		t.Fatal("tab hides the pane")
	}
	press(t, m, "tab")
	// 2 shows the actions; e marks done, enter opens the item.
	st.SetOutcome("RDIR-1", "", store.OutcomeInput{Decision: "OK", Actions: []string{"Envoyer|Marie|"}})
	press(t, m, "2")
	if m.view != vActions || len(m.actions) != 1 {
		t.Fatalf("2: view %d, %d actions", m.view, len(m.actions))
	}
	press(t, m, "enter")
	if m.view != vItem || m.item == nil || m.item.ID != "RDIR-1" {
		t.Fatalf("enter opens the action's item: view %d", m.view)
	}
	press(t, m, "h") // h goes back, like esc
	if m.view != vActions {
		t.Fatalf("h goes back to the actions: view %d", m.view)
	}
	press(t, m, "e")
	if len(m.actions) != 0 {
		t.Fatal("e marks the action done")
	}
	press(t, m, "esc")
	if m.view == vActions {
		t.Fatal("esc leaves the actions")
	}
	press(t, m, "1")
	if m.view != vMeetings {
		t.Fatalf("1: view %d", m.view)
	}
	press(t, m, "3")
	if m.view != vSittings {
		t.Fatalf("3: view %d", m.view)
	}
}

func TestCitedInPane(t *testing.T) {
	m, st, _ := setup(t)
	st.Cited = []config.Cited{
		{Title: "Tâches", Run: []string{"sh", "-c", "echo \"- Relancer Marie pour $0\"", "{ref}"}},
		{Title: "Notes", Run: []string{"true"}},
	}
	press(t, m, "enter")
	if s := screen(m); !strings.Contains(s, "·· Tâches") || !strings.Contains(s, "Relancer Marie pour oj:RDIR-1") || strings.Contains(s, "Notes") {
		t.Fatalf("pane:\n%s", s)
	}
	press(t, m, "enter")
	if s := screen(m); !strings.Contains(s, "Tâches") || !strings.Contains(s, "Relancer Marie pour oj:RDIR-1") {
		t.Fatalf("item view:\n%s", s)
	}
}
