package gmail

import (
	"context"
	"slices"
	"strconv"
	"testing"

	"github.com/arqueon/dux-mail/core/errdefs"
	"github.com/arqueon/dux-mail/core/internal/provider"
	gmailv1 "google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
)

func (f *fakeAPI) ListLabels(_ context.Context) ([]*gmailv1.Label, error) {
	f.record("ListLabels")
	return f.labels, f.labelErr
}

func (f *fakeAPI) CreateLabel(_ context.Context, l *gmailv1.Label) (*gmailv1.Label, error) {
	f.record("CreateLabel")
	if f.labelErr != nil {
		return nil, f.labelErr
	}
	f.labelCreated = append(f.labelCreated, l)
	out := *l
	out.Id = "Label_" + strconv.Itoa(100+len(f.labelCreated))
	out.Type = "user"
	return &out, nil
}

func (f *fakeAPI) PatchLabel(_ context.Context, id string, l *gmailv1.Label) (*gmailv1.Label, error) {
	f.record("PatchLabel")
	if f.labelErr != nil {
		return nil, f.labelErr
	}
	if f.labelPatches == nil {
		f.labelPatches = map[string]*gmailv1.Label{}
	}
	f.labelPatches[id] = l
	out := *l
	out.Id = id
	return &out, nil
}

func (f *fakeAPI) DeleteLabel(_ context.Context, id string) error {
	f.record("DeleteLabel")
	if f.labelErr != nil {
		return f.labelErr
	}
	f.labelDeleted = append(f.labelDeleted, id)
	return nil
}

func (f *fakeAPI) ListLabelThreads(_ context.Context, labelID, pageToken string, _ int) ([]string, string, error) {
	f.record("ListLabelThreads")
	if f.labelErr != nil {
		return nil, "", f.labelErr
	}
	pages := f.labelPages[labelID]
	idx := 0
	if pageToken != "" {
		idx, _ = strconv.Atoi(pageToken)
	}
	if idx >= len(pages) {
		return nil, "", nil
	}
	return pages[idx].ids, pages[idx].next, nil
}

var (
	_ provider.LabelManager = (*Provider)(nil)
	_ provider.LabelPager   = (*Provider)(nil)
)

func TestLabelsCapabilityDeclared(t *testing.T) {
	p := newTestProvider(&fakeAPI{}, Options{})
	if !p.Capabilities().Has(provider.CapLabels) {
		t.Fatal("gmail must declare CapLabels")
	}
}

