// Package config reads ~/.config/ordo/config.yaml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/aclemen1/ordo-cli/internal/calendar"
)

type Sphere struct {
	Root   string       `yaml:"root"`
	VCS    string       `yaml:"vcs,omitempty"` // jj (default), git, none
	Render SphereRender `yaml:"render,omitempty"`
}

// SphereRender: documents of a sphere.
type SphereRender struct {
	Lang         string            `yaml:"lang,omitempty"`          // en (default) or fr
	ReferenceDoc string            `yaml:"reference_doc,omitempty"` // docx layout for pandoc
	Templates    map[string]string `yaml:"templates,omitempty"`     // agenda, minutes: Go templates
}

// Render: tools shared by every sphere.
type Render struct {
	Pandoc    string `yaml:"pandoc,omitempty"`
	PDFEngine string `yaml:"pdf_engine,omitempty"`
}

type Config struct {
	Spheres   map[string]Sphere          `yaml:"spheres"`
	Render    Render                     `yaml:"render,omitempty"`
	Calendars map[string]calendar.Source `yaml:"calendars,omitempty"`

	path string
}

var sphereName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// Path resolves the configuration file: the flag, then ORDO_CONFIG, then
// ~/.config/ordo/config.yaml.
func Path(flag string) string {
	if flag != "" {
		return Expand(flag)
	}
	if env := os.Getenv("ORDO_CONFIG"); env != "" {
		return Expand(env)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ordo", "config.yaml")
}

// Load reads the configuration. A missing file yields an empty one.
func Load(flag string) (*Config, error) {
	p := Path(flag)
	c := &Config{Spheres: map[string]Sphere{}, path: p}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	if c.Spheres == nil {
		c.Spheres = map[string]Sphere{}
	}
	for name, s := range c.Spheres {
		s.Root = Expand(s.Root)
		if s.VCS == "" {
			s.VCS = "jj"
		}
		s.Render.ReferenceDoc = Expand(s.Render.ReferenceDoc)
		for k, v := range s.Render.Templates {
			s.Render.Templates[k] = Expand(v)
		}
		c.Spheres[name] = s
	}
	if c.Render.Pandoc == "" {
		c.Render.Pandoc = "pandoc"
	}
	if c.Render.PDFEngine == "" {
		c.Render.PDFEngine = "xelatex"
	}
	return c, nil
}

func (c *Config) File() string { return c.path }

func (c *Config) Names() []string {
	var out []string
	for n := range c.Spheres {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// AddSphere declares a sphere and writes the file.
func (c *Config) AddSphere(name string, s Sphere) error {
	if !sphereName.MatchString(name) {
		return fmt.Errorf("sphere name %q: use lower-case letters, digits, - or _, e.g. pro", name)
	}
	c.Spheres[name] = s
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, b, 0o644)
}

// Expand replaces a leading ~ with the home directory.
func Expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}
