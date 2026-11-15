package main

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/arqueon/dux-mail/core/ent"
	"github.com/arqueon/dux-mail/core/ent/account"
	"github.com/arqueon/dux-mail/core/ent/pendingop"
	"github.com/arqueon/dux-mail/core/internal/bus"
	"github.com/arqueon/dux-mail/core/internal/provider"
	"github.com/arqueon/dux-mail/core/internal/rules"
	"github.com/arqueon/dux-mail/core/internal/settings"
	dsync "github.com/arqueon/dux-mail/core/internal/sync"
	"github.com/arqueon/dux-mail/core/models"
	"github.com/arqueon/dux-mail/core/repo"
)

// labelProvider is a Gmail-like fake: CapLabels, an in-memory catalog and a
// recording of every call that reaches the provider.
type labelProvider struct {
	provider.Provider
	labels    []provider.Label
	created   []provider.Label
	updated   []provider.Label
	deleted   []string
	pages     []string // tokens LabelPage was asked for
	pageLabel string
	modified  int
}

func (p *labelProvider) Capabilities() provider.Capability {
	return provider.CapModifyFlags | provider.CapLabels
}
func (p *labelProvider) ListLabels(context.Context) ([]provider.Label, error) {
	return slices.Clone(p.labels), nil
}
func (p *labelProvider) CreateLabel(_ context.Context, l provider.Label) (provider.Label, error) {
	l.ID = "Label_new" + string(rune('0'+len(p.created)))
	p.created = append(p.created, l)
	p.labels = append(p.labels, l)
	return l, nil
}
func (p *labelProvider) UpdateLabel(_ context.Context, l provider.Label) (provider.Label, error) {
	p.updated = append(p.updated, l)
	for i := range p.labels {
		if p.labels[i].ID == l.ID {
			if l.Name != "" {
				p.labels[i].Name = l.Name
			}
			if l.ColorBg == "none" {
				p.labels[i].ColorBg, p.labels[i].ColorText = "", ""
			} else if l.ColorBg != "" {
				p.labels[i].ColorBg, p.labels[i].ColorText = l.ColorBg, l.ColorText
			}
			return p.labels[i], nil
		}
	}
	return provider.Label{}, nil
}
func (p *labelProvider) DeleteLabel(_ context.Context, id string) error {
	p.deleted = append(p.deleted, id)
	p.labels = slices.DeleteFunc(p.labels, func(l provider.Label) bool { return l.ID == id })
	return nil
}
func (p *labelProvider) ModifyLabels(context.Context, []string, []string, []string) error {
	p.modified++
	return nil
}
func (p *labelProvider) LabelPalette() []provider.LabelColor {
	return []provider.LabelColor{{Background: "#16a765", Text: "#ffffff"}}
}
func (p *labelProvider) LabelPage(_ context.Context, id, token string) (provider.Changes, string, error) {
	p.pageLabel = id
	p.pages = append(p.pages, token)
	return provider.Changes{Upserted: []provider.ThreadDelta{{ThreadID: "paged-" + id, LastMessage: time.Now().Unix(), Labels: []string{id}, MessageCount: 1}}}, "", nil
}

// plainProvider declares no labels capability.
type plainProvider struct{ provider.Provider }

func (plainProvider) Capabilities() provider.Capability { return provider.CapModifyFlags }

type labelRig struct {
	d    *daemon
	db   *ent.Client
	a, b *ent.Account
	pa   *labelProvider
	pb   *labelProvider
}

