package tui

import (
	"reflect"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/aclemen1/tuikit"

	"github.com/aclemen1/oj-cli/internal/store"
)

// Every input but the filter opens in a tuikit modal, which takes every key
// while it is open; its answer goes to answer like the inline prompt did.

// openModal shows the input of p; target is the item or sitting it acts on.
func (m *model) openModal(p prompt, target, value string) tea.Cmd {
	var title string
	var content tuikit.Content
	text := func(label string, required bool) tuikit.Content {
		f := tuikit.TextArea("text", label).Default(value)
		if required {
			f = f.Required()
		}
		return tuikit.NewForm("text", f)
	}
	switch p {
	case pNewItem:
		title, content = m.tr("Nouveau point", "New item"), text(m.tr("Titre", "Title"), true)
	case pDrop:
		title, content = m.tr("Retirer ", "Drop ")+target, text(m.tr("Raison", "Reason"), true)
	case pSummary:
		title, content = m.tr("Résumé de ", "Summary of ")+target, tuikit.NewEditor("text", m.tr("Résumé", "Summary"), value)
	case pDecision:
		title, content = m.tr("Décision sur ", "Decision on ")+target, tuikit.NewEditor("text", m.tr("Décision", "Decision"), value)
	case pAction:
		title = m.tr("Action pour ", "Action for ") + target
		content = tuikit.NewForm("action",
			tuikit.Text("what", m.tr("Quoi", "What")).Required(),
			tuikit.Ref("who", m.tr("Qui", "Who"), m.people).Help(m.tr("vide : vous", "empty: you")),
			tuikit.Date("due", m.tr("Échéance", "Due")),
			tuikit.Bool("done", m.tr("Déjà faite", "Already done")),
		)
	case pMinute:
		title, content = m.tr("PV", "Minutes"), tuikit.NewConfirm("confirm", m.tr("Approuver le PV de ", "Approve the minutes of ")+target+" ?")
	case pUnminute:
		title, content = m.tr("PV", "Minutes"), tuikit.NewConfirm("confirm", m.tr("Annuler l'approbation du PV de ", "Take back the minutes of ")+target+" ?")
	case pMakeStanding:
		title = target + m.tr(" récurrent", " recurring")
		content = tuikit.NewForm("place", tuikit.Choice("place", m.tr("Place dans l'ordre du jour", "Place on the agenda"),
			m.tr("début", "start"), m.tr("fin", "end")).Default(m.tr("fin", "end")))
	case pNote:
		title, content = m.tr("Note sur ", "Note on ")+target, tuikit.NewEditor("text", m.tr("Note", "Note"), value)
	case pNewStanding:
		title, content = m.tr("Nouveau point récurrent", "New recurring item"), text(m.tr("Titre", "Title"), true)
	default:
		return nil
	}
	m.prompt, m.target = p, target
	m.modal = tuikit.NewModal(title, content).SetSize(m.w, m.h)
	return nil
}

// modalDone turns the modal's values into the prompt's answer.
func (m *model) modalDone(msg tuikit.DoneMsg) tea.Cmd {
	p, target := m.prompt, m.target
	m.prompt, m.modal = pNone, nil
	v := msg.Values
	var answer string
	switch p {
	case pMinute, pUnminute:
		answer = "yes"
	case pMakeStanding:
		answer = v.String("place")
	case pAction:
		parts := []string{clean(v.String("what")), clean(v.String("who")), ""}
		if v.Has("due") && !v.Time("due").IsZero() {
			parts[2] = v.Time("due").Format("2006-01-02")
		}
		if v.Bool("done") {
			parts = append(parts, "done")
		}
		answer = strings.Join(parts, "|")
	default:
		answer = strings.TrimSpace(v.String("text"))
	}
	return m.answer(p, answer, target)
}

// clean keeps a field of an action on one line, without the | that separates fields.
func clean(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(strings.ReplaceAll(s, "|", "/")), " "))
}

// people are the names already met in the sphere: owners, chairs, members, who of actions.
func (m *model) people(q string) []tuikit.Item {
	seen := map[string]bool{}
	add := func(n string) {
		if n = strings.TrimSpace(n); n != "" {
			seen[n] = true
		}
	}
	if ms, err := m.st.Meetings(); err == nil {
		for _, mt := range ms {
			add(mt.Chair)
			for _, p := range mt.Members {
				add(p)
			}
		}
	}
	if rows, err := m.st.Actions(store.ActionFilter{State: "all"}); err == nil {
		for _, a := range rows {
			add(a.Who)
		}
	}
	if m.agenda != nil {
		for _, ai := range m.agenda.Items {
			add(ai.Item.Owner)
		}
	}
	var out []tuikit.Item
	for n := range seen {
		if matches(q, n) {
			out = append(out, tuikit.Item{Value: n, Label: n})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
	return out
}

// ownPkg is the package of oj's own messages, which the modal never needs.
var ownPkg = reflect.TypeOf(watchMsg{}).PkgPath()
