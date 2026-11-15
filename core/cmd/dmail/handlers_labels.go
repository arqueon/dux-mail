package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/arqueon/dux-mail/core/ent"
	"github.com/arqueon/dux-mail/core/ent/account"
	"github.com/arqueon/dux-mail/core/ent/thread"
	"github.com/arqueon/dux-mail/core/internal/ipc"
	"github.com/arqueon/dux-mail/core/internal/provider"
	dsync "github.com/arqueon/dux-mail/core/internal/sync"
	"github.com/arqueon/dux-mail/core/models"
	"github.com/arqueon/dux-mail/core/repo"
)

// labelsRefreshEvery is how stale the cached catalog may get before a
// completed sync refreshes it.
const labelsRefreshEvery = 10 * time.Minute

// capabilityNames maps the provider capabilities exposed to the UI to the
// strings accounts.list reports in AccountView.Capabilities.
func capabilityNames(c provider.Capability) []string {
	names := []string{}
	if c.Has(provider.CapLabels) {
		names = append(names, "labels")
	}
	return names
}

// accountViews is accounts.list: the repo views plus the capabilities of each
// account's live provider (empty while the provider is unavailable).
func (d *daemon) accountViews(ctx context.Context) ([]models.AccountView, error) {
	views, err := d.repo.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range views {
		id, err := parseUUID(views[i].ID)
		if err != nil || d.registry == nil {
			continue
		}
		if prov, ok := d.registry.Provider(id); ok {
			views[i].Capabilities = capabilityNames(prov.Capabilities())
		}
	}
	return views, nil
}

func accountParam(p map[string]any, key string) (uuid.UUID, error) {
	s, _ := p[key].(string)
	id, err := parseUUID(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("bad account id")
	}
	return id, nil
}

// labelManager returns the account's LabelManager after the capability check.
// A provider without CapLabels is rejected up front, never degraded.
func (d *daemon) labelManager(id uuid.UUID) (provider.LabelManager, error) {
	prov, ok := d.registry.Provider(id)
	if !ok {
		return nil, fmt.Errorf("account provider unavailable")
	}
	if !prov.Capabilities().Has(provider.CapLabels) {
		return nil, fmt.Errorf("this account does not support labels")
	}
	mgr, ok := prov.(provider.LabelManager)
	if !ok {
		return nil, fmt.Errorf("this account does not support labels")
	}
	return mgr, nil
}

// refreshLabels re-reads the account's catalog from the provider and, when it
// changed, publishes labels.changed.
func (d *daemon) refreshLabels(ctx context.Context, id uuid.UUID) error {
	mgr, err := d.labelManager(id)
	if err != nil {
		return err
	}
	labels, err := mgr.ListLabels(ctx)
	if err != nil {
		return err
	}
	catalog := make([]repo.CatalogLabel, 0, len(labels))
	for _, l := range labels {
		catalog = append(catalog, repo.CatalogLabel{ID: l.ID, Name: l.Name, ColorBg: l.ColorBg, ColorText: l.ColorText})
	}
	changed, err := d.repo.ReplaceLabels(ctx, id, catalog)
	if err != nil {
		return err
	}
	d.labelsAt.Store(id, time.Now())
	if changed {
		d.bus.Publish("labels.changed", map[string]any{"accountId": id.String()})
	}
	return nil
}

// refreshAllLabels refreshes every label-capable account (daemon start and
// provider rebuilds). Failures are logged, never fatal: the cached catalog
// keeps serving.
func (d *daemon) refreshAllLabels(ctx context.Context) {
	accts, err := d.db.Account.Query().All(ctx)
	if err != nil {
		return
	}
	for _, a := range accts {
		if _, err := d.labelManager(a.ID); err != nil {
			continue
		}
		if err := d.refreshLabels(ctx, a.ID); err != nil && ctx.Err() == nil {
			slog.Debug("label catalog refresh failed", "account", a.Email, "err", err)
		}
	}
}

// refreshLabelsIfStale is the post-sync hook: it refreshes in the background
// when the cached catalog is older than labelsRefreshEvery.
func (d *daemon) refreshLabelsIfStale(ctx context.Context, id uuid.UUID) {
	if at, ok := d.labelsAt.Load(id); ok && time.Since(at.(time.Time)) < labelsRefreshEvery {
		return
	}
	if _, err := d.labelManager(id); err != nil {
		return
	}
	d.labelsAt.Store(id, time.Now()) // one attempt per window, even when it fails
	go func() {
		if err := d.refreshLabels(ctx, id); err != nil && ctx.Err() == nil {
			slog.Debug("label catalog refresh failed", "account", id, "err", err)
		}
	}()
}

