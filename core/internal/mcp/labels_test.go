package mcp

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

var labelToolNames = map[Mode][]string{
	ModeReadOnly:    {"list_labels"},
	ModeDraftStrict: {"list_labels"},
	ModeDraft:       {"list_labels", "label_threads"},
	ModeFull:        {"list_labels", "label_threads", "create_label", "update_label", "delete_label"},
}

func toolNames(t *testing.T, srv *Server) []string {
	t.Helper()
	resps := runRPC(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	raw, _ := json.Marshal(resps["1"].Result)
	var res struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func TestLabelToolsAreGatedByMode(t *testing.T) {
	all := labelToolNames[ModeFull]
	for mode, want := range labelToolNames {
		names := toolNames(t, NewServer(Options{Invoker: &fakeInvoker{}, Mode: mode}))
		for _, tool := range all {
			if got := slices.Contains(names, tool); got != slices.Contains(want, tool) {
				t.Errorf("mode %s: %s advertised=%v, want %v", mode, tool, got, !got)
			}
		}
	}
}

func TestLabelToolsRefuseCallsAboveTheMode(t *testing.T) {
	inv := &fakeInvoker{}
	cases := []struct {
		mode Mode
		tool string
		args string
		need string
	}{
		{ModeReadOnly, "label_threads", `{"threadIds":[42],"add":["Work"]}`, "--mode draft"},
		{ModeDraftStrict, "label_threads", `{"threadIds":[42],"add":["Work"]}`, "--mode draft"},
		{ModeDraft, "create_label", `{"name":"X"}`, "--mode full"},
		{ModeDraft, "update_label", `{"label":"Work","name":"Y"}`, "--mode full"},
		{ModeDraft, "delete_label", `{"label":"Work"}`, "--mode full"},
	}
	for _, tc := range cases {
		srv := NewServer(Options{Invoker: inv, Mode: tc.mode})
		resps := runRPC(t, srv, fmt.Sprintf(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, tc.tool, tc.args))
		raw, _ := json.Marshal(resps["7"].Result)
		if !strings.Contains(string(raw), `"isError":true`) || !strings.Contains(string(raw), "requires "+tc.need) {
			t.Errorf("%s in %s: %s", tc.tool, tc.mode, raw)
		}
	}
	for _, m := range []string{"ops.label", "labels.create", "labels.update", "labels.delete"} {
		if len(inv.callsTo(m)) != 0 {
			t.Errorf("%s reached the daemon from a mode that must not allow it", m)
		}
	}
}

func callTool(t *testing.T, srv *Server, name, args string) (callToolResult, string) {
	t.Helper()
	resps := runRPC(t, srv, fmt.Sprintf(`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, name, args))
	raw, _ := json.Marshal(resps["9"].Result)
	var res callToolResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("%s: %v (%s)", name, err, raw)
	}
	text := ""
	if len(res.Content) > 0 {
		text = res.Content[0].Text
	}
	return res, text
}

func TestLabelThreadsForwardsToOpsLabel(t *testing.T) {
	inv := &fakeInvoker{}
	srv := NewServer(Options{Invoker: inv, Mode: ModeDraft})
	if res, text := callTool(t, srv, "label_threads", `{"threadIds":[42,43],"add":["Work"],"remove":["Label_1"]}`); res.IsError {
		t.Fatal(text)
	}
	calls := inv.callsTo("ops.label")
	if len(calls) != 1 {
		t.Fatalf("ops.label calls = %v", calls)
	}
	if ids, _ := calls[0]["ids"].([]int); !slices.Equal(ids, []int{42, 43}) {
		t.Errorf("ids = %#v", calls[0]["ids"])
	}
	if add, _ := calls[0]["add"].([]string); !slices.Equal(add, []string{"Work"}) {
		t.Errorf("add = %#v", calls[0]["add"])
	}
	if res, _ := callTool(t, srv, "label_threads", `{"threadIds":[42]}`); !res.IsError {
		t.Error("label_threads without add/remove must fail")
	}
}

func TestListThreadsLabelFilterAndNames(t *testing.T) {
	inv := &fakeInvoker{}
	srv := NewServer(Options{Invoker: inv, Mode: ModeReadOnly})

	res, text := callTool(t, srv, "list_threads", `{"label":"auto/COMPRAS"}`)
	if res.IsError {
		t.Fatal(text)
	}
	lists := inv.callsTo("threads.list")
	if len(lists) != 1 || lists[0]["labelName"] != "Auto/Compras" {
		t.Fatalf("threads.list params = %v, want labelName", lists)
	}
	if _, inbox := lists[0]["inbox"]; inbox {
		t.Error("a label query without a view must not be limited to the inbox")
	}
	// System label INBOX and the unknown ID are dropped; only user label names remain.
	sc, _ := res.StructuredContent.(map[string]any)
	threads, _ := sc["threads"].([]any)
	if len(threads) != 1 {
		t.Fatalf("threads = %v", sc["threads"])
	}
	names, _ := threads[0].(map[string]any)["labelNames"].([]any)
	if len(names) != 1 || names[0] != "Auto/Compras" {
		t.Errorf("labelNames = %v, want [Auto/Compras]", names)
	}

	inv2 := &fakeInvoker{}
	srv2 := NewServer(Options{Invoker: inv2, Mode: ModeReadOnly})
	if res, text := callTool(t, srv2, "list_threads", `{"label":"Label_2","view":"inbox"}`); res.IsError {
		t.Fatal(text)
	}
	l := inv2.callsTo("threads.list")[0]
	if l["label"] != "Label_2" || l["account"] != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("ID filter params = %v", l)
	}
	if res, _ := callTool(t, srv2, "list_threads", `{"label":"nope"}`); !res.IsError {
		t.Error("unknown label must be an error")
	}

	inv3 := &fakeInvoker{}
	if res, text := callTool(t, NewServer(Options{Invoker: inv3, Mode: ModeReadOnly}), "search_mail", `{"query":"invoice","label":"Work","remote":false}`); res.IsError {
		t.Fatal(text)
	} else if !strings.Contains(text, `"labelNames"`) {
		t.Errorf("search_mail lacks labelNames: %s", text)
	}
	if l := inv3.callsTo("threads.list")[0]; l["labelName"] != "Work" {
		t.Errorf("search_mail params = %v", l)
	}
}

func TestManageLabelTools(t *testing.T) {
	inv := &fakeInvoker{}
	srv := NewServer(Options{Invoker: inv, Mode: ModeFull})

	if res, text := callTool(t, srv, "create_label", `{"name":"Created","colorBg":"#16a765","colorText":"#ffffff"}`); res.IsError {
		t.Fatal(text)
	}
	c := inv.callsTo("labels.create")[0]
	if c["account"] != "11111111-1111-1111-1111-111111111111" || c["name"] != "Created" || c["colorBg"] != "#16a765" {
		t.Errorf("create params = %v", c)
	}

	if res, text := callTool(t, srv, "update_label", `{"label":"work","name":"Job"}`); res.IsError {
		t.Fatal(text)
	}
	u := inv.callsTo("labels.update")[0]
	if u["id"] != "Label_2" || u["name"] != "Job" {
		t.Errorf("update params = %v", u)
	}
	if res, _ := callTool(t, srv, "update_label", `{"label":"work"}`); !res.IsError {
		t.Error("update without changes must fail")
	}
	if res, _ := callTool(t, srv, "update_label", `{"label":"work","colorBg":"#16a765"}`); !res.IsError {
		t.Error("colorBg without colorText must fail")
	}
	if res, text := callTool(t, srv, "update_label", `{"label":"work","removeColor":true}`); res.IsError {
		t.Fatal(text)
	}
	if calls := inv.callsTo("labels.update"); calls[len(calls)-1]["colorBg"] != "none" {
		t.Errorf("removeColor params = %v", calls[len(calls)-1])
	}

	if res, text := callTool(t, srv, "delete_label", `{"label":"Auto/Compras"}`); res.IsError {
		t.Fatal(text)
	}
	d := inv.callsTo("labels.delete")[0]
	if d["id"] != "Label_1" {
		t.Errorf("delete params = %v", d)
	}
	if res, _ := callTool(t, srv, "delete_label", `{"label":"ghost"}`); !res.IsError {
		t.Error("deleting an unknown label must fail")
	}
}

func TestDeleteLabelToolWarnsItIsIrreversible(t *testing.T) {
	srv := NewServer(Options{Invoker: &fakeInvoker{}, Mode: ModeFull})
	tool, ok := srv.byName["delete_label"]
	if !ok {
		t.Fatal("delete_label missing in full mode")
	}
	for _, phrase := range []string{"IRREVERSIBLE", "every thread", "NOT deleted"} {
		if !strings.Contains(tool.Description, phrase) {
			t.Errorf("description lacks %q: %s", phrase, tool.Description)
		}
	}
	if !tool.Annotations.DestructiveHint {
		t.Error("delete_label must carry the destructive hint")
	}
}
