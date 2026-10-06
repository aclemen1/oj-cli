package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func frenchCycle(t *testing.T) *Store {
	s := withRDIR(t)
	s.Render.Lang = "fr"
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Budget 2027", Owner: "Marie", Kind: "decision", Duration: "20m",
		Expected: "Approuver le projet ?", Attach: []string{"artefact://pro/01JB"}}, true, ""))
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Point RH"}, true, ""))
	return s
}

func TestAgendaVersions(t *testing.T) {
	s := frenchCycle(t)
	ch := must[*Changed](t)(s.FreezeSitting("RDIR", false))
	if !strings.HasSuffix(ch.Rendered, "RDIR/rendered/2026-10-08-agenda-v1.md") {
		t.Fatalf("rendered %s", ch.Rendered)
	}
	md := read(t, ch.Rendered)
	for _, want := range []string{
		"# Ordre du jour — Séance de direction",
		"**jeudi 8 octobre 2026 · 09:00**",
		"## 1. Budget 2027", "09:00 · 20 min · décision · Marie", "Question : Approuver le projet ?",
		"Pièces : artefact://pro/01JB", "## 2. Point RH", "09:20 · 10 min · discussion",
		"Durée prévue : 30 min / 1 h 30",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("agenda lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "Projet") || strings.Contains(md, "\n\n\n") {
		t.Fatalf("final agenda marked as draft or with blank runs:\n%s", md)
	}
	must[*Sitting](t)(s.ReopenSitting("RDIR-2026-10-08"))
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Divers"}, true, "RDIR-2026-10-08"))
	ch = must[*Changed](t)(s.FreezeSitting("RDIR-2026-10-08", false))
	if !strings.HasSuffix(ch.Rendered, "-agenda-v2.md") || !strings.Contains(read(t, ch.Rendered), "## 3. Divers") {
		t.Fatalf("second version %s", ch.Rendered)
	}
	if !strings.Contains(read(t, strings.Replace(ch.Rendered, "v2", "v1", 1)), "Point RH") {
		t.Fatal("v1 must stay as it was")
	}
	// render reads the latest final version.
	r := must[*Rendered](t)(s.RenderDoc("RDIR-2026-10-08", "agenda", "md", ""))
	if !r.Final || r.Path != ch.Rendered {
		t.Fatalf("render of a frozen agenda %+v", r)
	}
}

func TestMinutesRender(t *testing.T) {
	s := frenchCycle(t)
	must[*Changed](t)(s.FreezeSitting("RDIR", false))
	must[*Sitting](t)(s.HoldSitting("RDIR-2026-10-08", []string{"Marie", "Paul"}, []string{"Anne"}))
	must[*Item](t)(s.SetOutcome("RDIR-1", "", OutcomeInput{Summary: "Présenté par Marie.", Decision: "Projet approuvé.",
		Actions: []string{"Transmettre au rectorat|Marie|2026-10-20", "Informer|Paul|"}}))
	draft := must[*Rendered](t)(s.RenderDoc("RDIR-2026-10-08", "minutes", "md", ""))
	if draft.Final || !strings.Contains(read(t, draft.Path), "*Projet*") || strings.HasPrefix(draft.Path, s.Root) {
		t.Fatalf("draft minutes %+v", draft)
	}
	mn := must[*Minuted](t)(s.MinuteSitting("RDIR-2026-10-08"))
	md := read(t, mn.Rendered)
	for _, want := range []string{
		"# Procès-verbal — Séance de direction", "Présents : Marie, Paul", "Excusés : Anne",
		"Présenté par Marie.", "**Décision :** Projet approuvé.", "- Transmettre au rectorat — Marie — 20.10.2026",
		"## 2. Point RH", "*Reporté à la prochaine séance.*",
		"| Action | Qui | Échéance | Point |", "| Transmettre au rectorat | Marie | 20.10.2026 | 1 |",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("minutes lack %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "*Projet*") {
		t.Fatal("approved minutes marked as draft")
	}
	html := must[*Rendered](t)(s.RenderDoc("RDIR-2026-10-08", "minutes", "html", filepath.Join(t.TempDir(), "pv.html")))
	if h := read(t, html.Path); !strings.Contains(h, "<h1>Procès-verbal") || !strings.Contains(h, "<table>") || !strings.Contains(h, `lang="fr"`) {
		t.Fatalf("html:\n%s", h)
	}
}

func TestEnglishAndCustomTemplate(t *testing.T) {
	s := withRDIR(t)
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Budget"}, true, ""))
	r := must[*Rendered](t)(s.RenderDoc("RDIR", "agenda", "md", ""))
	if md := read(t, r.Path); !strings.Contains(md, "# Agenda — Séance de direction") || !strings.Contains(md, "Thursday 8 October 2026") || !strings.Contains(md, "*Draft*") {
		t.Fatalf("english draft:\n%s", md)
	}
	tpl := filepath.Join(t.TempDir(), "agenda.tmpl")
	os.WriteFile(tpl, []byte("ODJ {{.Meeting.Alias}} {{range .Items}}[{{.N}} {{.Item.Title}}]{{end}}\n"), 0o644)
	s.Render.Templates = map[string]string{"agenda": tpl}
	r = must[*Rendered](t)(s.RenderDoc("RDIR", "agenda", "md", ""))
	if md := read(t, r.Path); md != "ODJ RDIR [1 Budget]\n" {
		t.Fatalf("custom template: %q", md)
	}
	if _, err := s.RenderDoc("RDIR", "agenda", "odt", ""); kind(err) != "user_error" {
		t.Fatalf("unknown format: %v", err)
	}
}

func TestPandocFormats(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc not installed")
	}
	s := frenchCycle(t)
	must[*Changed](t)(s.FreezeSitting("RDIR", false))
	r := must[*Rendered](t)(s.RenderDoc("RDIR-2026-10-08", "agenda", "docx", ""))
	if !r.Final || !strings.HasSuffix(r.Path, "2026-10-08-agenda-v1.docx") || !strings.HasPrefix(r.Path, s.Root) {
		t.Fatalf("docx %+v", r)
	}
	if st, err := os.Stat(r.Path); err != nil || st.Size() < 1000 {
		t.Fatalf("docx file: %v", err)
	}
	if _, err := exec.LookPath("xelatex"); err != nil {
		t.Skip("xelatex not installed")
	}
	p := must[*Rendered](t)(s.RenderDoc("RDIR-2026-10-08", "agenda", "pdf", filepath.Join(t.TempDir(), "odj.pdf")))
	if b := read(t, p.Path); !strings.HasPrefix(b, "%PDF") {
		t.Fatal("not a PDF")
	}
}

func TestFreezeRendersSphereFormats(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc not installed")
	}
	s := frenchCycle(t)
	s.Render.Formats = []string{"docx", "html"}
	ch := must[*Changed](t)(s.FreezeSitting("RDIR", false))
	base := strings.TrimSuffix(ch.Rendered, ".md")
	for _, ext := range []string{".docx", ".html"} {
		if st, err := os.Stat(base + ext); err != nil || st.Size() == 0 {
			t.Fatalf("%s not rendered at freeze: %v", ext, err)
		}
	}
	// A broken pandoc warns and the freeze stands.
	var warns []string
	s.Warn = func(m string) { warns = append(warns, m) }
	s.Tools.Pandoc = "/nonexistent/pandoc"
	must[*Sitting](t)(s.ReopenSitting("RDIR-2026-10-08"))
	must[*Item](t)(s.AddItem("RDIR", ItemInput{Title: "Divers"}, true, "RDIR-2026-10-08"))
	ch = must[*Changed](t)(s.FreezeSitting("RDIR-2026-10-08", false))
	if ch.Sitting.State != "frozen" || len(warns) != 1 || !strings.Contains(warns[0], "docx") {
		t.Fatalf("broken pandoc: %v, %v", ch.Sitting.State, warns)
	}
}

func TestSphereConverter(t *testing.T) {
	s := frenchCycle(t)
	s.Render.Converters = map[string][]string{"docx": {"sh", "-c", `cp "$0" "$1"`, "{in}", "{out}"}}
	s.Render.Formats = []string{"docx"}
	ch := must[*Changed](t)(s.FreezeSitting("RDIR", false))
	docx := strings.TrimSuffix(ch.Rendered, ".md") + ".docx"
	if read(t, docx) != read(t, ch.Rendered) {
		t.Fatal("the sphere's converter did not make the docx")
	}
	s.Render.Converters["docx"] = []string{"true"}
	out := filepath.Join(t.TempDir(), "x.docx")
	if _, err := s.RenderDoc("RDIR-2026-10-08", "agenda", "docx", out); err == nil || !strings.Contains(err.Error(), "wrote no") {
		t.Fatalf("a converter that writes nothing: %v", err)
	}
}
