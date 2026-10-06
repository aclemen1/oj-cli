package store

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"

	"github.com/aclemen1/oj-cli/internal/spec"
)

//go:embed templates/*.tmpl
var builtin embed.FS

var Docs = []string{"agenda", "minutes"}
var Formats = []string{"md", "html", "docx", "pdf"}

// DocItem is one item of a rendered document.
type DocItem struct {
	N        int
	Start    string
	Item     *Item
	Outcome  *Outcome
	Deferred bool
}

// DocAction is one action of the minutes, with the number of its item.
type DocAction struct {
	ActionItem
	N int
}

// Doc is what a template sees.
type Doc struct {
	Kind, Lang string
	Meeting    *Meeting
	Sitting    *Sitting
	Date       string
	Items      []DocItem
	Actions    []DocAction
	Planned    string
	Final      bool
	Version    int
}

var labels = map[string]map[string]string{
	"fr": {
		"Agenda": "Ordre du jour", "Minutes": "Procès-verbal", "Draft": "Projet",
		"Question:": "Question :", "Attachments:": "Pièces :", "No item on the agenda.": "Aucun point à l'ordre du jour.",
		"Planned time:": "Durée prévue :", "Present:": "Présents :", "Excused:": "Excusés :", "Decision:": "Décision :",
		"Deferred to the next sitting.": "Reporté à la prochaine séance.", "Actions": "Actions", "Action": "Action",
		"Who": "Qui", "Due": "Échéance", "Item": "Point",
		"info": "information", "discussion": "discussion", "decision": "décision",
	},
}

var (
	weekdaysFR = []string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}
	monthsFR   = []string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}
)

func longDate(day, lang string) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return day
	}
	if lang == "fr" {
		d := strconv.Itoa(t.Day())
		if t.Day() == 1 {
			d = "1er"
		}
		return fmt.Sprintf("%s %s %s %d", weekdaysFR[t.Weekday()], d, monthsFR[t.Month()-1], t.Year())
	}
	return t.Format("Monday 2 January 2006")
}

func funcs(lang string) template.FuncMap {
	tr := func(s string) string {
		if v, ok := labels[lang][s]; ok {
			return v
		}
		return s
	}
	return template.FuncMap{
		"t":    tr,
		"kind": tr,
		"join": strings.Join,
		"cell": func(s string) string { return strings.ReplaceAll(s, "|", `\|`) },
		"dur": func(d string) string {
			v, err := time.ParseDuration(d)
			if err != nil || d == "" {
				return d
			}
			h, m := int(v.Hours()), int(v.Minutes())%60
			switch {
			case h > 0 && m > 0:
				return fmt.Sprintf("%d h %02d", h, m)
			case h > 0:
				return fmt.Sprintf("%d h", h)
			}
			return fmt.Sprintf("%d min", m)
		},
		"date": func(d string) string {
			t, err := time.Parse("2006-01-02", d)
			if err != nil || lang != "fr" {
				return d
			}
			return t.Format("02.01.2006")
		},
	}
}

func (s *Store) lang(m *Meeting) string {
	switch {
	case m.Lang != "":
		return m.Lang
	case s.Render.Lang != "":
		return s.Render.Lang
	}
	return "en"
}

func (s *Store) template(kind, lang string) (*template.Template, error) {
	t := template.New(kind).Funcs(funcs(lang))
	if p := s.Render.Templates[kind]; p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, spec.UserError("template for %s: %v", kind, err)
		}
		return t.Parse(string(b))
	}
	b, err := builtin.ReadFile("templates/" + kind + ".md.tmpl")
	if err != nil {
		return nil, err
	}
	return t.Parse(string(b))
}

// doc gathers what the template of kind needs for a sitting.
func (s *Store) doc(sit *Sitting, m *Meeting, kind string, final bool) (*Doc, error) {
	on, _, err := s.agendaItems(sit)
	if err != nil {
		return nil, err
	}
	lang := s.lang(m)
	d := &Doc{Kind: kind, Lang: lang, Meeting: m, Sitting: sit, Date: longDate(sit.Date, lang), Final: final}
	var clock time.Time
	if sit.Time != "" {
		clock, _ = time.Parse("15:04", sit.Time)
	}
	var total time.Duration
	for i, it := range on {
		start := FormatDuration(total)
		if sit.Time != "" {
			start = clock.Add(total).Format("15:04")
		}
		di := DocItem{N: i + 1, Start: start, Item: it}
		if e := it.entry(sit.ID); e != nil {
			di.Outcome = e.Outcome
			di.Deferred = e.Result == "deferred" || (e.Result == "" && (e.Outcome == nil || e.Outcome.Next == "deferred"))
		} else {
			di.Deferred = kind == "minutes"
		}
		if di.Outcome != nil {
			for _, a := range di.Outcome.Actions {
				d.Actions = append(d.Actions, DocAction{ActionItem: a, N: di.N})
			}
		}
		d.Items = append(d.Items, di)
		total += minutes(it.Duration)
	}
	sort.SliceStable(d.Actions, func(i, j int) bool {
		a, b := d.Actions[i].Due, d.Actions[j].Due
		return a != "" && (b == "" || a < b)
	})
	d.Planned = FormatDuration(total)
	return d, nil
}

