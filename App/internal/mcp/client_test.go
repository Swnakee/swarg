package mcp_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"swarg/internal/config"
	"swarg/internal/mcp"
	"swarg/internal/mcp/mcptest"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func echoTool(name string, ro bool) mcptest.Tool {
	return mcptest.Tool{Name: name, Description: "the " + name + " tool", ReadOnly: ro,
		Handler: func(args json.RawMessage) (string, bool) { return name + ":" + string(args), false }}
}

// start builds a client over the fake with one server "demo" and starts it.
func start(t *testing.T, fake *mcptest.Server, cfg config.MCPServer, confirm bool) (*mcp.Client, []string) {
	t.Helper()
	if cfg.Command == "" {
		cfg.Command = "fake"
	}
	c := mcp.New(map[string]config.MCPServer{"demo": cfg}, mcp.Deps{Confirm: confirm, Dial: fake.Dial})
	t.Cleanup(c.Close)
	return c, c.Start(context.Background())
}

func specNames(c *mcp.Client) string {
	var out []string
	for _, s := range c.Specs() {
		out = append(out, s.Name)
	}
	return strings.Join(out, " ")
}

// ---------------------------------------------------------------------------
// Specs, Kind, Run
// ---------------------------------------------------------------------------

func TestClient_SpecsNamedByServerAndPaged(t *testing.T) {
	fake := mcptest.New(echoTool("b", false), echoTool("a", true), echoTool("c", false))
	fake.PageSize = 2
	fake.Instructions = "  use a   for lookups  "
	c, warnings := start(t, fake, config.MCPServer{}, true)
	if len(warnings) != 0 {
		t.Fatalf("warnings %q", warnings)
	}
	if got := specNames(c); got != "mcp__demo__a mcp__demo__b mcp__demo__c" {
		t.Fatalf("specs %q", got)
	}
	if ins := c.Instructions(); ins != "demo: use a for lookups" {
		t.Fatalf("instructions %q", ins)
	}
	if srv := c.Servers(); len(srv) != 1 || srv[0].State != mcp.StateRunning || srv[0].Tools != 3 {
		t.Fatalf("servers %+v", srv)
	}
}

func TestClient_RunReturnsTextAndIsErrorBecomesError(t *testing.T) {
	fake := mcptest.New(echoTool("a", false), mcptest.Tool{Name: "bad", Handler: func(json.RawMessage) (string, bool) { return "boom", true }})
	c, _ := start(t, fake, config.MCPServer{}, true)
	out, brief, err := c.Run(context.Background(), "mcp__demo__a", `{"q":"x"}`)
	if err != nil || out != `a:{"q":"x"}` || brief != `a:{"q":"x"}` {
		t.Fatalf("run %q %q %v", out, brief, err)
	}
	if _, _, err := c.Run(context.Background(), "mcp__demo__bad", ""); err == nil || err.Error() != "boom" {
		t.Fatalf("isError: %v", err)
	}
	if _, _, err := c.Run(context.Background(), "mcp__demo__zzz", "{}"); err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("unknown: %v", err)
	}
	if fake.Calls[0] != "a" || fake.Calls[1] != "bad" || len(fake.Calls) != 2 {
		t.Fatalf("calls %v", fake.Calls)
	}
}

