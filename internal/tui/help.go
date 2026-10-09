package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/aclemen1/oj-cli/internal/store"
)

// The help panel says where the user is and what can be done next, for the
// view, the sitting's state and the selected item's state. It is open by
// default; ? hides or shows it. French for a sphere rendered in French.

func (m *model) fr() bool { return m.st.Render.Lang == "fr" }

// tr picks the French or the English text.
func (m *model) tr(fr, en string) string {
	if m.fr() {
		return fr
	}
	return en
}

type helpBuilder struct {
	m     *model
	w     int
	lines []string
}

func (b *helpBuilder) title(s string) {
	if len(b.lines) > 0 {
		b.lines = append(b.lines, "")
	}
	b.lines = append(b.lines, sSection.Render(s))
}

func (b *helpBuilder) text(s string) {
	b.lines = append(b.lines, strings.Split(ansi.Wordwrap(s, b.w, ""), "\n")...)
}

// key adds a key and what it does; the text wraps under itself.
func (b *helpBuilder) key(k, what string) {
	kw := 6
	wrapped := strings.Split(ansi.Wordwrap(what, max(10, b.w-kw), ""), "\n")
	for i, l := range wrapped {
		head := strings.Repeat(" ", kw)
		if i == 0 {
			head = pad(sKey.Render(k), kw)
		}
		b.lines = append(b.lines, head+l)
	}
}

// flow draws the sitting's states with the current one marked.
func (b *helpBuilder) flow(state string) {
	steps := []string{"planned", "frozen", "held", "minuted"}
	var parts []string
	for _, s := range steps {
		if s == state {
			parts = append(parts, sWarn.Render("● "+s))
		} else {
			parts = append(parts, sMuted.Render(s))
		}
	}
	line := strings.Join(parts, sMuted.Render(" → "))
	if state == "cancelled" {
		line = sMuted.Render(strings.Join(steps, " → ")) + "  " + sWarn.Render("● cancelled")
	}
	b.lines = append(b.lines, strings.Split(ansi.Wordwrap(line, b.w, ""), "\n")...)
}

func (m *model) helpPanel(w int) []string {
	b := &helpBuilder{m: m, w: w}
	if m.moving != nil {
		b.title(m.tr("OÙ VOUS EN ÊTES", "WHERE YOU ARE"))
		b.text(m.tr("Choix de la séance où placer "+m.moving.ID+". Seules les séances planifiées (non figées) sont proposées.",
			"Choosing the sitting for "+m.moving.ID+". Only planned (not frozen) sittings are listed."))
		b.text(m.tr("Le point garde son état ; un point reporté redevient retenu.", "The item keeps its state; a deferred item becomes accepted."))
		b.title(m.tr("OPTIONS", "OPTIONS"))
		b.key("enter", m.tr("déplacer sur la séance choisie", "move to the chosen sitting"))
		b.key("j k", m.tr("choisir", "choose"))
		b.key("esc", m.tr("annuler", "cancel"))
		return b.lines
	}
	switch m.view {
	case vMeetings:
		m.helpMeetings(b)
	case vAgenda:
		m.helpAgenda(b)
	case vLive:
		m.helpLive(b)
	case vItem:
		m.helpItem(b)
	case vActions:
		m.helpActions(b)
	case vSittings:
		m.helpSittings(b)
	case vStanding:
		m.helpStanding(b)
	case vDoc:
		m.helpDoc(b)
	}
	b.title(m.tr("AIDE", "HELP"))
	b.key("?", m.tr("masquer ou afficher cette aide", "hide or show this help"))
	b.key("esc", m.tr("revenir à la vue précédente", "back to the previous view"))
	b.key("q", m.tr("quitter (l'état est gardé pour le prochain lancement)", "quit (the state is kept for the next start)"))
	return b.lines
}

