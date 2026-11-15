package gmail

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/arqueon/dux-mail/core/errdefs"
	"github.com/arqueon/dux-mail/core/internal/provider"
	gmailv1 "google.golang.org/api/gmail/v1"
)

// allowedLabelColors is the fixed set of color values the Gmail API accepts
// for BOTH Label.color.backgroundColor and Label.color.textColor. It is
// copied from the LabelColor documentation of google.golang.org/api/gmail/v1
// (gmail-gen.go); anything outside it is rejected by Gmail with a 400.
var allowedLabelColors = []string{
	"#000000", "#434343", "#666666", "#999999", "#cccccc", "#efefef", "#f3f3f3", "#ffffff",
	"#fb4c2f", "#ffad47", "#fad165", "#16a766", "#43d692", "#4a86e8", "#a479e2", "#f691b3",
	"#f6c5be", "#ffe6c7", "#fef1d1", "#b9e4d0", "#c6f3de", "#c9daf8", "#e4d7f5", "#fcdee8",
	"#efa093", "#ffd6a2", "#fce8b3", "#89d3b2", "#a0eac9", "#a4c2f4", "#d0bcf1", "#fbc8d9",
	"#e66550", "#ffbc6b", "#fcda83", "#44b984", "#68dfa9", "#6d9eeb", "#b694e8", "#f7a7c0",
	"#cc3a21", "#eaa041", "#f2c960", "#149e60", "#3dc789", "#3c78d8", "#8e63ce", "#e07798",
	"#ac2b16", "#cf8933", "#d5ae49", "#0b804b", "#2a9c68", "#285bac", "#653e9b", "#b65775",
	"#822111", "#a46a21", "#aa8831", "#076239", "#1a764d", "#1c4587", "#41236d", "#83334c",
	"#464646", "#e7e7e7", "#0d3472", "#b6cff5", "#0d3b44", "#98d7e4", "#3d188e", "#e3d7ff",
	"#711a36", "#fbd3e0", "#8a1c0a", "#f2b2a8", "#7a2e0b", "#ffc8af", "#7a4706", "#ffdeb5",
	"#594c05", "#fbe983", "#684e07", "#fdedc1", "#0b4f30", "#b3efd3", "#04502e", "#a2dcc1",
	"#c2c2c2", "#4986e7", "#2da2bb", "#b99aff", "#994a64", "#f691b2", "#ff7537", "#ffad46",
	"#662e37", "#ebdbde", "#cca6ac", "#094228", "#42d692", "#16a765", "#757575", "#1e53b8",
	"#007286", "#7858c3", "#c2185b", "#d93025", "#54240e", "#633e04", "#521d28", "#202124",
	"#083018",
}

// labelPalette pairs every allowed background with whichever of the allowed
// black/white text colors reads better on it (WCAG relative luminance).
var labelPalette = buildLabelPalette()

func buildLabelPalette() []provider.LabelColor {
	out := make([]provider.LabelColor, 0, len(allowedLabelColors))
	for _, bg := range allowedLabelColors {
		text := "#ffffff"
		if relativeLuminance(bg) > 0.179 { // contrast with black beats white
			text = "#000000"
		}
		out = append(out, provider.LabelColor{Background: bg, Text: text})
	}
	return out
}