func TestListLabelsKeepsOnlyUserLabelsAndMapsColors(t *testing.T) {
	f := &fakeAPI{labels: []*gmailv1.Label{
		{Id: "INBOX", Name: "INBOX", Type: "system"},
		{Id: "CATEGORY_SOCIAL", Name: "CATEGORY_SOCIAL", Type: "system"},
		{Id: "Label_1", Name: "Auto/Compras", Type: "user",
			Color: &gmailv1.LabelColor{BackgroundColor: "#16a765", TextColor: "#FFFFFF"}},
		{Id: "Label_2", Name: "Plain", Type: "user"},
	}}
	got, err := newTestProvider(f, Options{}).ListLabels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []provider.Label{
		{ID: "Label_1", Name: "Auto/Compras", ColorBg: "#16a765", ColorText: "#ffffff"},
		{ID: "Label_2", Name: "Plain"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("labels = %+v, want %+v", got, want)
	}
}

func TestLabelPaletteIsInsideGmailAllowedSet(t *testing.T) {
	pal := newTestProvider(&fakeAPI{}, Options{}).LabelPalette()
	if len(pal) == 0 {
		t.Fatal("empty palette")
	}
	for _, c := range pal {
		if !isAllowedLabelColor(c.Background) || !isAllowedLabelColor(c.Text) {
			t.Errorf("palette pair %+v leaves the Gmail allowed set", c)
		}
	}
	if pal[0].Background != "#000000" || pal[0].Text != "#ffffff" {
		t.Errorf("black background must get white text, got %+v", pal[0])
	}
	if pal[7].Background != "#ffffff" || pal[7].Text != "#000000" {
		t.Errorf("white background must get black text, got %+v", pal[7])
	}
}

func TestCreateLabelValidatesAndSendsColor(t *testing.T) {
	f := &fakeAPI{}
	p := newTestProvider(f, Options{})
	ctx := context.Background()

	if _, err := p.CreateLabel(ctx, provider.Label{Name: "  "}); err == nil {
		t.Error("blank name must be rejected")
	}
	if _, err := p.CreateLabel(ctx, provider.Label{Name: "X", ColorBg: "#123456", ColorText: "#ffffff"}); errdefs.KindOf(err) != errdefs.KindPermanent {
		t.Errorf("off-palette color: err = %v, want permanent", err)
	}
	if _, err := p.CreateLabel(ctx, provider.Label{Name: "X", ColorBg: "#16a765"}); err == nil {
		t.Error("background without text must be rejected")
	}
	if len(f.labelCreated) != 0 {
		t.Fatal("invalid creates must not reach the API")
	}

	l, err := p.CreateLabel(ctx, provider.Label{Name: " Auto/Compras ", ColorBg: "#16a765", ColorText: "#ffffff"})
	if err != nil {
		t.Fatal(err)
	}
	if l.ID != "Label_101" || l.Name != "Auto/Compras" || l.ColorBg != "#16a765" {
		t.Errorf("created = %+v", l)
	}
	sent := f.labelCreated[0]
	if sent.Color == nil || sent.Color.TextColor != "#ffffff" || sent.LabelListVisibility != "labelShow" {
		t.Errorf("request = %+v", sent)
	}
}

func TestUpdateLabelPatchesOnlyGivenFieldsAndRefusesSystem(t *testing.T) {
	f := &fakeAPI{}
	p := newTestProvider(f, Options{})
	ctx := context.Background()

	if _, err := p.UpdateLabel(ctx, provider.Label{ID: "INBOX", Name: "x"}); err == nil {
		t.Fatal("system label update must be refused")
	}
	if _, err := p.UpdateLabel(ctx, provider.Label{ID: "Label_1", Name: "Renamed"}); err != nil {
		t.Fatal(err)
	}
	patch := f.labelPatches["Label_1"]
	if patch.Name != "Renamed" || patch.Color != nil || len(patch.NullFields) != 0 {
		t.Errorf("rename patch = %+v", patch)
	}
	if _, err := p.UpdateLabel(ctx, provider.Label{ID: "Label_2", ColorBg: ColorNone}); err != nil {
		t.Fatal(err)
	}
	if got := f.labelPatches["Label_2"].NullFields; !slices.Equal(got, []string{"Color"}) {
		t.Errorf("clear color NullFields = %v", got)
	}
	if _, err := p.UpdateLabel(ctx, provider.Label{ID: "Label_3", ColorBg: "#000001", ColorText: "#ffffff"}); err == nil {
		t.Error("off-palette recolor must be refused")
	}
}

func TestDeleteLabelIsIdempotentAndNeverTouchesMail(t *testing.T) {
	f := &fakeAPI{}
	p := newTestProvider(f, Options{})
	ctx := context.Background()

	if err := p.DeleteLabel(ctx, "Label_9"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.labelDeleted, []string{"Label_9"}) || len(f.modifies) != 0 {
		t.Errorf("deleted=%v modifies=%v", f.labelDeleted, f.modifies)
	}
	for _, sys := range []string{"INBOX", "TRASH", "SPAM", "CATEGORY_PROMOTIONS", ""} {
		if err := p.DeleteLabel(ctx, sys); err == nil {
			t.Errorf("deleting %q must be refused", sys)
		}
	}
	f.labelErr = &googleapi.Error{Code: 404}
	if err := p.DeleteLabel(ctx, "Label_gone"); err != nil {
		t.Errorf("already-deleted label must not be an error, got %v", err)
	}
	f.labelErr = &googleapi.Error{Code: 500}
	if err := p.DeleteLabel(ctx, "Label_9"); errdefs.KindOf(err) != errdefs.KindNetwork {
		t.Errorf("5xx kind = %v, want network", errdefs.KindOf(err))
	}
}

func TestModifyLabelsRefusesSystemIDsAndAppliesPerThread(t *testing.T) {
	f := &fakeAPI{}
	p := newTestProvider(f, Options{})
	ctx := context.Background()

	for _, bad := range [][2][]string{
		{{"INBOX"}, nil}, {nil, {"TRASH"}}, {{"Label_1", "UNREAD"}, nil},
		{{"CATEGORY_UPDATES"}, nil}, {{"STARRED"}, nil}, {{""}, nil},
	} {
		if err := p.ModifyLabels(ctx, []string{"t1"}, bad[0], bad[1]); errdefs.KindOf(err) != errdefs.KindPermanent {
			t.Errorf("ModifyLabels(%v,%v) err = %v, want permanent refusal", bad[0], bad[1], err)
		}
	}
	if len(f.modifies) != 0 {
		t.Fatalf("refused calls reached the API: %v", f.modifies)
	}

	if err := p.ModifyLabels(ctx, []string{"t1", "t2"}, []string{"Label_1"}, []string{"Label_2"}); err != nil {
		t.Fatal(err)
	}
	want := []modifyCall{
		{threadID: "t1", add: []string{"Label_1"}, remove: []string{"Label_2"}},
		{threadID: "t2", add: []string{"Label_1"}, remove: []string{"Label_2"}},
	}
	if len(f.modifies) != 2 || f.modifies[0].threadID != want[0].threadID || !slices.Equal(f.modifies[1].add, want[1].add) {
		t.Errorf("modifies = %+v", f.modifies)
	}
}

func TestModifyLabelsClassifiesErrors(t *testing.T) {
	f := &fakeAPI{modifyErr: &googleapi.Error{Code: 429}}
	err := newTestProvider(f, Options{}).ModifyLabels(context.Background(), []string{"t1"}, []string{"Label_1"}, nil)
	if errdefs.KindOf(err) != errdefs.KindRateLimit {
		t.Fatalf("kind = %v, want rate limit (%v)", errdefs.KindOf(err), err)
	}
}

func TestLabelPagePagesNewestFirstAsBackfill(t *testing.T) {
	f := &fakeAPI{
		labelPages: map[string][]listPage{"Label_1": {{ids: []string{"t1"}, next: "1"}, {ids: []string{"t1"}}}},
		threads:    fixtureThreads(),
	}
	p := newTestProvider(f, Options{})
	ctx := context.Background()

	ch, next, err := p.LabelPage(ctx, "Label_1", "")
	if err != nil {
		t.Fatal(err)
	}
	if !ch.Backfill || len(ch.Upserted) != 1 || next == "" {
		t.Fatalf("page 1: backfill=%v upserted=%d next=%q", ch.Backfill, len(ch.Upserted), next)
	}
	_, last, err := p.LabelPage(ctx, "Label_1", next)
	if err != nil || last != "" {
		t.Fatalf("page 2: next=%q err=%v, want exhausted", last, err)
	}
	// A token from one label must not be replayed against another.
	if _, _, err := p.LabelPage(ctx, "Label_2", next); err == nil {
		t.Error("cross-label cursor must be rejected")
	}
	if _, _, err := p.LabelPage(ctx, "INBOX", ""); err == nil {
		t.Error("system label pages must be refused")
	}
}

// noLabelsAPI satisfies gmailAPI but not gmailLabelsAPI.
type noLabelsAPI struct{ gmailAPI }

func TestLabelsUnsupportedAPIIsPermanent(t *testing.T) {
	p := New("acct", "a@b.c", noLabelsAPI{}, Options{})
	if _, err := p.ListLabels(context.Background()); errdefs.KindOf(err) != errdefs.KindPermanent {
		t.Fatalf("err = %v", err)
	}
}