func TestClient_KindTrustAndConfirm(t *testing.T) {
	fake := mcptest.New(echoTool("ro", true), echoTool("rw", false))
	c, _ := start(t, fake, config.MCPServer{}, true) // untrusted: every tool asks
	for _, n := range []string{"mcp__demo__ro", "mcp__demo__rw"} {
		if known, ro, confirm := c.Kind(n); !known || ro || !confirm {
			t.Fatalf("%s untrusted: %v %v %v", n, known, ro, confirm)
		}
	}
	c, _ = start(t, mcptest.New(echoTool("ro", true), echoTool("rw", false)), config.MCPServer{Trust: true}, true)
	if _, ro, confirm := c.Kind("mcp__demo__ro"); !ro || confirm {
		t.Fatalf("trusted ro: %v %v", ro, confirm)
	}
	if _, ro, confirm := c.Kind("mcp__demo__rw"); ro || !confirm {
		t.Fatalf("trusted rw: %v %v", ro, confirm)
	}
	c, _ = start(t, mcptest.New(echoTool("rw", false)), config.MCPServer{}, false) // mcp.confirm off
	if _, ro, confirm := c.Kind("mcp__demo__rw"); ro || confirm {
		t.Fatalf("confirm off: %v %v", ro, confirm)
	}
	if known, _, _ := c.Kind("read_file"); known {
		t.Fatal("a built-in name is not an MCP tool")
	}
}

func TestClient_AllowlistAndDescriptionCut(t *testing.T) {
	long := mcptest.Tool{Name: "long", Description: strings.Repeat("d", 300)}
	fake := mcptest.New(echoTool("a", false), echoTool("b", false), long)
	c, _ := start(t, fake, config.MCPServer{Tools: []string{"a", "long", "nope"}}, true)
	if got := specNames(c); got != "mcp__demo__a mcp__demo__long" {
		t.Fatalf("specs %q", got)
	}
	if known, _, _ := c.Kind("mcp__demo__b"); known {
		t.Fatal("b is not offered")
	}
	if _, _, err := c.Run(context.Background(), "mcp__demo__b", "{}"); err == nil {
		t.Fatal("b must not run")
	}
	for _, s := range c.Specs() {
		if s.Name == "mcp__demo__long" && (len([]rune(s.Description)) != 203 || !strings.HasSuffix(s.Description, "...")) {
			t.Fatalf("description not cut: %d", len(s.Description))
		}
	}
	list, _, err := c.Tools("demo")
	if err != nil || len(list) != 3 || list[1].Offered || !list[0].Offered {
		t.Fatalf("tools %+v %v", list, err)
	}
	one, _ := start(t, mcptest.New(echoTool("a", false), echoTool("b", false)), config.MCPServer{Tools: []string{"a"}}, true)
	if got := specNames(one); got != "mcp__demo__a" { // the allowlist must hold after its last name was seen
		t.Fatalf("one-tool allowlist offers %q", got)
	}
	if srv := c.Servers(); !strings.Contains(srv[0].Reason, "nope") {
		t.Fatalf("missing allowlisted name not reported: %+v", srv)
	}
}

// ---------------------------------------------------------------------------
// Failure, crash, timeout, restart, off
// ---------------------------------------------------------------------------

func TestClient_FailedStartIsAWarning(t *testing.T) {
	fake := mcptest.New(echoTool("a", false))
	fake.FailInit = true
	c, warnings := start(t, fake, config.MCPServer{}, true)
	if len(warnings) != 1 || !strings.Contains(warnings[0], `server "demo" failed to start`) || !strings.Contains(warnings[0], "init refused") {
		t.Fatalf("warnings %q", warnings)
	}
	if srv := c.Servers(); srv[0].State != mcp.StateFailed || len(c.Specs()) != 0 {
		t.Fatalf("servers %+v specs %d", srv, len(c.Specs()))
	}
	fake.Version = "1999-01-01"
	fake.FailInit = false
	if err := c.Restart(context.Background(), "demo"); err == nil || !strings.Contains(err.Error(), "protocol version") {
		t.Fatalf("version: %v", err)
	}
}