func (s *Store) execute(d *Doc) ([]byte, error) {
	t, err := s.template(d.Kind, d.Lang)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, d); err != nil {
		return nil, spec.UserError("template for %s: %v", d.Kind, err)
	}
	return collapse(buf.Bytes()), nil
}

var blankRuns = regexp.MustCompile(`\n{3,}`)

func collapse(b []byte) []byte {
	return append(bytes.TrimSpace(blankRuns.ReplaceAll(b, []byte("\n\n"))), '\n')
}

func (s *Store) renderedDir(alias string) string {
	return filepath.Join(s.meetingDir(alias), "rendered")
}

func sittingKey(sit *Sitting) string { return strings.TrimPrefix(sit.ID, sit.Meeting+"-") }

// latestAgenda returns the last frozen version of an agenda, or 0.
func (s *Store) latestAgenda(sit *Sitting) (string, int) {
	l, _ := filepath.Glob(filepath.Join(s.renderedDir(sit.Meeting), sittingKey(sit)+"-agenda-v*.md"))
	best, path := 0, ""
	for _, p := range l {
		v, err := strconv.Atoi(strings.TrimSuffix(p[strings.LastIndex(p, "-v")+2:], ".md"))
		if err == nil && v > best {
			best, path = v, p
		}
	}
	return path, best
}

// final is what renderFinal wrote: the Markdown, every file of the document
// (the Markdown and the sphere's formats), and whether an agenda identical to
// the latest version was kept instead of a new version.
type final struct {
	Path      string
	Files     []string
	Unchanged bool
}

