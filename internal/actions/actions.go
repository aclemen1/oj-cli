// Package actions declares every oj action once; the CLI, the schema and
// the MCP server are projections of these declarations.
package actions

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aclemen1/oj-cli/internal/config"
	"github.com/aclemen1/oj-cli/internal/spec"
	"github.com/aclemen1/oj-cli/internal/store"
)

const Version = "0.1.0"

//go:embed skill.md
var skillText string

// clock replaces the store clock in tests.
var clock func() time.Time

// SetClock fixes the date the stores see; nil restores the real clock.
func SetClock(f func() time.Time) { clock = f }

func sphereParam() spec.Param {
	return spec.Param{Name: "sphere", Kind: spec.String,
		Help: "Sphere, e.g. pro. A read covers every sphere by default; a write needs one: --sphere, a qualified id (pro:RDIR-3) or $OJ_SPHERE."}
}

// idParams are the params whose value may be qualified by a sphere: pro:RDIR-3.
var idParams = []string{"id", "sitting", "meeting", "alias", "to"}

// qualify strips sphere qualifiers from the id params and returns the sphere they name.
func qualify(ctx *spec.Context, cfg *config.Config) (string, error) {
	sphere := ""
	take := func(v string) (string, error) {
		pre, rest, ok := strings.Cut(v, ":")
		if !ok {
			return v, nil
		}
		if _, known := cfg.Spheres[pre]; !known {
			return "", spec.UserError("%q: %q is not a configured sphere (configured: %s). Example: pro:RDIR-3", v, pre, strings.Join(cfg.Names(), ", "))
		}
		if sphere != "" && sphere != pre {
			return "", spec.UserError("ids of two spheres in one call: %s and %s", sphere, pre)
		}
		sphere = pre
		return rest, nil
	}
	for _, name := range idParams {
		switch v := ctx.Args[name].(type) {
		case string:
			r, err := take(v)
			if err != nil {
				return "", err
			}
			ctx.Args[name] = r
		case []string:
			out := make([]string, len(v))
			for i, x := range v {
				r, err := take(x)
				if err != nil {
					return "", err
				}
				out[i] = r
			}
			ctx.Args[name] = out
		}
	}
	if explicit := ctx.Str("sphere"); explicit != "" && sphere != "" && explicit != sphere {
		return "", spec.UserError("--sphere %s and an id of sphere %s", explicit, sphere)
	}
	return sphere, nil
}

func openSphere(ctx *spec.Context, cfg *config.Config, sphere string) (*store.Store, error) {
	if ctx.Spheres != nil && !contains(ctx.Spheres, sphere) {
		return nil, spec.Forbidden("sphere %q is not served here; served: %s", sphere, strings.Join(ctx.Spheres, ", "))
	}
	by := os.Getenv("OJ_BY")
	if ctx.Spheres != nil && !strings.HasPrefix(by, "agent:") {
		by = "agent:mcp"
	}
	st, err := store.Open(cfg, sphere, by)
	if err != nil {
		return nil, err
	}
	if clock != nil {
		st.Now = clock
	}
	st.Warn = ctx.Warn
	return st, nil
}

