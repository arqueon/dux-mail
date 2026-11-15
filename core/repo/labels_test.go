package repo

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestReplaceLabelsReportsChangesAndPrunes(t *testing.T) {
	ctx := context.Background()
	c := searchDB(t)
	a := c.Account.Create().SetType("gmail").SetEmail("l1@example.org").SaveX(ctx)
	r := New(c)

	first := []CatalogLabel{{ID: "Label_1", Name: "Auto/Compras", ColorBg: "#16a765", ColorText: "#ffffff"}, {ID: "Label_2", Name: "Work"}}
	if changed, err := r.ReplaceLabels(ctx, a.ID, first); err != nil || !changed {
		t.Fatalf("first replace changed=%v err=%v", changed, err)
	}
	if changed, err := r.ReplaceLabels(ctx, a.ID, first); err != nil || changed {
		t.Fatalf("identical replace changed=%v err=%v, want no change", changed, err)
	}
	renamed := []CatalogLabel{{ID: "Label_1", Name: "Auto/Shopping", ColorBg: "#16a765", ColorText: "#ffffff"}}
	if changed, err := r.ReplaceLabels(ctx, a.ID, renamed); err != nil || !changed {
		t.Fatalf("rename+remove changed=%v err=%v", changed, err)
	}
	got, err := r.Labels(ctx, &a.ID)
	if err != nil || len(got) != 1 || got[0].Name != "Auto/Shopping" || got[0].AccountID != a.ID.String() {
		t.Fatalf("labels = %+v err=%v", got, err)
	}
}

func TestLabelsMonitoredFlagComesFromAccountConfig(t *testing.T) {
	ctx := context.Background()
	c := searchDB(t)
	a := c.Account.Create().SetType("gmail").SetEmail("l2@example.org").SetConfig(map[string]any{"labels": []any{"Label_2"}}).SaveX(ctx)
	r := New(c)
	if _, err := r.ReplaceLabels(ctx, a.ID, []CatalogLabel{{ID: "Label_1", Name: "A"}, {ID: "Label_2", Name: "B"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Labels(ctx, nil)
	if len(got) != 2 || got[0].Monitored || !got[1].Monitored {
		t.Fatalf("labels = %+v, want only B monitored", got)
	}
}

func TestDeletingAccountCascadesLabels(t *testing.T) {
	ctx := context.Background()
	c := searchDB(t)
	a := c.Account.Create().SetType("gmail").SetEmail("l3@example.org").SaveX(ctx)
	r := New(c)
	_, _ = r.ReplaceLabels(ctx, a.ID, []CatalogLabel{{ID: "Label_1", Name: "A"}})
	c.Account.DeleteOneID(a.ID).ExecX(ctx)
	if n := c.Label.Query().CountX(ctx); n != 0 {
		t.Fatalf("labels left after account delete: %d", n)
	}
}

func TestListThreadsByLabelNameMergesAccounts(t *testing.T) {
	ctx := context.Background()
	c := searchDB(t)
	a := c.Account.Create().SetType("gmail").SetEmail("l4a@example.org").SaveX(ctx)
	b := c.Account.Create().SetType("gmail").SetEmail("l4b@example.org").SaveX(ctx)
	r := New(c)
	_, _ = r.ReplaceLabels(ctx, a.ID, []CatalogLabel{{ID: "Label_7", Name: "Auto/Compras"}})
	_, _ = r.ReplaceLabels(ctx, b.ID, []CatalogLabel{{ID: "Label_99", Name: "auto/compras"}, {ID: "Label_7", Name: "Other"}})

	add := func(key string, at int64, labels []string, owner string) int {
		acct := a
		if owner == "b" {
			acct = b
		}
		th := addSearchThread(t, c, acct, key, "s "+key, "", "", "")
		return c.Thread.UpdateOne(th).SetLastMessageAt(time.Unix(at, 0)).SetInInbox(false).SetLabels(labels).SaveX(ctx).ID
	}
	ta := add("a1", 30, []string{"Label_7"}, "a")
	tb := add("b1", 20, []string{"Label_99"}, "b")
	add("b-other", 10, []string{"Label_7"}, "b") // same ID, different label in account b
	add("a-none", 5, []string{"Label_8"}, "a")

	rows, err := r.ListThreads(ctx, ThreadFilter{LabelName: "Auto/Compras"})
	if err != nil {
		t.Fatal(err)
	}
	if got := resultIDs(rows); !reflect.DeepEqual(got, []int{ta, tb}) {
		t.Fatalf("by name = %v, want [%d %d]", got, ta, tb)
	}
	rows, _ = r.ListThreads(ctx, ThreadFilter{LabelName: "Auto/Compras", AccountID: &b.ID})
	if got := resultIDs(rows); !reflect.DeepEqual(got, []int{tb}) {
		t.Fatalf("by name+account = %v, want [%d]", got, tb)
	}
	rows, err = r.ListThreads(ctx, ThreadFilter{LabelName: "nope"})
	if err != nil || len(rows) != 0 {
		t.Fatalf("unknown name = %v, %v", rows, err)
	}
}
