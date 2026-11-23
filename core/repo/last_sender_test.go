package repo

import (
	"context"
	"testing"
	"time"
)

func TestLastSenderReflectsNewestMessageAcrossViews(t *testing.T) {
	ctx := context.Background()
	db := searchDB(t)
	acct := db.Account.Create().SetType("gmail").SetEmail("ruben@example.org").SaveX(ctx)

	// Round-trip A -> B -> A: Participants records first-appearance order,
	// so Ruben ends up last even though Melanie sent the newest message.
	roundTrip := db.Thread.Create().
		SetAccount(acct).
		SetProviderThreadID("round-trip").
		SetSubject("Duda").
		SetLastMessageAt(time.Unix(300, 0)).
		SetParticipants([]string{"Melanie <m@example.org>", "Ruben <ruben@example.org>"}).
		SetMessageCount(3).
		SaveX(ctx)

	db.Message.Create().SetThread(roundTrip).SetProviderMessageID("m1").
		SetFrom("Melanie <m@example.org>").SetDate(time.Unix(100, 0)).SaveX(ctx)
	db.Message.Create().SetThread(roundTrip).SetProviderMessageID("m2").
		SetFrom("Ruben <ruben@example.org>").SetIsSent(true).SetTo([]string{"Melanie <m@example.org>"}).
		SetDate(time.Unix(200, 0)).SaveX(ctx)
	db.Message.Create().SetThread(roundTrip).SetProviderMessageID("m3").
		SetFrom("Melanie <m@example.org>").SetDate(time.Unix(300, 0)).SaveX(ctx)

	// Thread with 0 cached message rows must still list cleanly with empty LastSender.
	empty := db.Thread.Create().
		SetAccount(acct).
		SetProviderThreadID("no-messages").
		SetSubject("Empty").
		SetLastMessageAt(time.Unix(150, 0)).
		SetParticipants([]string{"Ada <ada@example.org>"}).
		SetMessageCount(1).
		SaveX(ctx)

	r := New(db)
	rows, err := r.ListThreads(ctx, ThreadFilter{InboxOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(rows))
	}
	if rows[0].ID != roundTrip.ID || rows[0].LastSender != "Melanie <m@example.org>" {
		t.Fatalf("roundTrip summary = %+v, want LastSender Melanie <m@example.org>", rows[0])
	}
	if rows[1].ID != empty.ID || rows[1].LastSender != "" {
		t.Fatalf("empty summary = %+v, want empty LastSender", rows[1])
	}

	detail, err := r.GetThread(ctx, roundTrip.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.LastSender != "Melanie <m@example.org>" {
		t.Fatalf("GetThread LastSender = %q, want Melanie <m@example.org>", detail.LastSender)
	}

	// Sent view continues exposing outgoing recipients in Participants and leaves LastSender empty.
	sentRows, err := r.ListThreads(ctx, ThreadFilter{SentOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(sentRows) != 1 || sentRows[0].LastSender != "" {
		t.Fatalf("SentOnly rows = %+v, want 1 row with empty LastSender", sentRows)
	}
	if len(sentRows[0].Participants) != 1 || sentRows[0].Participants[0] != "Melanie <m@example.org>" {
		t.Fatalf("SentOnly Participants = %v, want outgoing recipient", sentRows[0].Participants)
	}
}
