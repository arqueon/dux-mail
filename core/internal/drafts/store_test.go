package drafts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/arqueon/dux-mail/core/internal/provider"
	"github.com/arqueon/dux-mail/core/models"
)

func TestStoreSaveListAndMergeRemote(t *testing.T) {
	dir := t.TempDir()
	attachPath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(attachPath, []byte("hello attachment"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(dir, "drafts.json"))

	saved, err := store.Save(models.DraftView{
		AccountID: "acct-1",
		To:        []string{"ada@example.org"},
		Subject:   "Borrador inicial",
		Body:      "Contenido de prueba",
		Attachments: []models.AttachmentView{
			{Path: attachPath},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == "" || len(saved.Attachments) != 1 || saved.Attachments[0].Filename != "sample.txt" || saved.Attachments[0].Size != 16 {
		t.Fatalf("unexpected saved draft: %+v", saved)
	}

	reloaded := NewStore(filepath.Join(dir, "drafts.json"))
	list := reloaded.List("acct-1", "inicial")
	if len(list) != 1 || list[0].ID != saved.ID {
		t.Fatalf("expected 1 persisted draft, got %+v", list)
	}

	reloaded.MergeRemote("acct-1", []provider.DraftItem{
		{
			ProviderDraftID: "gd-1",
			To:              []string{"bob@example.org"},
			Subject:         "Remote draft",
			Body:            "Body from server",
			UpdatedAt:       1700000500,
		},
	})
	if got := len(reloaded.List("acct-1", "")); got != 2 {
		t.Fatalf("expected 2 drafts after remote merge, got %d", got)
	}
}

func TestMergeRemotePreservesAttachmentIDsAndLocalPath(t *testing.T) {
	dir := t.TempDir()
	localPath := filepath.Join(dir, "local.pdf")
	if err := os.WriteFile(localPath, []byte("%PDF-1.7 local copy"), 0o600); err != nil {
		t.Fatal(err)
	}

	store := NewStore(filepath.Join(dir, "drafts.json"))
	// Pre-fix draft synced without MessageID/AttachmentID and without local path, plus one local file draft.
	_, err := store.Save(models.DraftView{
		ID:              "draft-backfill",
		AccountID:       "acct-1",
		ProviderDraftID: "gd-backfill",
		Subject:         "Synced on another host",
		Attachments: []models.AttachmentView{
			{Filename: "remote.docx", MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Size: 2048},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Save(models.DraftView{
		ID:              "draft-local",
		AccountID:       "acct-1",
		ProviderDraftID: "gd-local",
		Subject:         "With local file",
		Attachments: []models.AttachmentView{
			{Path: localPath, Filename: "local.pdf"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	store.MergeRemote("acct-1", []provider.DraftItem{
		{
			ProviderDraftID:   "gd-new",
			ProviderMessageID: "gm-new",
			To:                []string{"ina@example.org"},
			Subject:           "Fresh remote draft",
			UpdatedAt:         1700001000,
			Attachments: []provider.AttachmentFile{
				{MessageID: "gm-new", AttachmentID: "att-new", Filename: "report.pdf", MimeType: "application/pdf", Size: 5120},
			},
		},
		{
			ProviderDraftID:   "gd-backfill",
			ProviderMessageID: "gm-backfill",
			To:                []string{"jorge@example.org"},
			Subject:           "Synced on another host",
			UpdatedAt:         1700001000,
			Attachments: []provider.AttachmentFile{
				{MessageID: "gm-backfill", AttachmentID: "att-backfill", Filename: "remote.docx", MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Size: 2048},
			},
		},
		{
			ProviderDraftID:   "gd-local",
			ProviderMessageID: "gm-local",
			To:                []string{"ada@example.org"},
			Subject:           "With local file",
			UpdatedAt:         1700001000,
			Attachments: []provider.AttachmentFile{
				{MessageID: "gm-local", AttachmentID: "att-local", Filename: "local.pdf", MimeType: "application/pdf", Size: 19},
			},
		},
	})

	var gotNew, gotBackfill, gotLocal models.DraftView
	for _, d := range store.List("acct-1", "") {
		switch d.ProviderDraftID {
		case "gd-new":
			gotNew = d
		case "gd-backfill":
			gotBackfill = d
		case "gd-local":
			gotLocal = d
		}
	}

	if len(gotNew.Attachments) != 1 || gotNew.Attachments[0].MessageID != "gm-new" || gotNew.Attachments[0].AttachmentID != "att-new" {
		t.Fatalf("(a) expected remote draft IDs stored, got %+v", gotNew.Attachments)
	}
	if len(gotBackfill.Attachments) != 1 || gotBackfill.Attachments[0].MessageID != "gm-backfill" || gotBackfill.Attachments[0].AttachmentID != "att-backfill" {
		t.Fatalf("(b) expected pre-fix attachment backfilled with remote IDs, got %+v", gotBackfill.Attachments)
	}
	if len(gotLocal.Attachments) != 1 || gotLocal.Attachments[0].Path != localPath || gotLocal.Attachments[0].MessageID != "gm-local" || gotLocal.Attachments[0].AttachmentID != "att-local" {
		t.Fatalf("(c) expected local Path preserved alongside remote IDs, got %+v", gotLocal.Attachments)
	}
}
