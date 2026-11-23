package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	zkeyring "github.com/zalando/go-keyring"

	"github.com/arqueon/dux-mail/core/config"
	"github.com/arqueon/dux-mail/core/ent/account"
	"github.com/arqueon/dux-mail/core/internal/bus"
	"github.com/arqueon/dux-mail/core/internal/drafts"
	"github.com/arqueon/dux-mail/core/internal/oauth"
	"github.com/arqueon/dux-mail/core/internal/provider"
	"github.com/arqueon/dux-mail/core/internal/rules"
	dsync "github.com/arqueon/dux-mail/core/internal/sync"
	"github.com/arqueon/dux-mail/core/models"
	"github.com/arqueon/dux-mail/core/repo"
)

type fakeAttProvider struct {
	provider.Provider
	data map[string][]byte
}

func (f *fakeAttProvider) FetchAttachment(_ context.Context, messageID, attachmentID, filename string) ([]byte, error) {
	return f.data[messageID+"|"+attachmentID+"|"+filename], nil
}

func TestSnoozedFilterAndUnsnooze(t *testing.T) {
	ctx := context.Background()
	db, err := repo.OpenFile(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	b := bus.New()
	r := repo.New(db)
	d := &daemon{db: db, repo: r, bus: b, queue: dsync.NewQueue(db, b, rules.DefaultPolicies)}

	acct := db.Account.Create().SetType(account.TypeGmail).SetEmail("u@example.org").SaveX(ctx)
	later := time.Now().Add(4 * time.Hour).UTC().Truncate(time.Second)
	sooner := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)

	tLater := db.Thread.Create().SetAccount(acct).SetProviderThreadID("th-later").SetSubject("Later").
		SetLastMessageAt(time.Now()).SetInInbox(false).SetSnoozedUntil(later).SaveX(ctx)
	tSooner := db.Thread.Create().SetAccount(acct).SetProviderThreadID("th-sooner").SetSubject("Sooner").
		SetLastMessageAt(time.Now()).SetInInbox(false).SetSnoozedUntil(sooner).SaveX(ctx)
	_ = db.Thread.Create().SetAccount(acct).SetProviderThreadID("th-inbox").SetSubject("Inbox").
		SetLastMessageAt(time.Now()).SetInInbox(true).SaveX(ctx)

	snoozed, err := r.ListThreads(ctx, repo.ThreadFilter{SnoozedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(snoozed) != 2 || snoozed[0].ID != tSooner.ID || snoozed[1].ID != tLater.ID {
		t.Fatalf("expected snoozed threads ordered by wake time, got %+v", snoozed)
	}

	if err := d.enqueueThreadOp(ctx, map[string]any{"id": float64(tSooner.ID)}, dsync.OpSnoozeWake, dsync.OpPayload{}); err != nil {
		t.Fatal(err)
	}
	woken := db.Thread.GetX(ctx, tSooner.ID)
	if !woken.InInbox || woken.SnoozedUntil != nil {
		t.Fatalf("expected thread woken into inbox with cleared SnoozedUntil, got %+v", woken)
	}
}

func TestAttachmentCacheAndSaveAs(t *testing.T) {
	ctx := context.Background()
	db, err := repo.OpenFile(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := repo.New(db)
	reg := newRegistry(&config.Config{}, db)
	acct := db.Account.Create().SetType(account.TypeGmail).SetEmail("u@example.org").SaveX(ctx)
	reg.providers[acct.ID] = &fakeAttProvider{
		data: map[string][]byte{"m-1|att-1|report.pdf": []byte("%PDF-1.7 test payload")},
	}
	d := &daemon{db: db, repo: r, registry: reg}

	th := db.Thread.Create().SetAccount(acct).SetProviderThreadID("th-1").SetSubject("Doc").SetLastMessageAt(time.Now()).SaveX(ctx)
	db.Message.Create().SetThread(th).SetProviderMessageID("m-1").SetFrom("a@b.org").SetDate(time.Now()).
		SetAttachments([]provider.AttachmentMeta{{Filename: "report.pdf", MimeType: "application/pdf", Size: 21, AttachmentID: "att-1"}}).
		SaveX(ctx)

	cachedPath, cleanName, err := d.ensureAttachmentCached(ctx, map[string]any{
		"threadId":     float64(th.ID),
		"messageId":    "m-1",
		"attachmentId": "att-1",
		"filename":     "../report.pdf",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(cachedPath))
	if cleanName != "report.pdf" {
		t.Fatalf("expected sanitized filename report.pdf, got %q", cleanName)
	}
	st, err := os.Stat(cachedPath)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 cached file, got %v (err=%v)", st, err)
	}

	destDir := t.TempDir()
	out1 := uniqueDestPath(destDir, cleanName)
	if err := os.WriteFile(out1, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	out2 := uniqueDestPath(destDir, cleanName)
	if filepath.Base(out2) != "report (1).pdf" {
		t.Fatalf("expected collision-safe filename report (1).pdf, got %q", filepath.Base(out2))
	}
}

func TestSetupGuideReflectsSavedDefaultClient(t *testing.T) {
	zkeyring.MockInit()
	t.Setenv("DMAIL_OAUTH_CLIENTS_PATH", filepath.Join(t.TempDir(), "oauth-clients.json"))
	if err := oauth.SaveDefaultClientCreds("google", oauth.ClientCreds{ClientID: "g-cid", ClientSecret: "g-sec"}); err != nil {
		t.Fatal(err)
	}
	c, err := oauth.ResolveGoogleClient(oauth.ClientCreds{}, "", "", uuid.Nil.String())
	if err != nil || c.ClientID != "g-cid" {
		t.Fatalf("expected g-cid, got %+v (err=%v)", c, err)
	}
}

func TestHydrateOutgoingAttachmentsFromRemoteDraft(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	ctx := context.Background()
	db, err := repo.OpenFile(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := repo.New(db)
	reg := newRegistry(&config.Config{}, db)
	acct := db.Account.Create().SetType(account.TypeGmail).SetEmail("u@example.org").SaveX(ctx)
	payload := []byte("%PDF-1.7 remote draft attachment")
	reg.providers[acct.ID] = &fakeAttProvider{
		data: map[string][]byte{
			"draft-m1|draft-att1|report.pdf": payload,
			"draft-m2|att-empty|empty.pdf":   {},
		},
	}
	store := drafts.NewStore("")
	if _, err := store.Save(models.DraftView{
		ID:                "d-1",
		AccountID:         acct.ID.String(),
		ProviderDraftID:   "gd-1",
		ProviderMessageID: "draft-m1",
		Attachments: []models.AttachmentView{
			{
				MessageID:    "draft-m1",
				AttachmentID: "draft-att1",
				Filename:     "report.pdf",
				MimeType:     "application/pdf",
				Size:         int64(len(payload)),
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	d := &daemon{db: db, repo: r, registry: reg, drafts: store}

	// Simulate an outgoing send/save where caller only carried draftId + filename (empty Path).
	provAtts, viewAtts, err := d.hydrateOutgoingAttachments(ctx, acct.ID, "d-1",
		[]provider.AttachmentFile{{Filename: "report.pdf", MimeType: "application/pdf"}},
		nil,
	)
	if err != nil {
		t.Fatalf("hydrateOutgoingAttachments failed: %v", err)
	}
	if len(provAtts) != 1 || provAtts[0].Path == "" || provAtts[0].Size != int64(len(payload)) {
		t.Fatalf("expected hydrated local cache path and size, got %+v", provAtts)
	}
	if len(viewAtts) != 1 || viewAtts[0].Path != provAtts[0].Path || viewAtts[0].MessageID != "draft-m1" || viewAtts[0].AttachmentID != "draft-att1" {
		t.Fatalf("expected hydrated view attachment metadata, got %+v", viewAtts)
	}
	gotBytes, err := os.ReadFile(provAtts[0].Path)
	if err != nil || !bytes.Equal(gotBytes, payload) {
		t.Fatalf("expected cached bytes %q, got %q (err=%v)", payload, gotBytes, err)
	}

	// Fail closed when attachment has neither a local file nor remote metadata.
	if _, _, err := d.hydrateOutgoingAttachments(ctx, acct.ID, "", []provider.AttachmentFile{{Filename: "orphan.pdf"}}, nil); err == nil {
		t.Fatal("expected error for unresolved attachment without path or remote ID")
	}

	// Fail closed when local file is missing on disk.
	missing := filepath.Join(t.TempDir(), "gone.pdf")
	if _, _, err := d.hydrateOutgoingAttachments(ctx, acct.ID, "", []provider.AttachmentFile{{Filename: "gone.pdf", Path: missing}}, nil); err == nil {
		t.Fatal("expected error for missing file on disk")
	}

	// Fail closed when provider returns empty 0-byte payload.
	if _, _, err := d.hydrateOutgoingAttachments(ctx, acct.ID, "", []provider.AttachmentFile{{Filename: "empty.pdf", MessageID: "draft-m2", AttachmentID: "att-empty"}}, nil); err == nil || !strings.Contains(err.Error(), "has no payload") {
		t.Fatalf("expected empty-payload error, got %v", err)
	}
}

func TestEnsureAttachmentCachedLocalPathAndDraft(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	ctx := context.Background()
	db, err := repo.OpenFile(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := repo.New(db)
	reg := newRegistry(&config.Config{}, db)
	acct := db.Account.Create().SetType(account.TypeGmail).SetEmail("u@example.org").SaveX(ctx)
	docxPayload := []byte("PK\x03\x04fake-docx")
	reg.providers[acct.ID] = &fakeAttProvider{
		data: map[string][]byte{
			"dm-2|da-2|draft.docx": docxPayload,
		},
	}
	store := drafts.NewStore("")
	if _, err := store.Save(models.DraftView{
		ID:                "d-2",
		AccountID:         acct.ID.String(),
		ProviderMessageID: "dm-2",
		Attachments: []models.AttachmentView{
			{MessageID: "dm-2", AttachmentID: "da-2", Filename: "draft.docx"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	d := &daemon{db: db, repo: r, registry: reg, drafts: store}

	localPath := filepath.Join(t.TempDir(), "local.txt")
	if err := os.WriteFile(localPath, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	gotPath, cleanName, err := d.ensureAttachmentCached(ctx, map[string]any{
		"path":     localPath,
		"filename": "local.txt",
	})
	if err != nil || gotPath != localPath || cleanName != "local.txt" {
		t.Fatalf("local ensureAttachmentCached = (%q, %q, %v)", gotPath, cleanName, err)
	}

	gotDraftPath, draftName, err := d.ensureAttachmentCached(ctx, map[string]any{
		"draftId":  "d-2",
		"filename": "draft.docx",
	})
	if err != nil || draftName != "draft.docx" {
		t.Fatalf("draft ensureAttachmentCached = (%q, %q, %v)", gotDraftPath, draftName, err)
	}
	raw, err := os.ReadFile(gotDraftPath)
	if err != nil || !bytes.Equal(raw, docxPayload) {
		t.Fatalf("expected cached docx payload %q, got %q (err=%v)", docxPayload, raw, err)
	}

	if _, _, err := d.ensureAttachmentCached(ctx, map[string]any{
		"accountId":    acct.ID.String(),
		"messageId":    "dm-empty",
		"attachmentId": "da-empty",
		"filename":     "empty.bin",
	}); err == nil || !strings.Contains(err.Error(), "has no payload") {
		t.Fatalf("expected empty-payload error, got %v", err)
	}
}