func (d *daemon) labelView(ctx context.Context, acct uuid.UUID, id string) (models.LabelView, error) {
	views, err := d.repo.Labels(ctx, &acct)
	if err != nil {
		return models.LabelView{}, err
	}
	for _, v := range views {
		if v.ID == id {
			return v, nil
		}
	}
	return models.LabelView{}, fmt.Errorf("label %q not found after refresh", id)
}

// knownLabel requires the ID to be in the account's catalog, refreshing once
// when it is not (the catalog may be stale).
func (d *daemon) knownLabel(ctx context.Context, acct uuid.UUID, id string) error {
	if id == "" {
		return fmt.Errorf("missing label id")
	}
	ok, err := d.repo.HasLabelID(ctx, acct, id)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	if err := d.refreshLabels(ctx, acct); err != nil {
		return err
	}
	if ok, err = d.repo.HasLabelID(ctx, acct, id); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("unknown label %q", id)
	}
	return nil
}

func (d *daemon) registerLabelIPC(srv *ipc.Server) {
	srv.Register("labels.list", d.labelsList)
	srv.Register("labels.palette", d.labelsPalette)
	srv.Register("labels.create", d.labelsCreate)
	srv.Register("labels.update", d.labelsUpdate)
	srv.Register("labels.delete", d.labelsDelete)
	srv.Register("ops.label", d.opsLabel)
	srv.Register("threads.fetchLabel", d.fetchLabel)
	srv.Register("accounts.setMonitoredLabels", d.setMonitoredLabels)
}

func (d *daemon) labelsList(ctx context.Context, p map[string]any) (any, error) {
	var only *uuid.UUID
	if s, _ := p["account"].(string); s != "" {
		id, err := accountParam(p, "account")
		if err != nil {
			return nil, err
		}
		only = &id
	}
	if refresh, _ := p["refresh"].(bool); refresh {
		accts, err := d.db.Account.Query().All(ctx)
		if err != nil {
			return nil, err
		}
		for _, a := range accts {
			if only != nil && a.ID != *only {
				continue
			}
			if _, err := d.labelManager(a.ID); err != nil {
				if only != nil {
					return nil, err
				}
				continue
			}
			if err := d.refreshLabels(ctx, a.ID); err != nil {
				if only != nil {
					return nil, err
				}
				slog.Debug("label catalog refresh failed", "account", a.Email, "err", err)
			}
		}
	}
	return d.repo.Labels(ctx, only)
}

func (d *daemon) labelsPalette(ctx context.Context, p map[string]any) (any, error) {
	id, err := accountParam(p, "account")
	if err != nil {
		return nil, err
	}
	mgr, err := d.labelManager(id)
	if err != nil {
		return nil, err
	}
	pal := mgr.LabelPalette()
	if pal == nil {
		pal = []provider.LabelColor{}
	}
	return pal, nil
}

func (d *daemon) labelsCreate(ctx context.Context, p map[string]any) (any, error) {
	id, err := accountParam(p, "account")
	if err != nil {
		return nil, err
	}
	mgr, err := d.labelManager(id)
	if err != nil {
		return nil, err
	}
	name, _ := p["name"].(string)
	l := provider.Label{Name: name}
	l.ColorBg, _ = p["colorBg"].(string)
	l.ColorText, _ = p["colorText"].(string)
	created, err := mgr.CreateLabel(ctx, l)
	if err != nil {
		return nil, err
	}
	if err := d.refreshLabels(ctx, id); err != nil {
		return nil, err
	}
	return d.labelView(ctx, id, created.ID)
}

// clearsColor reports whether a labels.update asks to remove the color:
// colorBg "none", or both colorBg and colorText present and empty.
func clearsColor(p map[string]any) bool {
	bg, hasBg := p["colorBg"].(string)
	text, hasText := p["colorText"].(string)
	if strings.EqualFold(strings.TrimSpace(bg), "none") {
		return true
	}
	return hasBg && hasText && bg == "" && text == ""
}

func (d *daemon) labelsUpdate(ctx context.Context, p map[string]any) (any, error) {
	acct, err := accountParam(p, "account")
	if err != nil {
		return nil, err
	}
	mgr, err := d.labelManager(acct)
	if err != nil {
		return nil, err
	}
	labelID, _ := p["id"].(string)
	if err := d.knownLabel(ctx, acct, labelID); err != nil {
		return nil, err
	}
	l := provider.Label{ID: labelID}
	l.Name, _ = p["name"].(string)
	if clearsColor(p) {
		l.ColorBg = "none"
	} else {
		l.ColorBg, _ = p["colorBg"].(string)
		l.ColorText, _ = p["colorText"].(string)
	}
	if _, err := mgr.UpdateLabel(ctx, l); err != nil {
		return nil, err
	}
	if err := d.refreshLabels(ctx, acct); err != nil {
		return nil, err
	}
	return d.labelView(ctx, acct, labelID)
}

