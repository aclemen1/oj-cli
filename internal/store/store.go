// Package store keeps meetings, sittings and items as Markdown files with
// YAML frontmatter, one root per sphere.
package store

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aclemen1/oj-cli/internal/config"
	"github.com/aclemen1/oj-cli/internal/spec"
)

type Store struct {
	Sphere string
	Root   string
	VCS    string
	// By names who acts, for the logs and the outcomes.
	By string
	// Now is the clock; tests replace it.
	Now func() time.Time
	// Warn receives what went wrong after a write succeeded (a failed commit).
	Warn func(string)
	// Render and Tools set how documents are rendered.
	Render config.SphereRender
	Tools  config.Render
	// Hooks run on the events of this sphere.
	Hooks []config.Hook
	// Refs summarise the targets of item refs, by scheme.
	Refs map[string]config.RefSource
	// Asks are named requests a sitting can send to its meeting's ref target.
	Asks map[string]string
	// Cited lists what other tools hold about an item (oj item show --cited).
	Cited []config.Cited

	mu      sync.Mutex
	pending []hookEvent
}

type LogEntry struct {
	At   string `yaml:"at" json:"at"`
	By   string `yaml:"by" json:"by"`
	What string `yaml:"what" json:"what"`
}

// Open returns the store of a configured sphere.
func Open(cfg *config.Config, sphere, by string) (*Store, error) {
	s, ok := cfg.Spheres[sphere]
	if !ok {
		if len(cfg.Spheres) == 0 {
			return nil, spec.UserError("no sphere is configured in %s. Example: oj init --sphere pro --root ~/oj/pro", cfg.File())
		}
		return nil, spec.UserError("unknown sphere %q; configured: %s. Example: --sphere %s", sphere, strings.Join(cfg.Names(), ", "), cfg.Names()[0])
	}
	if _, err := os.Stat(filepath.Join(s.Root, ".oj")); err != nil {
		return nil, spec.UserError("sphere %q has no store at %s. Example: oj init --sphere %s --root %s", sphere, s.Root, sphere, s.Root)
	}
	if by == "" {
		by = "user"
	}
	return &Store{Sphere: sphere, Root: s.Root, VCS: s.VCS, By: by, Now: time.Now,
		Warn:   func(m string) { fmt.Fprintln(os.Stderr, "oj: warning: "+m) },
		Render: s.Render, Tools: cfg.Render, Hooks: s.Hooks, Refs: s.Refs, Asks: s.Asks, Cited: s.Cited}, nil
}

// Init creates the store directory and, with a VCS, its repository.
func Init(root, vcs string) error {
	if err := os.MkdirAll(filepath.Join(root, ".oj"), 0o755); err != nil {
		return err
	}
	ignore := filepath.Join(root, ".gitignore")
	if _, err := os.Stat(ignore); os.IsNotExist(err) {
		if err := os.WriteFile(ignore, []byte(".oj/\n"), 0o644); err != nil {
			return err
		}
	}
	switch vcs {
	case "jj":
		if _, err := os.Stat(filepath.Join(root, ".jj")); os.IsNotExist(err) {
			if out, err := runIn(root, "jj", "git", "init"); err != nil {
				return fmt.Errorf("jj git init: %v: %s", err, out)
			}
		}
	case "git":
		if _, err := os.Stat(filepath.Join(root, ".git")); os.IsNotExist(err) {
			if out, err := runIn(root, "git", "init", "-q"); err != nil {
				return fmt.Errorf("git init: %v: %s", err, out)
			}
		}
	}
	return nil
}