func (m *model) helpMeetings(b *helpBuilder) {
	b.title(m.tr("OÙ VOUS EN ÊTES", "WHERE YOU ARE"))
	b.text(m.tr("La liste de vos séances. Chacune a des occurrences (une par date), avec leur ordre du jour.",
		"Your meetings. Each has sittings (one per date), each with its agenda."))
	b.title(m.tr("LE CYCLE D'UNE SÉANCE", "A SITTING'S CYCLE"))
	b.flow("")
	b.text(m.tr("On prépare l'ordre du jour (planned), on le fige (frozen), la séance a lieu (held), on approuve le PV (minuted).",
		"Prepare the agenda (planned), freeze it (frozen), the sitting takes place (held), approve the minutes (minuted)."))
	b.title(m.tr("OPTIONS", "OPTIONS"))
	b.key("enter", m.tr("ouvrir l'ordre du jour de la prochaine occurrence", "open the agenda of the next sitting"))
	b.key("S", m.tr("toutes les occurrences, avec leurs points", "every sitting, with its items"))
	b.key("A", m.tr("les actions décidées en séance", "actions decided in sittings"))
	b.key("R", m.tr("rafraîchir", "refresh"))
	m.helpFilter(b)
}

// helpFilter explains s when several spheres are shown.
func (m *model) helpFilter(b *helpBuilder) {
	if !m.multi() {
		return
	}
	cur := m.tr("toutes", "all")
	if m.filter != "" {
		cur = m.filter
	}
	b.key("s", m.tr("sphère montrée : toutes, puis chacune tour à tour (actuelle : "+cur+")",
		"sphere shown: all, then each in turn (now: "+cur+")"))
}

func (m *model) sittingStateText(s *store.Sitting, a *store.Agenda) (what, next string) {
	switch s.State {
	case "planned":
		what = m.tr("Ordre du jour ouvert : on ajoute, accepte, ordonne et minute les points.",
			"The agenda is open: add, accept, order and time items.")
		next = m.tr("Trancher les points proposés (a ou x), puis figer l'ordre du jour (f).",
			"Decide on the proposed items (a or x), then freeze the agenda (f).")
		if len(a.Proposed) == 0 {
			next = m.tr("Figer l'ordre du jour (f) quand il est prêt ; il est alors rendu (Markdown, docx).",
				"Freeze the agenda (f) when it is ready; it is then rendered (Markdown, docx).")
		}
	case "frozen":
		what = m.tr("Ordre du jour arrêté et rendu. On n'y ajoute plus rien.",
			"The agenda is final and rendered. Nothing more goes in.")
		next = m.tr("Le jour de la séance : l (en direct) pour noter les issues, puis h (tenue). Pour corriger : r (rouvrir).",
			"On the day: l (live) to note outcomes, then h (held). To correct it: r (reopen).")
	case "held":
		what = m.tr("La séance a eu lieu. On finit de saisir les issues.", "The sitting took place. Finish the outcomes.")
		next = m.tr("Compléter les issues (l, puis s, D, t, ou G pour les demander à l'agent de la séance), relire le projet de PV (P), puis l'approuver (m). Les issues ✎ d'un agent sont des brouillons.",
			"Complete the outcomes (l, then s, D, t, or G to ask the meeting's agent), review the draft minutes (P), then approve them (m). Outcomes ✎ by an agent are drafts.")
	case "minuted":
		what = m.tr("PV approuvé et rendu. Les points traités sont faits ; les autres sont reportés à la séance suivante, sauf les points récurrents, retirés.",
			"Minutes approved and rendered. Items dealt with are done; the others moved to the next sitting, except recurring items, dropped.")
		next = m.tr("Suivre les actions (A). Pour revenir en arrière : U (annuler le PV).",
			"Follow the actions (A). To go back: U (take back the minutes).")
	case "cancelled":
		what = m.tr("Séance annulée ; ses points sont passés à la séance suivante.",
			"Sitting cancelled; its items moved to the next sitting.")
		next = m.tr("U pour la rétablir (les points déplacés restent où ils sont).",
			"U to restore it (moved items stay where they went).")
	}
	return what, next
}

