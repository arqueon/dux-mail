package repo

import (
	"context"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/arqueon/dux-mail/core/ent"
	"github.com/arqueon/dux-mail/core/ent/account"
	"github.com/arqueon/dux-mail/core/ent/label"
	"github.com/arqueon/dux-mail/core/models"
)

// CatalogLabel is one label as the provider reports it.
type CatalogLabel struct {
	ID, Name, ColorBg, ColorText string
}

// MonitoredLabels returns the account's always-synced label IDs
// (Account.config["labels"]).
func MonitoredLabels(a *ent.Account) []string {
	raw, _ := a.Config["labels"].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Labels returns the cached label catalog, ordered by name then account.
// accountID nil means every account.
func (r *Repo) Labels(ctx context.Context, accountID *uuid.UUID) ([]models.LabelView, error) {
	q := r.client.Label.Query().WithAccount()
	if accountID != nil {
		q = q.Where(label.HasAccountWith(account.IDEQ(*accountID)))
	}
	rows, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	monitored := map[uuid.UUID]map[string]bool{}
	out := make([]models.LabelView, 0, len(rows))
	for _, l := range rows {
		a := l.Edges.Account
		if a == nil {
			continue
		}
		m, ok := monitored[a.ID]
		if !ok {
			m = map[string]bool{}
			for _, id := range MonitoredLabels(a) {
				m[id] = true
			}
			monitored[a.ID] = m
		}
		out = append(out, models.LabelView{
			ID:        l.ProviderLabelID,
			AccountID: a.ID.String(),
			Name:      l.Name,
			ColorBg:   l.ColorBg,
			ColorText: l.ColorText,
			Monitored: m[l.ProviderLabelID],
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].AccountID < out[j].AccountID
	})
	return out, nil
}

// ReplaceLabels makes the account's cached catalog equal to labels and
// reports whether anything changed (added, renamed, recolored or removed).
func (r *Repo) ReplaceLabels(ctx context.Context, accountID uuid.UUID, labels []CatalogLabel) (bool, error) {
	changed := false
	err := r.WithTx(ctx, func(tx *ent.Tx) error {
		existing, err := tx.Label.Query().Where(label.HasAccountWith(account.IDEQ(accountID))).All(ctx)
		if err != nil {
			return err
		}
		byID := make(map[string]*ent.Label, len(existing))
		for _, l := range existing {
			byID[l.ProviderLabelID] = l
		}
		seen := make(map[string]bool, len(labels))
		for _, want := range labels {
			if want.ID == "" || want.Name == "" || seen[want.ID] {
				continue
			}
			seen[want.ID] = true
			have, ok := byID[want.ID]
			switch {
			case !ok:
				changed = true
				if err := tx.Label.Create().
					SetAccountID(accountID).
					SetProviderLabelID(want.ID).
					SetName(want.Name).
					SetColorBg(want.ColorBg).
					SetColorText(want.ColorText).
					Exec(ctx); err != nil {
					return err
				}
			case have.Name != want.Name || have.ColorBg != want.ColorBg || have.ColorText != want.ColorText:
				changed = true
				if err := tx.Label.UpdateOne(have).
					SetName(want.Name).
					SetColorBg(want.ColorBg).
					SetColorText(want.ColorText).
					Exec(ctx); err != nil {
					return err
				}
			}
		}
		for _, l := range existing {
			if !seen[l.ProviderLabelID] {
				changed = true
				if err := tx.Label.DeleteOne(l).Exec(ctx); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return changed, err
}

// LabelIDsByName resolves a label name (case-insensitive, exact) to the
// provider label ID of each account that has it. Accounts without a label of
// that name are absent from the result.
func (r *Repo) LabelIDsByName(ctx context.Context, name string) (map[uuid.UUID]string, error) {
	name = strings.TrimSpace(name)
	out := map[uuid.UUID]string{}
	if name == "" {
		return out, nil
	}
	rows, err := r.client.Label.Query().WithAccount().All(ctx)
	if err != nil {
		return nil, err
	}
	for _, l := range rows {
		if l.Edges.Account != nil && strings.EqualFold(l.Name, name) {
			out[l.Edges.Account.ID] = l.ProviderLabelID
		}
	}
	return out, nil
}

// HasLabelID reports whether the account's catalog holds the label ID.
func (r *Repo) HasLabelID(ctx context.Context, accountID uuid.UUID, id string) (bool, error) {
	return r.client.Label.Query().
		Where(label.HasAccountWith(account.IDEQ(accountID)), label.ProviderLabelIDEQ(id)).
		Exist(ctx)
}