// renderFinal writes the final Markdown of a document into the store, under
// a lock the caller holds: a new agenda version (or the latest one when the
// agenda did not change), or the minutes; then the sphere's formats.
func (s *Store) renderFinal(sit *Sitting, m *Meeting, kind string) (*final, error) {
	d, err := s.doc(sit, m, kind, true)
	if err != nil {
		return nil, err
	}
	name := sittingKey(sit) + "-minutes.md"
	latest := ""
	if kind == "agenda" {
		var v int
		latest, v = s.latestAgenda(sit)
		d.Version = v + 1
		name = fmt.Sprintf("%s-agenda-v%d.md", sittingKey(sit), d.Version)
	}
	b, err := s.execute(d)
	if err != nil {
		return nil, err
	}
	out := &final{Path: filepath.Join(s.renderedDir(sit.Meeting), name)}
	if prev, err := os.ReadFile(latest); latest != "" && err == nil && bytes.Equal(prev, b) {
		out.Path, out.Unchanged = latest, true
	} else {
		if err := os.MkdirAll(filepath.Dir(out.Path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(out.Path, b, 0o644); err != nil {
			return nil, err
		}
	}
	out.Files = []string{out.Path}
	// The sphere's other formats: a failure is a warning, the Markdown stands.
	for _, f := range s.Render.Formats {
		if f == "md" || !contains(Formats, f) {
			continue
		}
		dest := strings.TrimSuffix(out.Path, ".md") + "." + f
		if out.Unchanged && fileExists(dest) {
			out.Files = append(out.Files, dest)
			continue
		}
		if err := s.convert(out.Path, f, dest, s.lang(m)); err != nil {
			s.warn(fmt.Sprintf("%s of %s not rendered: %v", f, filepath.Base(out.Path), err))
			continue
		}
		out.Files = append(out.Files, dest)
	}
	return out, nil
}

type Rendered struct {
	Doc      string `json:"doc"`
	Sitting  string `json:"sitting"`
	Format   string `json:"format"`
	Path     string `json:"path"`
	Markdown string `json:"markdown"`
	Final    bool   `json:"final"`
}

// RenderDoc renders the agenda or the minutes of a sitting. A frozen agenda
// and approved minutes come from their final Markdown in the store; anything
// else is a draft, written to out or to a temporary directory.
func (s *Store) RenderDoc(arg, kind, to, out string) (*Rendered, error) {
	if !contains(Docs, kind) {
		return nil, spec.UserError("--doc takes agenda or minutes, got %q", kind)
	}
	if to == "" {
		to = "md"
	}
	if !contains(Formats, to) {
		return nil, spec.UserError("--to takes md, html, docx or pdf, got %q", to)
	}
	sit, m, err := s.Resolve(arg)
	if err != nil {
		return nil, err
	}
	r := &Rendered{Doc: kind, Sitting: sit.ID, Format: to}
	var md string
	switch {
	case kind == "agenda" && sit.State != "planned" && sit.State != "cancelled":
		md, _ = s.latestAgenda(sit)
	case kind == "minutes" && sit.State == "minuted":
		if p := filepath.Join(s.renderedDir(sit.Meeting), sittingKey(sit)+"-minutes.md"); fileExists(p) {
			md = p
		}
	}
	base := ""
	if md != "" {
		r.Final = true
		base = strings.TrimSuffix(md, ".md")
	} else {
		d, err := s.doc(sit, m, kind, false)
		if err != nil {
			return nil, err
		}
		b, err := s.execute(d)
		if err != nil {
			return nil, err
		}
		dir := filepath.Join(os.TempDir(), "oj", s.Sphere)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		base = filepath.Join(dir, sit.ID+"-"+kind+"-draft")
		md = base + ".md"
		if err := os.WriteFile(md, b, 0o600); err != nil {
			return nil, err
		}
	}
	r.Markdown = md
	dest := out
	if dest == "" {
		dest = base + "." + to
	}
	if to == "md" && dest == md {
		r.Path = md
		return r, nil
	}
	convert := func() error { return s.convert(md, to, dest, s.lang(m)) }
	if r.Final && out == "" && to != "md" {
		err = s.Write(fmt.Sprintf("render %s %s %s", sit.ID, kind, to), convert)
	} else {
		err = convert()
	}
	if err != nil {
		return nil, err
	}
	r.Path = dest
	return r, nil
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// convert turns a Markdown file into another format, with the sphere's
// converter for that format when it has one.
func (s *Store) convert(md, to, dest, lang string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if conv := s.Render.Converters[to]; len(conv) > 0 {
		if _, err := s.run(conv, map[string]string{"{in}": md, "{out}": dest, "{lang}": lang}); err != nil {
			return fmt.Errorf("%s converter: %w", to, err)
		}
		if _, err := os.Stat(dest); err != nil {
			return fmt.Errorf("%s converter wrote no %s", to, dest)
		}
		return nil
	}
	src, err := os.ReadFile(md)
	if err != nil {
		return err
	}
	switch to {
	case "md":
		return os.WriteFile(dest, src, 0o644)
	case "html":
		var body bytes.Buffer
		if err := goldmark.New(goldmark.WithExtensions(extension.Table)).Convert(src, &body); err != nil {
			return err
		}
		title := strings.TrimPrefix(strings.SplitN(string(src), "\n", 2)[0], "# ")
		page := fmt.Sprintf("<!doctype html>\n<html lang=\"%s\"><head><meta charset=\"utf-8\"><title>%s</title>\n"+
			"<style>body{font-family:system-ui,sans-serif;max-width:46rem;margin:2rem auto;padding:0 1rem;line-height:1.5}"+
			"table{border-collapse:collapse}td,th{border:1px solid #ccc;padding:.3rem .6rem}</style></head>\n<body>\n%s</body></html>\n",
			lang, template.HTMLEscapeString(title), body.String())
		return os.WriteFile(dest, []byte(page), 0o644)
	}
	pandoc := s.Tools.Pandoc
	if pandoc == "" {
		pandoc = "pandoc"
	}
	if _, err := exec.LookPath(pandoc); err != nil {
		return spec.UserError("--to %s needs pandoc, not found as %q; install it or set render.pandoc in the configuration", to, pandoc)
	}
	args := []string{"-f", "markdown", "-o", dest, "-V", "lang=" + lang, md}
	if to == "docx" && s.Render.ReferenceDoc != "" {
		args = append(args, "--reference-doc", s.Render.ReferenceDoc)
	}
	if to == "pdf" {
		engine := s.Tools.PDFEngine
		if engine == "" {
			engine = "xelatex"
		}
		args = append(args, "--pdf-engine", engine, "-V", "geometry:margin=2.5cm")
	}
	if out, err := exec.Command(pandoc, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("pandoc: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