func relativeLuminance(hex string) float64 {
	if len(hex) != 7 || hex[0] != '#' {
		return 0
	}
	channel := func(s string) float64 {
		v, err := strconv.ParseUint(s, 16, 8)
		if err != nil {
			return 0
		}
		c := float64(v) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(hex[1:3]) + 0.7152*channel(hex[3:5]) + 0.0722*channel(hex[5:7])
}

func isAllowedLabelColor(hex string) bool {
	hex = strings.ToLower(hex)
	for _, c := range allowedLabelColors {
		if c == hex {
			return true
		}
	}
	return false
}

// systemLabelIDs are the Gmail system label IDs. They are never listed as
// labels and ModifyLabels refuses them: inbox/star/spam/trash/read state have
// their own typed operations.
var systemLabelIDs = map[string]bool{
	"INBOX": true, "SPAM": true, "TRASH": true, "UNREAD": true, "STARRED": true,
	"IMPORTANT": true, "SENT": true, "DRAFT": true, "CHAT": true,
	"SCHEDULED": true, "SNOOZED": true,
}

func isSystemLabelID(id string) bool {
	return systemLabelIDs[id] ||
		strings.HasPrefix(id, "CATEGORY_") ||
		strings.HasSuffix(id, "_STAR")
}

// LabelPalette implements provider.LabelManager.
func (p *Provider) LabelPalette() []provider.LabelColor {
	return append([]provider.LabelColor(nil), labelPalette...)
}

func (p *Provider) labelsAPI() (gmailLabelsAPI, error) {
	lapi, ok := p.api.(gmailLabelsAPI)
	if !ok {
		return nil, errdefs.Wrap(errdefs.KindPermanent, fmt.Errorf("gmail: labels not supported by this API"))
	}
	return lapi, nil
}

func permanent(format string, a ...any) error {
	return errdefs.Wrap(errdefs.KindPermanent, fmt.Errorf(format, a...))
}

func toLabel(l *gmailv1.Label) provider.Label {
	out := provider.Label{ID: l.Id, Name: l.Name}
	if l.Color != nil {
		out.ColorBg = strings.ToLower(l.Color.BackgroundColor)
		out.ColorText = strings.ToLower(l.Color.TextColor)
	}
	return out
}

// ListLabels returns the account's user labels (system labels are dropped).
func (p *Provider) ListLabels(ctx context.Context) ([]provider.Label, error) {
	lapi, err := p.labelsAPI()
	if err != nil {
		return nil, err
	}
	ctx = withRetryBudget(ctx)
	raw, err := lapi.ListLabels(ctx)
	if err != nil {
		return nil, classify(err)
	}
	out := make([]provider.Label, 0, len(raw))
	for _, l := range raw {
		if l == nil || l.Id == "" || l.Type == "system" || isSystemLabelID(l.Id) {
			continue
		}
		out = append(out, toLabel(l))
	}
	return out, nil
}

func validateColors(bg, text string) error {
	if (bg == "") != (text == "") {
		return permanent("gmail: a label color needs both a background and a text color")
	}
	if bg == "" {
		return nil
	}
	if !isAllowedLabelColor(bg) || !isAllowedLabelColor(text) {
		return permanent("gmail: label color %s/%s is not in the Gmail palette", bg, text)
	}
	return nil
}

// CreateLabel creates a user label. The name may contain "/" to nest it.
func (p *Provider) CreateLabel(ctx context.Context, l provider.Label) (provider.Label, error) {
	lapi, err := p.labelsAPI()
	if err != nil {
		return provider.Label{}, err
	}
	name := strings.TrimSpace(l.Name)
	if name == "" {
		return provider.Label{}, permanent("gmail: label name is required")
	}
	if err := validateColors(strings.ToLower(l.ColorBg), strings.ToLower(l.ColorText)); err != nil {
		return provider.Label{}, err
	}
	req := &gmailv1.Label{
		Name:                  name,
		LabelListVisibility:   "labelShow",
		MessageListVisibility: "show",
	}
	if l.ColorBg != "" {
		req.Color = &gmailv1.LabelColor{BackgroundColor: strings.ToLower(l.ColorBg), TextColor: strings.ToLower(l.ColorText)}
	}
	ctx = withRetryBudget(ctx)
	out, err := lapi.CreateLabel(ctx, req)
	if err != nil {
		return provider.Label{}, classify(err)
	}
	return toLabel(out), nil
}

// ColorNone passed as UpdateLabel's ColorBg clears the label color.
const ColorNone = "none"

// UpdateLabel patches a user label by ID. Empty fields keep their value;
// ColorBg == ColorNone removes the color.
func (p *Provider) UpdateLabel(ctx context.Context, l provider.Label) (provider.Label, error) {
	lapi, err := p.labelsAPI()
	if err != nil {
		return provider.Label{}, err
	}
	if l.ID == "" || isSystemLabelID(l.ID) {
		return provider.Label{}, permanent("gmail: cannot modify label %q", l.ID)
	}
	req := &gmailv1.Label{}
	if name := strings.TrimSpace(l.Name); name != "" {
		req.Name = name
	}
	switch bg := strings.ToLower(l.ColorBg); {
	case bg == ColorNone:
		req.NullFields = append(req.NullFields, "Color")
	case bg != "" || l.ColorText != "":
		if err := validateColors(bg, strings.ToLower(l.ColorText)); err != nil {
			return provider.Label{}, err
		}
		req.Color = &gmailv1.LabelColor{BackgroundColor: bg, TextColor: strings.ToLower(l.ColorText)}
	}
	ctx = withRetryBudget(ctx)
	out, err := lapi.PatchLabel(ctx, l.ID, req)
	if err != nil {
		return provider.Label{}, classify(err)
	}
	return toLabel(out), nil
}

// DeleteLabel deletes a user label. Gmail strips it from every thread; the
// mail itself is untouched. A label that is already gone is not an error.
func (p *Provider) DeleteLabel(ctx context.Context, labelID string) error {
	lapi, err := p.labelsAPI()
	if err != nil {
		return err
	}
	if labelID == "" || isSystemLabelID(labelID) {
		return permanent("gmail: cannot delete label %q", labelID)
	}
	ctx = withRetryBudget(ctx)
	if err := lapi.DeleteLabel(ctx, labelID); err != nil && !isNotFound(err) {
		return classify(err)
	}
	return nil
}

// ModifyLabels adds/removes user labels on threads. System label IDs are
// refused: inbox, star, spam, trash and read state have typed operations.
func (p *Provider) ModifyLabels(ctx context.Context, threadIDs []string, add, remove []string) error {
	for _, id := range append(append([]string(nil), add...), remove...) {
		if id == "" || isSystemLabelID(id) {
			return permanent("gmail: %q is a system label and cannot be modified as a label", id)
		}
	}
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	return p.modifyAll(ctx, threadIDs, add, remove)
}

// LabelPage implements provider.LabelPager: one page of the threads carrying
// labelID, newest first, ingested as backfill.
func (p *Provider) LabelPage(ctx context.Context, labelID, pageToken string) (provider.Changes, string, error) {
	lapi, err := p.labelsAPI()
	if err != nil {
		return provider.Changes{}, pageToken, err
	}
	if labelID == "" || isSystemLabelID(labelID) {
		return provider.Changes{}, pageToken, permanent("gmail: %q is not a user label", labelID)
	}
	return p.pagedThreads(ctx, "label:"+labelID, pageToken, func(ctx context.Context, page string) ([]string, string, error) {
		return lapi.ListLabelThreads(ctx, labelID, page, 25)
	})
}