func (m *model) itemStateText(r *row) string {
	it := r.item
	switch {
	case r.away:
		return m.tr(fmt.Sprintf("Reporté d'ici à %s. u le ramène sur cette séance.", orDash(it.Sitting)),
			fmt.Sprintf("Deferred from here to %s. u brings it back to this sitting.", orDash(it.Sitting)))
	case it.State == "proposed":
		return m.tr("Proposé : quelqu'un le demande, il n'est pas encore retenu.", "Proposed: someone asks for it; not on the agenda yet.")
	case it.State == "accepted":
		return m.tr("Retenu à l'ordre du jour de cette séance.", "On the agenda of this sitting.")
	case it.State == "deferred":
		return m.tr("Reporté d'une séance précédente ; il figure à l'ordre du jour.", "Deferred from an earlier sitting; it is on the agenda.")
	case it.State == "done":
		return m.tr("Traité : son issue est approuvée.", "Done: its outcome is approved.")
	case it.State == "dropped":
		return m.tr("Retiré : "+it.Reason, "Dropped: "+it.Reason)
	}
	return it.State
}

func (m *model) helpAgenda(b *helpBuilder) {
	if m.agenda == nil {
		return
	}
	s := m.agenda.Sitting
	what, next := m.sittingStateText(s, m.agenda)
	b.title(m.tr("OÙ VOUS EN ÊTES", "WHERE YOU ARE"))
	b.text(sBold.Render(s.ID))
	b.flow(s.State)
	b.text(what)
	if next != "" {
		b.title(m.tr("ÉTAPE SUIVANTE", "NEXT STEP"))
		b.text(next)
	}
	b.title(m.tr("SÉANCE", "SITTING"))
	switch s.State {
	case "planned":
		b.key("n", m.tr("nouveau point (retenu)", "new item (accepted)"))
		b.key("f", m.tr("figer l'ordre du jour", "freeze the agenda"))
		b.key("h", m.tr("marquer la séance tenue", "mark the sitting held"))
		b.key("l", m.tr("séance en direct", "live sitting"))
	case "frozen":
		b.key("l", m.tr("séance en direct", "live sitting"))
		b.key("h", m.tr("marquer la séance tenue", "mark the sitting held"))
		b.key("r", m.tr("rouvrir l'ordre du jour", "reopen the agenda"))
	case "held":
		b.key("l", m.tr("saisir les issues en direct", "record outcomes live"))
		m.helpAsk(b)
		b.key("m", m.tr("approuver le PV (taper yes)", "approve the minutes (type yes)"))
	}
	if docKind(s) == "minutes" {
		b.key("P", m.tr("voir le PV tel qu'il sera produit", "see the minutes as they will be produced"))
	} else {
		b.key("P", m.tr("voir l'ordre du jour tel qu'il sera produit", "see the agenda as it will be produced"))
	}
	if u := undoSittingLabel(m.agenda); u != "" {
		b.key("U", u)
	}
	if r := m.current(); r != nil {
		b.title(m.tr("POINT", "ITEM") + " · " + idLabel(r.item))
		b.text(m.itemStateText(r))
		if k := r.item.StandingKey(); k != "" {
			if r.item.Virtual {
				b.text(m.tr("Point récurrent ("+k+") pas encore écrit : il l'est au premier geste, au gel ou à la tenue.",
					"Recurring item ("+k+") not written yet: it is at the first gesture, at freeze or hold."))
			} else {
				b.text(m.tr("Exemplaire du point récurrent "+k+" pour cette séance ; non traité, il est retiré au PV, pas reporté.",
					"This sitting's instance of the recurring item "+k+"; not dealt with, it is dropped at the minutes, not deferred."))
			}
		}
		if r.outcome != nil && r.outcome.Status == "draft" {
			b.text(sWarn.Render(m.tr("Issue rédigée par un agent : brouillon jusqu'au PV.", "Outcome written by an agent: a draft until the minutes.")))
		}
		open := r.item.State == "proposed" || r.item.State == "accepted" || r.item.State == "deferred"
		if r.item.State == "proposed" {
			b.key("a", m.tr("le retenir", "accept it"))
		}
		if open && !r.away {
			b.key("d", m.tr("le reporter à la séance suivante", "defer it to the next sitting"))
			b.key("M", m.tr("le déplacer sur une autre séance (il garde son état)", "move it to another sitting (it keeps its state)"))
			b.key("x", m.tr("le retirer (avec une raison)", "drop it (with a reason)"))
		}
		if u := m.undoLabel(); u != "" {
			b.key("u", u)
		}
		if open && !r.proposed && !r.away && s.State == "planned" {
			b.key("J/K", m.tr("le descendre / le monter", "move it down / up"))
		}
		if open {
			b.key("+/-", m.tr("5 minutes de plus / de moins", "5 minutes more / less"))
		}
		b.key("e", m.tr("corriger la fiche (titre, question, durée…)", "edit the item (title, question, duration…)"))
		b.key("N", m.tr("ajouter aux notes (fin de fichier, en insertion)", "add to the notes (end of file, insert mode)"))
		if r.item.StandingKey() == "" && !r.proposed {
			b.key("*", m.tr("le rendre récurrent (il reviendra à chaque séance)", "make it recurring (it comes back at every sitting)"))
		}
		if ref := m.jumpRef(r.item); ref != "" {
			b.key("o", m.tr("aller à "+ref, "go to "+ref))
		} else if len(m.st.CreateSchemes()) > 0 {
			b.key("c", m.tr("ouvrir un dossier pour ce point", "create a target (e.g. a dossier) for it"))
		}
		b.key("enter", m.tr("voir le point en entier", "see the whole item"))
	}
	b.title(m.tr("NAVIGUER", "NAVIGATE"))
	b.key("[ ]", m.tr("séance précédente / suivante", "previous / next sitting"))
	b.key("S", m.tr("toutes les séances de cette série", "every sitting of this meeting"))
	b.key("R", m.tr("les points récurrents de cette série", "the recurring items of this meeting"))
	b.key("A", m.tr("actions décidées", "decided actions"))
	b.key("esc", m.tr("retour aux séances", "back to meetings"))
}