func newLabelRig(t *testing.T) *labelRig {
	t.Helper()
	ctx := context.Background()
	db, err := repo.OpenFile(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	b := bus.New()
	d := &daemon{
		db: db, repo: repo.New(db), bus: b,
		queue:    dsync.NewQueue(db, b, rules.DefaultPolicies),
		registry: newRegistry(nil, db),
		settings: settings.NewStore(filepath.Join(t.TempDir(), "settings.json")),
		reload:   make(chan struct{}, 1),
	}
	r := &labelRig{d: d, db: db}
	r.a = db.Account.Create().SetType(account.TypeGmail).SetEmail("a@example.org").SaveX(ctx)
	r.b = db.Account.Create().SetType(account.TypeGmail).SetEmail("b@example.org").SaveX(ctx)
	r.pa = &labelProvider{labels: []provider.Label{
		{ID: "Label_1", Name: "Auto/Compras", ColorBg: "#16a765", ColorText: "#ffffff"},
		{ID: "Label_2", Name: "Work"},
	}}
	r.pb = &labelProvider{labels: []provider.Label{{ID: "Label_9", Name: "auto/compras"}}}
	d.registry.providers[r.a.ID] = r.pa
	d.registry.providers[r.b.ID] = r.pb
	for _, id := range []uuid.UUID{r.a.ID, r.b.ID} {
		if err := d.refreshLabels(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func (r *labelRig) thread(t *testing.T, acct *ent.Account, key string, labels ...string) float64 {
	t.Helper()
	th := r.db.Thread.Create().SetAccount(acct).SetProviderThreadID(key).SetSubject(key).
		SetLastMessageAt(time.Now()).SetInInbox(true).SetLabels(labels).SaveX(context.Background())
	return float64(th.ID)
}

func (r *labelRig) threadLabels(t *testing.T, id float64) []string {
	t.Helper()
	return r.db.Thread.GetX(context.Background(), int(id)).Labels
}

func TestOpsLabelResolvesNamesPerAccount(t *testing.T) {
	ctx := context.Background()
	r := newLabelRig(t)
	ta := r.thread(t, r.a, "ta")
	tb := r.thread(t, r.b, "tb")

	// "AUTO/compras" exists in both accounts under different IDs; "Work" only
	// in account A, so account B skips it silently.
	if _, err := r.d.opsLabel(ctx, map[string]any{"ids": []any{ta, tb}, "add": []any{"AUTO/compras", "Work"}}); err != nil {
		t.Fatal(err)
	}
	if got := r.threadLabels(t, ta); !slices.Equal(got, []string{"Label_1", "Label_2"}) {
		t.Errorf("account A labels = %v", got)
	}
	if got := r.threadLabels(t, tb); !slices.Equal(got, []string{"Label_9"}) {
		t.Errorf("account B labels = %v, want its own ID for the same name", got)
	}
	ops := r.db.PendingOp.Query().WithAccount().AllX(ctx)
	if len(ops) != 2 {
		t.Fatalf("ops = %d, want one per account", len(ops))
	}
	for _, op := range ops {
		if op.OpType != pendingop.OpTypeLabel {
			t.Errorf("op type = %s", op.OpType)
		}
	}
}

func TestOpsLabelAcceptsIDsAndRemoves(t *testing.T) {
	ctx := context.Background()
	r := newLabelRig(t)
	ta := r.thread(t, r.a, "ta", "Label_1", "Label_2")
	if _, err := r.d.opsLabel(ctx, map[string]any{"id": ta, "remove": []any{"Label_1"}, "add": []any{}}); err != nil {
		t.Fatal(err)
	}
	if got := r.threadLabels(t, ta); !slices.Equal(got, []string{"Label_2"}) {
		t.Errorf("labels = %v", got)
	}
}

func TestOpsLabelRejectsBeforeQueueingAnything(t *testing.T) {
	ctx := context.Background()
	r := newLabelRig(t)
	ta := r.thread(t, r.a, "ta")
	tb := r.thread(t, r.b, "tb")
	imap := r.db.Account.Create().SetType(account.TypeImap).SetEmail("i@example.org").SaveX(ctx)
	r.d.registry.providers[imap.ID] = plainProvider{}
	ti := r.thread(t, imap, "ti")

	cases := map[string]map[string]any{
		"unknown ref":         {"ids": []any{ta, tb}, "add": []any{"Label_1", "Nope"}},
		"unknown id":          {"ids": []any{ta}, "add": []any{"Label_404"}},
		"add and remove":      {"ids": []any{ta}, "add": []any{"Work"}, "remove": []any{"Label_2"}},
		"nothing to do":       {"ids": []any{ta}},
		"no capability":       {"ids": []any{ta, ti}, "add": []any{"Work"}},
		"missing selection":   {"add": []any{"Work"}},
		"selection not found": {"ids": []any{ta, float64(99999)}, "add": []any{"Work"}},
	}
	for name, params := range cases {
		if _, err := r.d.opsLabel(ctx, params); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if n := r.db.PendingOp.Query().CountX(ctx); n != 0 {
		t.Fatalf("%d ops queued by rejected calls", n)
	}
	if got := r.threadLabels(t, ta); len(got) != 0 {
		t.Errorf("optimistic labels leaked: %v", got)
	}
}

func TestThreadsListByLabelNameAndAccountsCapabilities(t *testing.T) {
	ctx := context.Background()
	r := newLabelRig(t)
	ta := r.thread(t, r.a, "ta", "Label_1")
	tb := r.thread(t, r.b, "tb", "Label_9")
	r.thread(t, r.a, "tc", "Label_2")

	rows, err := r.d.repo.ListThreads(ctx, repo.ThreadFilter{LabelName: "Auto/Compras"})
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	ids := []int{rows[0].ID, rows[1].ID}
	slices.Sort(ids)
	if !slices.Equal(ids, []int{int(ta), int(tb)}) {
		t.Errorf("ids = %v", ids)
	}

	imap := r.db.Account.Create().SetType(account.TypeImap).SetEmail("i@example.org").SaveX(ctx)
	r.d.registry.providers[imap.ID] = plainProvider{}
	views, err := r.d.accountViews(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, v := range views {
		got[v.Email] = v.Capabilities
	}
	if !slices.Equal(got["a@example.org"], []string{"labels"}) {
		t.Errorf("gmail capabilities = %v", got["a@example.org"])
	}
	if c := got["i@example.org"]; c == nil || len(c) != 0 {
		t.Errorf("imap capabilities = %#v, want empty non-nil", c)
	}
}

func TestLabelCRUDRefreshesCatalogAndEmitsEvents(t *testing.T) {
	ctx := context.Background()
	r := newLabelRig(t)
	_, events := r.d.bus.Subscribe(32)

	raw, err := r.d.labelsCreate(ctx, map[string]any{"account": r.a.ID.String(), "name": "New/One", "colorBg": "#16a765", "colorText": "#ffffff"})
	if err != nil {
		t.Fatal(err)
	}
	v := raw.(models.LabelView)
	if v.Name != "New/One" || v.ColorBg != "#16a765" || v.AccountID != r.a.ID.String() || v.Monitored {
		t.Errorf("created view = %+v", v)
	}
	raw, err = r.d.labelsUpdate(ctx, map[string]any{"account": r.a.ID.String(), "id": v.ID, "name": "New/Renamed"})
	if err != nil || raw.(models.LabelView).Name != "New/Renamed" {
		t.Fatalf("rename: %v %v", raw, err)
	}
	if len(r.pa.updated) != 1 || r.pa.updated[0].ColorBg != "" {
		t.Errorf("rename must not touch colors: %+v", r.pa.updated)
	}
	raw, err = r.d.labelsUpdate(ctx, map[string]any{"account": r.a.ID.String(), "id": v.ID, "colorBg": "", "colorText": ""})
	if err != nil || raw.(models.LabelView).ColorBg != "" {
		t.Fatalf("clear color: %v %v", raw, err)
	}
	if r.pa.updated[1].ColorBg != "none" {
		t.Errorf("clear must reach the provider as none, got %+v", r.pa.updated[1])
	}
	if _, err := r.d.labelsUpdate(ctx, map[string]any{"account": r.a.ID.String(), "id": "Label_404", "name": "x"}); err == nil {
		t.Error("unknown label id must be rejected")
	}

	changed := 0
	for _, ev := range collectEvents(events) {
		if ev.Topic == "labels.changed" && ev.Payload["accountId"] == r.a.ID.String() {
			changed++
		}
	}
	if changed < 3 {
		t.Errorf("labels.changed events = %d, want at least one per mutation", changed)
	}

	pal, err := r.d.labelsPalette(ctx, map[string]any{"account": r.a.ID.String()})
	if err != nil || len(pal.([]provider.LabelColor)) != 1 {
		t.Fatalf("palette = %v, %v", pal, err)
	}
}

func collectEvents(ch <-chan bus.Event) []bus.Event {
	var out []bus.Event
	for {
		select {
		case ev := <-ch:
			out = append(out, ev)
		default:
			return out
		}
	}
}

func TestLabelDeleteKeepsMailAndCleansLocalState(t *testing.T) {
	ctx := context.Background()
	r := newLabelRig(t)
	r.db.Account.UpdateOne(r.a).SetConfig(map[string]any{"labels": []any{"Label_1", "Label_2"}}).ExecX(ctx)
	th := r.thread(t, r.a, "ta", "Label_1", "Label_2")

	if _, err := r.d.labelsDelete(ctx, map[string]any{"account": r.a.ID.String(), "id": "Label_1"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.pa.deleted, []string{"Label_1"}) || r.pa.modified != 0 {
		t.Errorf("deleted=%v modified=%d", r.pa.deleted, r.pa.modified)
	}
	if n := r.db.Thread.Query().CountX(ctx); n != 1 {
		t.Fatalf("threads = %d: deleting a label must never delete mail", n)
	}
	if got := r.threadLabels(t, th); !slices.Equal(got, []string{"Label_2"}) {
		t.Errorf("thread labels = %v", got)
	}
	cfg := r.db.Account.GetX(ctx, r.a.ID).Config
	if got := repo.MonitoredLabels(r.db.Account.GetX(ctx, r.a.ID)); !slices.Equal(got, []string{"Label_2"}) {
		t.Errorf("monitored = %v (config %v)", got, cfg)
	}
	views, _ := r.d.repo.Labels(ctx, &r.a.ID)
	for _, v := range views {
		if v.ID == "Label_1" {
			t.Error("deleted label still in the catalog")
		}
	}
	if r.db.PendingOp.Query().CountX(ctx) != 0 {
		t.Error("delete must not queue thread ops")
	}
}

func TestSetMonitoredLabelsPersistsAndForcesResync(t *testing.T) {
	ctx := context.Background()
	r := newLabelRig(t)
	r.db.Account.UpdateOne(r.a).SetSyncCursor("labels-v2:123").ExecX(ctx)

	if _, err := r.d.setMonitoredLabels(ctx, map[string]any{"account": r.a.ID.String(), "labels": []any{"Label_2", "Label_2"}}); err != nil {
		t.Fatal(err)
	}
	a := r.db.Account.GetX(ctx, r.a.ID)
	if got := repo.MonitoredLabels(a); !slices.Equal(got, []string{"Label_2"}) {
		t.Errorf("monitored = %v", got)
	}
	if a.SyncCursor != "" {
		t.Errorf("sync cursor = %q, want cleared for a full resync", a.SyncCursor)
	}
	views, _ := r.d.repo.Labels(ctx, &r.a.ID)
	for _, v := range views {
		if v.Monitored != (v.ID == "Label_2") {
			t.Errorf("view %+v has the wrong monitored flag", v)
		}
	}
	if _, err := r.d.setMonitoredLabels(ctx, map[string]any{"account": r.a.ID.String(), "labels": []any{"Label_404"}}); err == nil {
		t.Error("unknown label id must be rejected")
	}
	if got := repo.MonitoredLabels(r.db.Account.GetX(ctx, r.a.ID)); !slices.Equal(got, []string{"Label_2"}) {
		t.Errorf("rejected call changed the config: %v", got)
	}
	select {
	case <-r.d.reload:
	default:
		t.Error("engine reload was not requested")
	}
}

func TestFetchLabelPagesEachAccountByItsOwnID(t *testing.T) {
	ctx := context.Background()
	r := newLabelRig(t)

	raw, err := r.d.fetchLabel(ctx, map[string]any{"labelName": "Auto/Compras"})
	if err != nil {
		t.Fatal(err)
	}
	res := raw.(map[string]any)
	if res["ingested"] != 2 || res["supported"] != 2 {
		t.Fatalf("result = %v", res)
	}
	if r.pa.pageLabel != "Label_1" || r.pb.pageLabel != "Label_9" {
		t.Errorf("labels asked: a=%s b=%s", r.pa.pageLabel, r.pb.pageLabel)
	}
	rows, _ := r.d.repo.ListThreads(ctx, repo.ThreadFilter{LabelName: "auto/compras"})
	if len(rows) != 2 {
		t.Errorf("backfilled threads visible by name = %d, want 2", len(rows))
	}

	// account + label ID form, and its validation.
	if _, err := r.d.fetchLabel(ctx, map[string]any{"account": r.a.ID.String(), "label": "Label_2"}); err != nil {
		t.Fatal(err)
	}
	if r.pa.pageLabel != "Label_2" {
		t.Errorf("pageLabel = %s", r.pa.pageLabel)
	}
	for _, bad := range []map[string]any{
		{}, {"label": "Label_2"}, {"account": r.a.ID.String(), "label": "Label_404"},
	} {
		if _, err := r.d.fetchLabel(ctx, bad); err == nil {
			t.Errorf("fetchLabel(%v) must fail", bad)
		}
	}
}

func TestRefreshLabelsIfStaleHonoursTenMinuteWindow(t *testing.T) {
	r := newLabelRig(t)
	r.pa.labels = append(r.pa.labels, provider.Label{ID: "Label_3", Name: "Later"})
	r.d.refreshLabelsIfStale(context.Background(), r.a.ID) // refreshed just now by the rig: no-op
	time.Sleep(50 * time.Millisecond)
	views, _ := r.d.repo.Labels(context.Background(), &r.a.ID)
	if len(views) != 2 {
		t.Fatalf("fresh catalog was refreshed again: %+v", views)
	}
	r.d.labelsAt.Store(r.a.ID, time.Now().Add(-11*time.Minute))
	r.d.refreshLabelsIfStale(context.Background(), r.a.ID)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v, _ := r.d.repo.Labels(context.Background(), &r.a.ID); len(v) == 3 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("stale catalog was not refreshed after a sync")
}
