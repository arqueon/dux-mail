package sync

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/arqueon/dux-mail/core/ent/pendingop"
	"github.com/arqueon/dux-mail/core/errdefs"
	"github.com/arqueon/dux-mail/core/internal/provider"
	"github.com/arqueon/dux-mail/core/internal/rules"
)

func labelOp(r *rig, add, remove []string, ids ...string) Op {
	return Op{AccountID: r.acct.ID, Type: OpLabel, ThreadIDs: ids,
		Payload: OpPayload{Labels: &LabelsPayload{Add: add, Remove: remove}}}
}

func setLabels(t *testing.T, r *rig, id string, labels ...string) {
	t.Helper()
	th := r.reloadThread(t, id)
	if _, err := th.Update().SetLabels(labels).Save(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLabelOpAppliesOptimisticallyAndSnapshotsPrev(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", inInbox: true})
	setLabels(t, r, "t1", "Label_1", "Label_2")

	if err := r.queue.Enqueue(context.Background(), labelOp(r, []string{"Label_3", "Label_1"}, []string{"Label_2"}, "t1")); err != nil {
		t.Fatal(err)
	}
	if got := r.reloadThread(t, "t1").Labels; !slices.Equal(got, []string{"Label_1", "Label_3"}) {
		t.Errorf("optimistic labels = %v, want [Label_1 Label_3]", got)
	}
	ops := r.ops(t)
	if len(ops) != 1 || ops[0].OpType != pendingop.OpTypeLabel {
		t.Fatalf("ops = %+v, want one label op", ops)
	}
	op, err := opFromRow(ops[0], r.acct.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(op.Payload.Prev["t1"].Labels, []string{"Label_1", "Label_2"}) {
		t.Errorf("prev labels = %v", op.Payload.Prev["t1"].Labels)
	}
	if op.Payload.Labels == nil || !slices.Equal(op.Payload.Labels.Remove, []string{"Label_2"}) {
		t.Errorf("payload = %+v", op.Payload.Labels)
	}
}

func TestLabelOpWithoutPayloadIsRejected(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", inInbox: true})
	err := r.queue.Enqueue(context.Background(), Op{AccountID: r.acct.ID, Type: OpLabel, ThreadIDs: []string{"t1"}})
	if err == nil {
		t.Fatal("label op without payload must not be queued")
	}
	if len(r.ops(t)) != 0 {
		t.Error("rejected enqueue must leave no pending row")
	}
}

func TestExecutorLabelCallsProvider(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", inInbox: true})
	ctx := context.Background()

	if err := r.queue.Enqueue(ctx, labelOp(r, []string{"Label_1"}, nil, "t1")); err != nil {
		t.Fatal(err)
	}
	r.exec.Drain(ctx)

	if len(r.prov.labelCalls) != 1 {
		t.Fatalf("label calls = %d, want 1", len(r.prov.labelCalls))
	}
	c := r.prov.labelCalls[0]
	if !slices.Equal(c.ids, []string{"t1"}) || !slices.Equal(c.add, []string{"Label_1"}) || len(c.remove) != 0 {
		t.Errorf("call = %+v", c)
	}
	if ops := r.ops(t); ops[0].State != pendingop.StateDone {
		t.Errorf("op state = %s, want done", ops[0].State)
	}
}

func TestExecutorCoalescesLabelOpsByPayload(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	for _, id := range []string{"t1", "t2", "t3"} {
		mkThread(t, r.db, r.acct, threadOpts{id: id, inInbox: true})
	}
	ctx := context.Background()
	// t1+t2 add Label_1; t3 removes Label_9: same op type, different payloads.
	for _, op := range []Op{
		labelOp(r, []string{"Label_1"}, nil, "t1"),
		labelOp(r, []string{"Label_1"}, nil, "t2"),
		labelOp(r, nil, []string{"Label_9"}, "t3"),
	} {
		if err := r.queue.Enqueue(ctx, op); err != nil {
			t.Fatal(err)
		}
	}
	r.exec.Drain(ctx)

	if len(r.prov.labelCalls) != 2 {
		t.Fatalf("label calls = %+v, want 2 (one per distinct payload)", r.prov.labelCalls)
	}
	first, second := r.prov.labelCalls[0], r.prov.labelCalls[1]
	if !slices.Equal(first.ids, []string{"t1", "t2"}) || !slices.Equal(first.add, []string{"Label_1"}) {
		t.Errorf("first call = %+v", first)
	}
	if !slices.Equal(second.ids, []string{"t3"}) || !slices.Equal(second.remove, []string{"Label_9"}) {
		t.Errorf("second call = %+v", second)
	}
}

func TestLabelOpPermanentFailureRevertsAndPublishesOpFailed(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", inInbox: true})
	setLabels(t, r, "t1", "Label_1")
	r.prov.fail("ModifyLabels", errdefs.Wrap(errdefs.KindPermanent, errors.New("label gone")))
	ctx := context.Background()

	_, events := r.bus.Subscribe(32)
	if err := r.queue.Enqueue(ctx, labelOp(r, []string{"Label_2"}, []string{"Label_1"}, "t1")); err != nil {
		t.Fatal(err)
	}
	if got := r.reloadThread(t, "t1").Labels; !slices.Equal(got, []string{"Label_2"}) {
		t.Fatalf("optimistic labels = %v", got)
	}
	r.exec.Drain(ctx)

	if got := r.reloadThread(t, "t1").Labels; !slices.Equal(got, []string{"Label_1"}) {
		t.Errorf("labels after failed op = %v, want revert to [Label_1]", got)
	}
	if ops := r.ops(t); ops[0].State != pendingop.StateFailed {
		t.Errorf("op state = %s, want failed", ops[0].State)
	}
	var failed bool
	for _, ev := range collect(events, 8, 200e6) {
		if ev.Topic == "op.failed" && ev.Payload["opType"] == string(OpLabel) {
			failed = true
		}
	}
	if !failed {
		t.Error("op.failed was not published for the label op")
	}
}

func TestLabelBatchRevertUnwindsNewestFirst(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", inInbox: true})
	setLabels(t, r, "t1", "Label_0")
	r.prov.fail("ModifyLabels", errdefs.Wrap(errdefs.KindPermanent, errors.New("nope")))
	ctx := context.Background()

	for _, op := range []Op{
		labelOp(r, []string{"Label_1"}, nil, "t1"),
		labelOp(r, []string{"Label_2"}, nil, "t1"),
	} {
		if err := r.queue.Enqueue(ctx, op); err != nil {
			t.Fatal(err)
		}
	}
	r.exec.Drain(ctx)
	if got := r.reloadThread(t, "t1").Labels; !slices.Equal(got, []string{"Label_0"}) {
		t.Errorf("labels = %v, want the original [Label_0]", got)
	}
}

type noLabelsProvider struct{ *fakeProvider }

func (noLabelsProvider) Capabilities() provider.Capability { return provider.CapModifyFlags }

func TestLabelOpOnProviderWithoutCapabilityFailsPermanently(t *testing.T) {
	r := newRig(t, rules.DefaultPolicies())
	r.exec.registry = regMap{r.acct.ID: noLabelsProvider{r.prov}}
	mkThread(t, r.db, r.acct, threadOpts{id: "t1", inInbox: true})
	ctx := context.Background()

	if err := r.queue.Enqueue(ctx, labelOp(r, []string{"Label_1"}, nil, "t1")); err != nil {
		t.Fatal(err)
	}
	r.exec.Drain(ctx)
	if len(r.prov.labelCalls) != 0 {
		t.Fatal("provider without CapLabels must never be asked to modify labels")
	}
	if ops := r.ops(t); ops[0].State != pendingop.StateFailed {
		t.Errorf("op state = %s, want failed without retries", ops[0].State)
	}
	if got := r.reloadThread(t, "t1").Labels; len(got) != 0 {
		t.Errorf("labels = %v, want reverted to none", got)
	}
}