func (m *model) helpLive(b *helpBuilder) {
	b.title(m.tr("OÙ VOUS EN ÊTES", "WHERE YOU ARE"))
	if m.agenda != nil {
		b.text(sBold.Render(m.agenda.Sitting.ID))
		b.flow(m.agenda.Sitting.State)
	}
	b.text(m.tr("Séance en direct : un point à la fois, chronométré. Chaque issue s'enregistre aussitôt.",
		"Live sitting: one item at a time, timed. Each outcome is saved at once."))
	b.title(m.tr("ÉTAPE SUIVANTE", "NEXT STEP"))
	b.text(m.tr("Pour chaque point : résumé (s), décision (D), actions (t), ou report (-). Puis n. À la fin : h (tenue).",
		"For each item: summary (s), decision (D), actions (t), or defer (-). Then n. At the end: h (held)."))
	b.title(m.tr("OPTIONS", "OPTIONS"))
	b.key("space", m.tr("lancer / arrêter le chronomètre", "start / stop the timer"))
	b.key("n p", m.tr("point suivant / précédent", "next / previous item"))
	b.key("s", m.tr("résumé du point", "summary of the item"))
	b.key("D", m.tr("décision", "decision"))
	b.key("t", m.tr("action : quoi|qui|AAAA-MM-JJ|fait (fait : déjà faite)", "action: what|who|YYYY-MM-DD|done (done: already done)"))
	if it := m.liveItem(); it != nil && it.StandingKey() != "" {
		b.key("-", m.tr("non traité : retiré au PV (point récurrent)", "not reached: dropped at the minutes (recurring item)"))
	} else {
		b.key("-", m.tr("non traité : reporté au PV", "not reached: deferred at the minutes"))
	}
	if it := m.liveItem(); it != nil {
		if ref := m.jumpRef(it); ref != "" {
			b.key("o", m.tr("aller à "+ref, "go to "+ref))
		}
	}
	b.key("h", m.tr("la séance est tenue", "the sitting is held"))
	b.key("esc", m.tr("retour à l'ordre du jour", "back to the agenda"))
}

func (m *model) helpItem(b *helpBuilder) {
	b.title(m.tr("OÙ VOUS EN ÊTES", "WHERE YOU ARE"))
	b.text(m.tr("Le point en entier : champs, notes, historique des séances, contenu du dossier lié, journal.",
		"The whole item: fields, notes, history across sittings, linked dossier, log."))
	b.title(m.tr("OPTIONS", "OPTIONS"))
	b.key("j k", m.tr("défiler", "scroll"))
	if m.item != nil {
		if ref := m.jumpRef(m.item); ref != "" {
			b.key("o", m.tr("aller à "+ref, "go to "+ref))
		}
	}
	b.key("e", m.tr("corriger la fiche dans $EDITOR", "edit the item in $EDITOR"))
	b.key("N", m.tr("ajouter aux notes (fin de fichier, en insertion)", "add to the notes (end of file, insert mode)"))
	b.key("esc", m.tr("retour", "back"))
}