func runIn(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Write runs fn under the sphere's lock, then commits with msg.
func (s *Store) Write(msg string, fn func() error) error {
	return s.writeAs(func() (string, error) { return msg, fn() })
}

// writeAs is Write with a commit message known only once fn has run. The
// events fn emits run their hooks after the commit, once the lock is released:
// a hook may call oj again.
func (s *Store) writeAs(fn func() (string, error)) error {
	events, err := s.locked(fn)
	if err != nil {
		return err
	}
	s.fire(events)
	return nil
}

func (s *Store) locked(fn func() (string, error)) ([]hookEvent, error) {
	f, err := os.OpenFile(filepath.Join(s.Root, ".oj", "lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return nil, spec.Locked("cannot lock the store of %s: %v", s.Sphere, err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	s.takePending()
	msg, err := fn()
	events := s.takePending()
	if err != nil {
		return nil, err
	}
	if msg != "" {
		s.commit(msg)
	}
	return events, nil
}

func (s *Store) commit(msg string) {
	var out string
	var err error
	switch s.VCS {
	case "jj":
		out, err = runIn(s.Root, "jj", "commit", "-m", msg)
	case "git":
		if out, err = runIn(s.Root, "git", "add", "-A"); err == nil {
			out, err = runIn(s.Root, "git", "commit", "-q", "--allow-empty", "-m", msg)
		}
	default:
		return
	}
	if err != nil && s.Warn != nil {
		s.Warn(fmt.Sprintf("%s commit failed in %s: %v: %s", s.VCS, s.Root, err, out))
	}
}

func (s *Store) log(l *[]LogEntry, what string) {
	*l = append(*l, LogEntry{At: s.Now().Format(time.RFC3339), By: s.By, What: what})
}

func (s *Store) today() string { return s.Now().Format("2006-01-02") }

// ---------------------------------------------------------------- ids

var (
	aliasRe   = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,15}$`)
	itemRe    = regexp.MustCompile(`^([A-Z][A-Z0-9]{1,15})-([0-9]+)$`)
	sittingRe = regexp.MustCompile(`^([A-Z][A-Z0-9]{1,15})-([0-9]{4}-[0-9]{2}-[0-9]{2})(?:-([0-9]+))?$`)
)

// NormAlias upper-cases and checks a meeting alias.
func NormAlias(a string) (string, error) {
	a = strings.ToUpper(strings.TrimSpace(a))
	if !aliasRe.MatchString(a) {
		return "", spec.UserError("meeting alias %q: 2 to 16 letters or digits, starting with a letter, e.g. RDIR", a)
	}
	return a, nil
}

func parseItemID(id string) (alias string, n int, err error) {
	m := itemRe.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(id)))
	if m == nil {
		return "", 0, spec.UserError("item id %q: expected <alias>-<number>, e.g. RDIR-17", id)
	}
	n, _ = strconv.Atoi(m[2])
	return m[1], n, nil
}

func parseSittingID(id string) (alias, key string, err error) {
	id = strings.ToUpper(strings.TrimSpace(id))
	m := sittingRe.FindStringSubmatch(id)
	if m == nil {
		return "", "", spec.UserError("sitting id %q: expected <alias>-<YYYY-MM-DD>, e.g. RDIR-2026-10-08", id)
	}
	if _, err := time.Parse("2006-01-02", m[2]); err != nil {
		return "", "", spec.UserError("sitting id %q: %s is not a date", id, m[2])
	}
	return m[1], strings.TrimPrefix(id, m[1]+"-"), nil
}

// IsSittingID tells a sitting id from a meeting alias.
func IsSittingID(s string) bool { return sittingRe.MatchString(strings.ToUpper(strings.TrimSpace(s))) }

// ---------------------------------------------------------------- durations

// NormDuration parses 15m, 1h30m or 90 (minutes) and returns its canonical form.
func NormDuration(d string) (string, error) {
	d = strings.TrimSpace(d)
	if d == "" {
		return "", nil
	}
	if n, err := strconv.Atoi(d); err == nil {
		d = strconv.Itoa(n) + "m"
	}
	v, err := time.ParseDuration(d)
	if err != nil || v < 0 || v%time.Minute != 0 {
		return "", spec.UserError("duration %q: use minutes, e.g. 15m, 90m or 1h30m", d)
	}
	return FormatDuration(v), nil
}

func FormatDuration(v time.Duration) string {
	h, m := int(v/time.Hour), int(v%time.Hour/time.Minute)
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh%dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

func minutes(d string) time.Duration {
	v, _ := time.ParseDuration(d)
	return v
}

func normDate(d string) (string, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(d))
	if err != nil {
		return "", spec.UserError("date %q: expected YYYY-MM-DD, e.g. 2026-10-08", d)
	}
	return t.Format("2006-01-02"), nil
}

func normTime(t string) (string, error) {
	if t == "" {
		return "", nil
	}
	v, err := time.Parse("15:04", strings.TrimSpace(t))
	if err != nil {
		return "", spec.UserError("time %q: expected HH:MM, e.g. 09:00", t)
	}
	return v.Format("15:04"), nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}
