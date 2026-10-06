package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aclemen1/oj-cli/internal/actions"
	"github.com/aclemen1/oj-cli/internal/config"
	"github.com/aclemen1/oj-cli/internal/store"
)

func connect(t *testing.T, spheres ...string) (*mcp.ClientSession, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg, _ := config.Load(cfgPath)
	for _, s := range []string{"perso", "pro"} {
		root := filepath.Join(dir, s)
		if err := store.Init(root, "none"); err != nil {
			t.Fatal(err)
		}
		if err := cfg.AddSphere(s, config.Sphere{Root: root, VCS: "none"}); err != nil {
			t.Fatal(err)
		}
	}
	actions.SetClock(func() time.Time { return time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC) })
	t.Cleanup(func() { actions.SetClock(nil) })
	t.Setenv("OJ_BY", "")
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := New(cfgPath, spheres).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, cfgPath
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (map[string]any, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &env); err != nil {
		t.Fatal(err)
	}
	r, _ := env["result"].(map[string]any)
	if res.IsError {
		e, _ := env["error"].(map[string]any)
		return e, false
	}
	return r, true
}

func TestToolsAndSphereDefault(t *testing.T) {
	cs, _ := connect(t, "pro")
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range tools.Tools {
		names = append(names, tl.Name)
	}
	joined := strings.Join(names, " ")
	for _, want := range []string{"meeting_add", "item_add", "sitting_minute", "outcome_set", "doc_render", "actions_ls", "import_gtasks"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %s", want, joined)
		}
	}
	for _, unwanted := range []string{"setup_init", "setup_mcp", "setup_tui", "meta_schema", "meta_skill"} {
		for _, n := range names {
			if n == unwanted {
				t.Fatalf("%s should not be a tool", n)
			}
		}
	}
	if _, ok := callTool(t, cs, "meeting_add", map[string]any{"alias": "RDIR", "title": "Séance", "rrule": "FREQ=WEEKLY;BYDAY=TH", "start": "2026-10-01T09:00", "tz": "Europe/Zurich"}); !ok {
		t.Fatal("meeting_add failed")
	}
	it, ok := callTool(t, cs, "item_add", map[string]any{"meeting": "RDIR", "title": "Budget", "accept": true, "ref": []string{"office:U-0042"}})
	if !ok || it["id"] != "RDIR-1" || it["sitting"] != "RDIR-2026-10-08" {
		t.Fatalf("item_add %v", it)
	}
	log := it["log"].([]any)[0].(map[string]any)
	if log["by"] != "agent:mcp" {
		t.Fatalf("log by %v", log["by"])
	}
}

func TestOutcomeThroughMCPIsDraft(t *testing.T) {
	cs, _ := connect(t, "pro")
	callTool(t, cs, "meeting_add", map[string]any{"alias": "RDIR", "title": "Séance", "rrule": "FREQ=WEEKLY;BYDAY=TH", "start": "2026-10-01T09:00", "tz": "Europe/Zurich"})
	callTool(t, cs, "item_add", map[string]any{"meeting": "RDIR", "title": "Budget", "accept": true})
	it, ok := callTool(t, cs, "outcome_set", map[string]any{"id": "RDIR-1", "decision": "Approuvé", "by": "alain"})
	if !ok {
		t.Fatalf("outcome_set %v", it)
	}
	o := it["history"].([]any)[0].(map[string]any)["outcome"].(map[string]any)
	if o["status"] != "draft" || o["by"] != "agent:alain" {
		t.Fatalf("outcome %v", o)
	}
}

func TestSpheresAreSealed(t *testing.T) {
	cs, _ := connect(t, "pro")
	e, ok := callTool(t, cs, "meeting_ls", map[string]any{"sphere": "perso"})
	if ok || (e["kind"] != "user_error" && e["kind"] != "forbidden") {
		t.Fatalf("perso through a pro server: %v", e)
	}
	if _, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "oj://perso/RDIR-1"}); err == nil {
		t.Fatal("a perso resource through a pro server")
	}
}

func TestResource(t *testing.T) {
	cs, _ := connect(t, "pro")
	callTool(t, cs, "meeting_add", map[string]any{"alias": "RDIR", "title": "Séance", "rrule": "FREQ=WEEKLY;BYDAY=TH", "start": "2026-10-01T09:00", "tz": "Europe/Zurich"})
	callTool(t, cs, "item_add", map[string]any{"meeting": "RDIR", "title": "Budget", "accept": true})
	res, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "oj://pro/RDIR-2026-10-08"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Contents[0].Text, `"Budget"`) {
		t.Fatalf("agenda resource: %s", res.Contents[0].Text)
	}
}

func TestReadsCoverServedSpheres(t *testing.T) {
	cs, _ := connect(t, "perso", "pro")
	t.Setenv("OJ_SPHERE", "")
	if e, ok := callTool(t, cs, "meeting_add", map[string]any{"alias": "RDIR", "title": "Séance"}); ok || !strings.Contains(e["message"].(string), "sphere") {
		t.Fatalf("a write without a sphere: %v", e)
	}
	if _, ok := callTool(t, cs, "meeting_add", map[string]any{"alias": "pro:RDIR", "title": "Séance"}); !ok {
		t.Fatal("a write with a qualified alias")
	}
	callTool(t, cs, "meeting_add", map[string]any{"alias": "FAM", "title": "Famille", "sphere": "perso"})
	if m, ok := callTool(t, cs, "meeting_show", map[string]any{"alias": "FAM"}); !ok || m["sphere"] != "perso" {
		t.Fatalf("a read across spheres: %v", m)
	}
	tools, _ := cs.ListTools(context.Background(), nil)
	for _, tl := range tools.Tools {
		b, _ := json.Marshal(tl.InputSchema)
		if strings.Contains(string(b), `"required":["sphere"`) || strings.Contains(string(b), `,"sphere"]`) {
			t.Fatalf("%s requires sphere: %s", tl.Name, b)
		}
	}
}

func TestQualifiedIDIsSealed(t *testing.T) {
	cs, _ := connect(t, "pro")
	if e, ok := callTool(t, cs, "meeting_add", map[string]any{"alias": "perso:FAM", "title": "Famille"}); ok {
		t.Fatalf("a perso write through a pro server: %v", e)
	}
	if e, ok := callTool(t, cs, "meeting_show", map[string]any{"alias": "perso:FAM"}); ok {
		t.Fatalf("a perso read through a pro server: %v", e)
	}
}