func TestClient_CrashMarksServerAndRestartRecovers(t *testing.T) {
	fake := mcptest.New(echoTool("a", false))
	c, _ := start(t, fake, config.MCPServer{}, true)
	fake.Crash()
	deadline := time.Now().Add(2 * time.Second)
	for c.Servers()[0].State != mcp.StateCrashed && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if srv := c.Servers(); srv[0].State != mcp.StateCrashed || srv[0].Reason != "exited" {
		t.Fatalf("servers %+v", srv)
	}
	if known, _, _ := c.Kind("mcp__demo__a"); !known { // the tool stays known so the call explains
		t.Fatal("crashed server's tool must stay known")
	}
	if _, _, err := c.Run(context.Background(), "mcp__demo__a", "{}"); err == nil || !strings.Contains(err.Error(), "crashed") || !strings.Contains(err.Error(), "/mcp restart demo") {
		t.Fatalf("err %v", err)
	}
	if err := c.Restart(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	if out, _, err := c.Run(context.Background(), "mcp__demo__a", "{}"); err != nil || out != "a:{}" {
		t.Fatalf("after restart %q %v", out, err)
	}
}

func TestClient_TimeoutNamesTheLimit(t *testing.T) {
	fake := mcptest.New(echoTool("a", false))
	fake.Silent = true
	c, _ := start(t, fake, config.MCPServer{TimeoutSec: 1}, true)
	t0 := time.Now()
	_, _, err := c.Run(context.Background(), "mcp__demo__a", "{}")
	if err == nil || !strings.Contains(err.Error(), "no answer in 1s") || time.Since(t0) > 3*time.Second {
		t.Fatalf("err %v after %s", err, time.Since(t0))
	}
	if srv := c.Servers(); srv[0].State != mcp.StateRunning {
		t.Fatalf("a timeout must not change the state: %+v", srv)
	}
}

func TestClient_OffHidesTools(t *testing.T) {
	fake := mcptest.New(echoTool("a", false))
	c, _ := start(t, fake, config.MCPServer{}, true)
	if err := c.SetEnabled("demo", false); err != nil || len(c.Specs()) != 0 {
		t.Fatalf("off: %v %d", err, len(c.Specs()))
	}
	if known, _, _ := c.Kind("mcp__demo__a"); known {
		t.Fatal("off server's tools must be unknown")
	}
	c.SetEnabled("demo", true)
	if len(c.Specs()) != 1 {
		t.Fatal("on again")
	}
	if err := c.SetEnabled("zzz", true); err == nil || !strings.Contains(err.Error(), "known servers: demo") {
		t.Fatalf("unknown: %v", err)
	}
	disabled, _ := start(t, mcptest.New(echoTool("a", false)), config.MCPServer{Disabled: true}, true)
	if srv := disabled.Servers(); srv[0].State != mcp.StateOff || len(disabled.Specs()) != 0 {
		t.Fatalf("disabled %+v", srv)
	}
}

func TestClient_LabelAndStub(t *testing.T) {
	c := mcp.New(nil, mcp.Deps{})
	if l := c.Label("mcp__demo__a", `{"z":"last","q":"first value"}`); l != "first value" { // the UI adds name(...)
		t.Fatalf("label %q", l)
	}
	if l := c.Label("mcp__demo__a", "{bad"); l != "" {
		t.Fatalf("label bad json %q", l)
	}
	if s := c.Stub("mcp__demo__a"); s != "[mcp__demo__a: result shown earlier]" {
		t.Fatalf("stub %q", s)
	}
	if !mcp.Owns("mcp__x__y") || mcp.Owns("read_file") {
		t.Fatal("Owns")
	}
}

// ---------------------------------------------------------------------------
// Real subprocess: the Python demo server
// ---------------------------------------------------------------------------

func TestStdio_PythonDemoServer(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH")
	}
	script, _ := filepath.Abs(filepath.Join("..", "..", "tests", "eval", "mcp", "mcp_demo.py"))
	dir := t.TempDir()
	c := mcp.New(map[string]config.MCPServer{"demo": {Command: py, Args: []string{script}, Trust: true, Env: map[string]string{"X": "${HOME}"}}}, mcp.Deps{Dir: dir, Confirm: true})
	defer c.Close()
	if w := c.Start(context.Background()); len(w) != 0 {
		t.Fatalf("warnings %q", w)
	}
	if got := specNames(c); got != "mcp__demo__lookup mcp__demo__save" {
		t.Fatalf("specs %q", got)
	}
	if _, ro, confirm := c.Kind("mcp__demo__lookup"); !ro || confirm {
		t.Fatal("lookup is read-only on a trusted server")
	}
	out, _, err := c.Run(context.Background(), "mcp__demo__lookup", `{"order_id":"A-100"}`)
	if err != nil || !strings.Contains(out, "shipped") {
		t.Fatalf("lookup %q %v", out, err)
	}
	if _, _, err := c.Run(context.Background(), "mcp__demo__lookup", `{"order_id":"Z"}`); err == nil || !strings.Contains(err.Error(), "unknown order") {
		t.Fatalf("isError %v", err)
	}
	if _, _, err := c.Run(context.Background(), "mcp__demo__save", `{"name":"../x/out.txt","text":"hi"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.Command("cat", filepath.Join(dir, "out.txt")).Output(); err != nil {
		t.Fatal("save did not write into the project folder")
	}
	c.Close()
	if srv := c.Servers(); srv[0].State != mcp.StateOff {
		t.Fatalf("after close %+v", srv)
	}
}

// ---------------------------------------------------------------------------
// Project servers need an approval (R-97, I-34)
// ---------------------------------------------------------------------------

func TestClient_ProjectServerWaitsForApproval(t *testing.T) {
	fake := mcptest.New(echoTool("a", false))
	approved := map[string]bool{}
	c := mcp.New(map[string]config.MCPServer{"demo": {Command: "fake"}}, mcp.Deps{Confirm: true, Dial: fake.Dial,
		Project: map[string]bool{"demo": true}, Approved: func(n string) bool { return approved[n] },
		OnApprove: func(n string) error { approved[n] = true; return nil }})
	t.Cleanup(c.Close)
	if w := c.Start(context.Background()); len(w) != 0 {
		t.Fatalf("warnings %q (waiting for approval is not a failure)", w)
	}
	if srv := c.Servers(); srv[0].State != mcp.StateNeedsApproval || srv[0].Source != "project" || srv[0].Tools != 0 || fake.Connected() {
		t.Fatalf("servers %+v connected %v", srv, fake.Connected())
	}
	if specNames(c) != "" {
		t.Fatalf("tools offered before approval: %q", specNames(c))
	}
	if _, _, err := c.Run(context.Background(), "mcp__demo__a", "{}"); err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("run %v", err)
	}
	if err := c.Restart(context.Background(), "demo"); err == nil || !strings.Contains(err.Error(), "/mcp approve demo") {
		t.Fatalf("restart must not bypass the approval: %v", err)
	}
	if err := c.Approve(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	if !approved["demo"] || specNames(c) != "mcp__demo__a" || c.Servers()[0].State != mcp.StateRunning {
		t.Fatalf("after approve: %v %q %+v", approved, specNames(c), c.Servers())
	}
	if err := c.Approve(context.Background(), "zzz"); err == nil || !strings.Contains(err.Error(), "unknown mcp server") {
		t.Fatalf("approve unknown: %v", err)
	}
}

func TestClient_ApprovedProjectServerStartsLikeAGlobalOne(t *testing.T) {
	fake := mcptest.New(echoTool("a", false))
	c := mcp.New(map[string]config.MCPServer{"demo": {Command: "fake"}, "glob": {URL: "https://h.example/mcp", Disabled: true}},
		mcp.Deps{Confirm: true, Dial: fake.Dial, Project: map[string]bool{"demo": true}, Approved: func(string) bool { return true }})
	t.Cleanup(c.Close)
	c.Start(context.Background())
	srv := c.Servers()
	if srv[0].State != mcp.StateRunning || srv[0].Source != "project" || srv[0].Transport != "stdio" {
		t.Fatalf("demo %+v", srv[0])
	}
	if srv[1].State != mcp.StateOff || srv[1].Source != "global" || srv[1].Transport != "http" {
		t.Fatalf("glob %+v", srv[1])
	}
}