// open returns the store a write acts in: a qualified id's sphere, --sphere,
// then $OJ_SPHERE; a write never falls back on every sphere.
func open(ctx *spec.Context) (*store.Store, error) {
	cfg, err := config.Load(ctx.Config)
	if err != nil {
		return nil, err
	}
	sphere, err := qualify(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if sphere == "" {
		sphere = ctx.Str("sphere")
	}
	if sphere == "" {
		sphere = os.Getenv("OJ_SPHERE")
	}
	if sphere == "" {
		names := cfg.Names()
		ex := "pro"
		if len(names) > 0 {
			ex = names[0]
		}
		return nil, spec.UserError("a write needs a sphere: pass --sphere, a qualified id (%s:RDIR-3) or set OJ_SPHERE (configured: %s). Example: --sphere %s",
			ex, strings.Join(names, ", "), ex)
	}
	return openSphere(ctx, cfg, sphere)
}

// OpenRead returns the stores a read covers: the sphere named by --sphere or a
// qualified id, else every served sphere. $OJ_SPHERE does not narrow a read.
func OpenRead(ctx *spec.Context) ([]*store.Store, error) {
	cfg, err := config.Load(ctx.Config)
	if err != nil {
		return nil, err
	}
	sphere, err := qualify(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if sphere == "" {
		sphere = ctx.Str("sphere")
	}
	names := cfg.Names()
	if ctx.Spheres != nil {
		names = ctx.Spheres
	}
	if sphere != "" {
		names = []string{sphere}
	}
	if len(names) == 0 {
		return nil, spec.UserError("no sphere is configured. Example: oj init --sphere pro --root ~/oj/pro")
	}
	var out []*store.Store
	for _, n := range names {
		st, err := openSphere(ctx, cfg, n)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// pick finds id in the stores: one match is returned with its store; none is
// the last not-found error; several ask for a qualified id.
func pick[T any](stores []*store.Store, id string, find func(*store.Store) (T, error)) (T, *store.Store, error) {
	var zero T
	var found []T
	var where []*store.Store
	var notFound error
	for _, st := range stores {
		v, err := find(st)
		if err != nil {
			if e, ok := err.(*spec.Error); ok && e.Kind == "not_found" {
				notFound = err
				continue
			}
			return zero, nil, err
		}
		found, where = append(found, v), append(where, st)
	}
	switch len(found) {
	case 0:
		return zero, nil, notFound
	case 1:
		return found[0], where[0], nil
	}
	var q []string
	for _, st := range where {
		q = append(q, st.Sphere+":"+strings.ToUpper(id))
	}
	return zero, nil, spec.UserError("%s exists in several spheres: use %s", strings.ToUpper(id), strings.Join(q, " or "))
}

// narrow keeps the store that holds the meeting alias, when one is given.
func narrow(stores []*store.Store, alias string) ([]*store.Store, error) {
	if alias == "" {
		return stores, nil
	}
	_, st, err := pick(stores, alias, func(st *store.Store) (*store.Meeting, error) { return st.Meeting(alias) })
	if err != nil {
		return nil, err
	}
	return []*store.Store{st}, nil
}

// withRead opens the stores of a read and runs f.
func withRead(f func(ctx *spec.Context, stores []*store.Store) (any, error)) func(*spec.Context) (any, error) {
	return func(ctx *spec.Context) (any, error) {
		stores, err := OpenRead(ctx)
		if err != nil {
			return nil, err
		}
		return f(ctx, stores)
	}
}

// spheresIn counts the distinct spheres of a result, to show them when there are several.
func spheresIn(names ...string) int {
	seen := map[string]bool{}
	for _, n := range names {
		seen[n] = true
	}
	return len(seen)
}

// tag prefixes a sphere when several are shown.
func tag(show bool, sphere string) string {
	if !show {
		return ""
	}
	return sphere + ":"
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// with opens the store and runs f.
func with(f func(ctx *spec.Context, st *store.Store) (any, error)) func(*spec.Context) (any, error) {
	return func(ctx *spec.Context) (any, error) {
		st, err := open(ctx)
		if err != nil {
			return nil, err
		}
		return f(ctx, st)
	}
}

func init() {
	registerMeetings()
	registerSittings()
	registerItems()
	registerMeta()
}

func registerMeta() {
	spec.Register(&spec.Action{
		Category: "setup", Name: "init", Top: true,
		Summary: "Declare a sphere and create its store.",
		Params: []spec.Param{
			{Name: "sphere", Kind: spec.String, Required: true, Help: "Sphere name, e.g. pro."},
			{Name: "root", Kind: spec.String, Required: true, Help: "Directory of the sphere's store, e.g. ~/oj/pro."},
			{Name: "vcs", Kind: spec.String, Default: "jj", Enum: []string{"jj", "git", "none"}, Help: "Version control of the store: a commit after each change."},
		},
		Effects:  []string{"Adds the sphere to the configuration file.", "Creates the store directory and, with jj or git, its repository."},
		Examples: []string{"oj init --sphere pro --root ~/oj/pro", "oj init --sphere perso --root ~/oj/perso --vcs none"},
		Run: func(ctx *spec.Context) (any, error) {
			cfg, err := config.Load(ctx.Config)
			if err != nil {
				return nil, err
			}
			name, root, vcs := ctx.Str("sphere"), config.Expand(ctx.Str("root")), ctx.Str("vcs")
			if old, ok := cfg.Spheres[name]; ok && old.Root != root {
				return nil, spec.Conflict("sphere %s already has its store at %s", name, old.Root)
			}
			if err := store.Init(root, vcs); err != nil {
				return nil, err
			}
			if err := cfg.AddSphere(name, config.Sphere{Root: root, VCS: vcs}); err != nil {
				return nil, spec.UserError("%v", err)
			}
			return map[string]any{"sphere": name, "root": root, "vcs": vcs, "config": cfg.File()}, nil
		},
	})
	spec.Register(&spec.Action{
		Category: "meta", Name: "version", Top: true, Meta: true, Summary: "Print the oj version.",
		Examples: []string{"oj version"},
		Run:      func(*spec.Context) (any, error) { return Version, nil },
	})
	spec.Register(&spec.Action{
		Category: "meta", Name: "schema", Top: true, Meta: true,
		Summary: "Browse actions: catalog, category, or one action's full spec.",
		Params: []spec.Param{
			{Name: "category", Kind: spec.String, Positional: true, Help: "Category to list."},
			{Name: "action", Kind: spec.String, Positional: true, Help: "Action to describe."},
			{Name: "search", Kind: spec.String, Help: "Match actions across categories."},
		},
		Examples: []string{"oj schema", "oj schema item", "oj schema item add", "oj schema --search defer"},
		Run: func(ctx *spec.Context) (any, error) {
			if q := ctx.Str("search"); q != "" {
				return spec.Search(q), nil
			}
			cat, act := ctx.Str("category"), ctx.Str("action")
			switch {
			case cat == "":
				return spec.Catalog(), nil
			case act == "":
				l := spec.ActionsIn(cat)
				if len(l) == 0 {
					return nil, spec.NotFound("no category %q. Categories: %s", cat, strings.Join(spec.Categories(), ", "))
				}
				return l, nil
			}
			a := spec.Find(cat, act)
			if a == nil {
				return nil, spec.NotFound("no action %q in %q. Try `oj schema %s`", act, cat, cat)
			}
			return spec.Leaf{Action: a, Usage: spec.Usage(a)}, nil
		},
		Text: spec.TextSchema,
	})
	spec.Register(&spec.Action{
		Category: "meta", Name: "skill", Top: true, Meta: true,
		Summary: "Print the embedded agent skill, or install it for an agent harness.",
		Params: []spec.Param{
			{Name: "verb", Kind: spec.String, Positional: true, Default: "show", Enum: []string{"show", "install"}, Help: "show or install"},
			{Name: "for", Kind: spec.String, Default: "claude", Help: "Harness to install for: claude."},
			{Name: "dir", Kind: spec.String, Help: "Install into this directory instead."},
		},
		Effects:  []string{"install: writes SKILL.md into ~/.claude/skills/oj/ (or --dir)."},
		Examples: []string{"oj skill show", "oj skill install --for claude"},
		Run: func(ctx *spec.Context) (any, error) {
			if ctx.Str("verb") != "install" {
				return strings.TrimSpace(skillText), nil
			}
			if f := ctx.Str("for"); f != "claude" && f != "claude-code" {
				return nil, spec.UserError("--for takes claude, got %q. Example: oj skill install --for claude", f)
			}
			dir := config.Expand(ctx.Str("dir"))
			if dir == "" {
				home, _ := os.UserHomeDir()
				dir = filepath.Join(home, ".claude", "skills", "oj")
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
			p := filepath.Join(dir, "SKILL.md")
			if err := os.WriteFile(p, []byte(skillText), 0o644); err != nil {
				return nil, err
			}
			return "installed " + p, nil
		},
	})
}