func (m *model) helpActions(b *helpBuilder) {
	b.title(m.tr("OÙ VOUS EN ÊTES", "WHERE YOU ARE"))
	b.text(m.tr("Les actions décidées en séance, par échéance ; en rouge, celles qui sont en retard.",
		"Actions decided in sittings, by due date; overdue ones in red."))
	b.title(m.tr("OPTIONS", "OPTIONS"))
	b.key("space", m.tr("marquer faite / à faire", "mark done / open"))
	b.key("o", m.tr("montrer aussi les actions faites", "also show done actions"))
	m.helpFilter(b)
	b.key("esc", m.tr("retour", "back"))
}

func (m *model) helpDoc(b *helpBuilder) {
	b.title(m.tr("OÙ VOUS EN ÊTES", "WHERE YOU ARE"))
	b.text(m.tr("Le document tel qu'il serait produit maintenant : l'ordre du jour avant la séance, le PV une fois la séance tenue. Il se met à jour quand les points ou les issues changent.",
		"The document as it would be produced now: the agenda before the sitting, the minutes once it is held. It follows changes to items and outcomes."))
	b.title(m.tr("OPTIONS", "OPTIONS"))
	b.key("j k", m.tr("défiler", "scroll"))
	m.helpAsk(b)
	b.key("esc", m.tr("retour à l'ordre du jour", "back to the agenda"))
}

// helpAsk shows G when the sphere declares the request and the meeting can receive it.
func (m *model) helpAsk(b *helpBuilder) {
	if m.agenda != nil && m.st.CanAsk(m.agenda.Sitting.Meeting, "outcomes") {
		b.key("G", m.tr("demander à l'agent de la séance d'écrire les issues (brouillons ✎ à relire avant m)",
			"ask the meeting's agent to write the outcomes (drafts ✎ to review before m)"))
	}
}

func (m *model) helpStanding(b *helpBuilder) {
	b.title(m.tr("OÙ VOUS EN ÊTES", "WHERE YOU ARE"))
	b.text(m.tr("Les points récurrents de "+m.meeting+" : chaque séance a le sien, retenu d'office, au début ou à la fin de l'ordre du jour.",
		"The recurring items of "+m.meeting+": every sitting has its own, accepted, at the start or the end of the agenda."))
	b.text(m.tr("Dans l'ordre du jour, ↻ marque un exemplaire pas encore écrit : il l'est au premier geste, au gel ou à la tenue. Non traité, il est retiré au PV, pas reporté.",
		"In the agenda, ↻ marks an instance not written yet: it is at the first gesture, at freeze or hold. Not dealt with, it is dropped at the minutes, not deferred."))
	b.title(m.tr("OPTIONS", "OPTIONS"))
	b.key("n", m.tr("nouveau point récurrent (à la fin)", "new recurring item (at the end)"))
	b.key("s", m.tr("le mettre au début / à la fin", "put it at the start / the end"))
	b.key("x", m.tr("l'arrêter (les exemplaires déjà écrits restent)", "stop it (instances already written stay)"))
	b.key("esc", m.tr("retour à l'ordre du jour", "back to the agenda"))
}

func (m *model) helpSittings(b *helpBuilder) {
	b.title(m.tr("OÙ VOUS EN ÊTES", "WHERE YOU ARE"))
	b.text(m.tr("Les séances des 30 derniers jours et les 4 prochaines, chacune avec ses points ; puis les points sans séance.",
		"Sittings of the last 30 days and the next 4, each with its items; then items with no sitting."))
	b.title(m.tr("OPTIONS", "OPTIONS"))
	b.key("enter", m.tr("ouvrir l'ordre du jour (sur le point choisi)", "open the agenda (on the chosen item)"))
	b.key("j k", m.tr("se déplacer", "move"))
	if m.ovScope == "" {
		m.helpFilter(b)
	}
	b.key("esc", m.tr("retour", "back"))
}