// labelsDelete removes a label from the account. The provider strips it from
// every thread; mail is never deleted. The local cache follows immediately: the
// label leaves the thread rows and the always-synced list (a monitored label
// that no longer exists would make every sync fail).
func (d *daemon) labelsDelete(ctx context.Context, p map[string]any) (any, error) {
	acct, err := accountParam(p, "account")
	if err != nil {
		return nil, err
	}
	mgr, err := d.labelManager(acct)
	if err != nil {
		return nil, err
	}
	labelID, _ := p["id"].(string)
	if err := d.knownLabel(ctx, acct, labelID); err != nil {
		return nil, err
	}
	if err := mgr.DeleteLabel(ctx, labelID); err != nil {
		return nil, err
	}
	if err := d.stripLocalLabel(ctx, acct, labelID); err != nil {
		return nil, err
	}
	if err := d.refreshLabels(ctx, acct); err != nil {
		slog.Debug("label catalog refresh after delete failed", "err", err)
		// The label is gone remotely; drop it from the cache regardless.
		_, _ = d.repo.ReplaceLabels(ctx, acct, d.catalogWithout(ctx, acct, labelID))
		d.bus.Publish("labels.changed", map[string]any{"accountId": acct.String()})
	}
	return map[string]any{"ok": true}, nil
}

func (d *daemon) catalogWithout(ctx context.Context, acct uuid.UUID, labelID string) []repo.CatalogLabel {
	views, _ := d.repo.Labels(ctx, &acct)
	out := make([]repo.CatalogLabel, 0, len(views))
	for _, v := range views {
		if v.ID != labelID {
			out = append(out, repo.CatalogLabel{ID: v.ID, Name: v.Name, ColorBg: v.ColorBg, ColorText: v.ColorText})
		}
	}
	return out
}

func (d *daemon) stripLocalLabel(ctx context.Context, acct uuid.UUID, labelID string) error {
	reload := false
	err := d.repo.WithTx(ctx, func(tx *ent.Tx) error {
		rows, err := tx.Thread.Query().Where(thread.HasAccountWith(account.IDEQ(acct))).All(ctx)
		if err != nil {
			return err
		}
		for _, t := range rows {
			if !slices.Contains(t.Labels, labelID) {
				continue
			}
			kept := slices.DeleteFunc(slices.Clone(t.Labels), func(s string) bool { return s == labelID })
			if err := tx.Thread.UpdateOne(t).SetLabels(kept).Exec(ctx); err != nil {
				return err
			}
		}
		a, err := tx.Account.Get(ctx, acct)
		if err != nil {
			return err
		}
		monitored := repo.MonitoredLabels(a)
		if slices.Contains(monitored, labelID) {
			kept := slices.DeleteFunc(slices.Clone(monitored), func(s string) bool { return s == labelID })
			cfg := cloneConfig(a.Config)
			cfg["labels"] = kept
			reload = true
			return tx.Account.UpdateOne(a).SetConfig(cfg).Exec(ctx)
		}
		return nil
	})
	if err != nil {
		return err
	}
	d.bus.Publish("threads.changed", map[string]any{"accountId": acct.String()})
	if reload {
		d.requestReload()
	}
	return nil
}

func cloneConfig(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

// resolveLabelRefs resolves each reference (a provider label ID, else a label
// name, case-insensitive) against one account's catalog. refs that match
// nothing in this account are returned in missing.
func resolveLabelRefs(catalog []models.LabelView, refs []string) (ids, missing []string) {
	for _, ref := range refs {
		hit := ""
		for _, l := range catalog {
			if l.ID == ref {
				hit = l.ID
				break
			}
		}
		if hit == "" {
			for _, l := range catalog {
				if strings.EqualFold(l.Name, ref) {
					hit = l.ID
					break
				}
			}
		}
		if hit == "" {
			missing = append(missing, ref)
		} else if !slices.Contains(ids, hit) {
			ids = append(ids, hit)
		}
	}
	return ids, missing
}

func stringList(raw any) []string {
	var out []string
	if arr, ok := raw.([]any); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	}
	return out
}

