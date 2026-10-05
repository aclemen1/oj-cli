package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aclemen1/ordo-cli/internal/actions"
	"github.com/aclemen1/ordo-cli/internal/spec"
)

type cli struct {
	t      *testing.T
	config string
}

func newCLI(t *testing.T) *cli {
	dir := t.TempDir()
	actions.SetClock(func() time.Time { return time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC) })
	t.Cleanup(func() { actions.SetClock(nil) })
	t.Setenv("ORDO_SPHERE", "")
	t.Setenv("ORDO_BY", "")
	c := &cli{t: t, config: filepath.Join(dir, "config.yaml")}
	c.ok("init", "--sphere", "pro", "--root", filepath.Join(dir, "pro"), "--vcs", "none")
	return c
}

func (c *cli) run(args ...string) (int, map[string]any, string) {
	var out, errw bytes.Buffer
	code := run(append(args, "--config", c.config), strings.NewReader(""), &out, &errw)
	var env map[string]any
	_ = json.Unmarshal(out.Bytes(), &env)
	return code, env, out.String()
}

func (c *cli) ok(args ...string) map[string]any {
	c.t.Helper()
	code, env, raw := c.run(args...)
	if code != 0 || env["ok"] != true {
		c.t.Fatalf("ordo %s: exit %d: %s", strings.Join(args, " "), code, raw)
	}
	r, _ := env["result"].(map[string]any)
	return r
}

func TestCLICycle(t *testing.T) {
	c := newCLI(t)
	c.ok("meeting", "add", "RDIR", "--title", "Séance de direction", "--rrule", "FREQ=WEEKLY;BYDAY=TH",
		"--start", "2026-10-01T09:00", "--tz", "Europe/Zurich", "--duration", "90m", "--sphere", "pro")
	it := c.ok("item", "add", "rdir", "Budget 2027", "--kind", "decision", "--duration", "20m", "--accept", "--sphere", "pro")
	if it["id"] != "RDIR-1" || it["sitting"] != "RDIR-2026-10-08" {
		t.Fatalf("item %v", it)
	}
	t.Setenv("ORDO_SPHERE", "pro")
	c.ok("sitting", "freeze", "RDIR")
	c.ok("sitting", "hold", "RDIR-2026-10-08", "--present", "Marie")
	c.ok("outcome", "set", "RDIR-1", "--decision", "Approuvé", "--by", "agent:claude")
	m := c.ok("sitting", "minute", "RDIR-2026-10-08")
	if m["approved"] != float64(1) {
		t.Fatalf("minute %v", m)
	}
	_, _, text := c.run("sitting", "show", "RDIR-2026-10-08", "--format", "text")
	if !strings.Contains(text, "Budget 2027") || !strings.Contains(text, "! Approuvé") || !strings.Contains(text, "minuted") {
		t.Fatalf("text agenda:\n%s", text)
	}
}

func TestCLIErrors(t *testing.T) {
	c := newCLI(t)
	code, env, raw := c.run("item", "ls")
	if code != spec.ExitUsage || !strings.Contains(raw, "--sphere") {
		t.Fatalf("missing sphere: %d %v", code, env)
	}
	code, _, raw = c.run("item", "add", "RDIR", "x", "--sphere", "pro")
	if code != spec.ExitNotFound || !strings.Contains(raw, "ordo meeting ls") {
		t.Fatalf("unknown meeting: %d %s", code, raw)
	}
	code, _, raw = c.run("item", "add", "RDIR", "x", "--kind", "vote", "--sphere", "pro")
	if code != spec.ExitUsage || !strings.Contains(raw, "info|discussion|decision") {
		t.Fatalf("bad enum: %d %s", code, raw)
	}
	code, _, raw = c.run("item", "frobnicate")
	if code != spec.ExitUsage || !strings.Contains(raw, "ordo schema item") {
		t.Fatalf("unknown action: %d %s", code, raw)
	}
	code, _, raw = c.run("sitting", "ls", "--sphere", "perso")
	if code != spec.ExitUsage || !strings.Contains(raw, "configured: pro") {
		t.Fatalf("unknown sphere: %d %s", code, raw)
	}
}

func TestCLISchemaAndSkill(t *testing.T) {
	c := newCLI(t)
	_, _, raw := c.run("schema", "--format", "json")
	for _, cat := range []string{"meeting", "sitting", "item", "outcome"} {
		if !strings.Contains(raw, `"`+cat+`"`) {
			t.Fatalf("catalog lacks %s: %s", cat, raw)
		}
	}
	leaf := c.ok("schema", "item", "add", "--format", "json")
	if leaf["usage"] == nil || leaf["examples"] == nil {
		t.Fatalf("leaf %v", leaf)
	}
	_, _, skill := c.run("skill", "show")
	if lines := strings.Count(skill, "\n"); lines > 50 || !strings.Contains(skill, "ordo schema") {
		t.Fatalf("skill: %d lines", lines)
	}
	for _, a := range spec.All() {
		if len(a.Examples) == 0 {
			t.Errorf("%s has no example", a.Command())
		}
		for _, ex := range a.Examples {
			if !strings.HasPrefix(ex, a.Command()) {
				t.Errorf("example %q does not start with %q", ex, a.Command())
			}
		}
	}
}