// opsLabel is ops.label: add/remove labels (IDs or names) on a thread
// selection. References are resolved per account; a name an account lacks is
// skipped for that account, a reference no account of the selection knows is
// rejected, and so is any account whose provider cannot do labels. Everything
// is validated before the first op is queued.
func (d *daemon) opsLabel(ctx context.Context, p map[string]any) (any, error) {
	add, remove := stringList(p["add"]), stringList(p["remove"])
	if len(add) == 0 && len(remove) == 0 {
		return nil, fmt.Errorf("nothing to add or remove")
	}
	groups, err := d.resolveThreadGroups(ctx, p)
	if err != nil {
		return nil, err
	}
	type plan struct {
		acct        uuid.UUID
		threads     []string
		add, remove []string
	}
	var plans []plan
	known := map[string]bool{}
	for acct, threads := range groups {
		if _, err := d.labelManager(acct); err != nil {
			return nil, fmt.Errorf("account %s: %w", acct, err)
		}
		catalog, err := d.repo.Labels(ctx, &acct)
		if err != nil {
			return nil, err
		}
		addIDs, addMissing := resolveLabelRefs(catalog, add)
		rmIDs, rmMissing := resolveLabelRefs(catalog, remove)
		for _, ref := range add {
			if !slices.Contains(addMissing, ref) {
				known[ref] = true
			}
		}
		for _, ref := range remove {
			if !slices.Contains(rmMissing, ref) {
				known[ref] = true
			}
		}
		for _, id := range addIDs {
			if slices.Contains(rmIDs, id) {
				return nil, fmt.Errorf("label %q is in both add and remove", id)
			}
		}
		if len(addIDs) == 0 && len(rmIDs) == 0 {
			continue
		}
		plans = append(plans, plan{acct: acct, threads: threads, add: addIDs, remove: rmIDs})
	}
	for _, ref := range append(slices.Clone(add), remove...) {
		if !known[ref] {
			return nil, fmt.Errorf("unknown label %q", ref)
		}
	}
	var failures []error
	for _, pl := range plans {
		if err := d.queue.Enqueue(ctx, dsync.Op{
			AccountID: pl.acct, Type: dsync.OpLabel, ThreadIDs: pl.threads,
			Payload: dsync.OpPayload{Labels: &dsync.LabelsPayload{Add: pl.add, Remove: pl.remove}},
		}); err != nil {
			failures = append(failures, fmt.Errorf("account %s: %w", pl.acct, err))
		}
	}
	return "ok", errors.Join(failures...)
}

// fetchLabel is threads.fetchLabel: one provider page of the threads carrying a
// label, shaped like threads.fetchArchived. The label is `labelName` (every
// account that has it) or `account` + `label` (a provider label ID).
func (d *daemon) fetchLabel(ctx context.Context, p map[string]any) (any, error) {
	name, _ := p["labelName"].(string)
	labelID, _ := p["label"].(string)
	byAccount := map[uuid.UUID]string{}
	switch {
	case strings.TrimSpace(name) != "":
		m, err := d.repo.LabelIDsByName(ctx, name)
		if err != nil {
			return nil, err
		}
		byAccount = m
	case labelID != "":
		acct, err := accountParam(p, "account")
		if err != nil {
			return nil, fmt.Errorf("account is required with label")
		}
		if err := d.knownLabel(ctx, acct, labelID); err != nil {
			return nil, err
		}
		byAccount[acct] = labelID
	default:
		return nil, fmt.Errorf("labelName or account + label is required")
	}
	return d.fetchHistory(ctx, p, func(acct uuid.UUID, prov provider.Provider) func(context.Context, string) (provider.Changes, string, error) {
		id, ok := byAccount[acct]
		if !ok {
			return nil
		}
		pager, ok := prov.(provider.LabelPager)
		if !ok || !prov.Capabilities().Has(provider.CapLabels) {
			return nil
		}
		return func(ctx context.Context, token string) (provider.Changes, string, error) {
			return pager.LabelPage(ctx, id, token)
		}
	})
}

// setMonitoredLabels is accounts.setMonitoredLabels: persist the always-synced
// label IDs, force a complete resync (so the newly monitored labels backfill)
// and reload the engine, which rebuilds the provider and starts that sync.
func (d *daemon) setMonitoredLabels(ctx context.Context, p map[string]any) (any, error) {
	acct, err := accountParam(p, "account")
	if err != nil {
		return nil, err
	}
	if _, err := d.labelManager(acct); err != nil {
		return nil, err
	}
	if _, ok := p["labels"].([]any); !ok {
		return nil, fmt.Errorf("labels must be an array of label ids")
	}
	var ids []string
	for _, id := range stringList(p["labels"]) {
		if slices.Contains(ids, id) {
			continue
		}
		if err := d.knownLabel(ctx, acct, id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	a, err := d.db.Account.Get(ctx, acct)
	if err != nil {
		return nil, err
	}
	cfg := cloneConfig(a.Config)
	if len(ids) == 0 {
		delete(cfg, "labels")
	} else {
		cfg["labels"] = ids
	}
	if err := d.db.Account.UpdateOne(a).SetConfig(cfg).SetSyncCursor("").Exec(ctx); err != nil {
		return nil, err
	}
	d.bus.Publish("labels.changed", map[string]any{"accountId": acct.String()})
	d.requestReload()
	return map[string]any{"ok": true}, nil
}
