package mcp

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/arqueon/dux-mail/core/models"
)

// labelAccounts returns the accounts whose provider supports labels.
func (s *Server) labelAccounts(ctx context.Context) ([]models.AccountView, error) {
	accounts, err := s.fetchAccounts(ctx)
	if err != nil {
		return nil, err
	}
	var out []models.AccountView
	for _, a := range accounts {
		if slices.Contains(a.Capabilities, "labels") {
			out = append(out, a)
		}
	}
	return out, nil
}

// labelAccountID resolves the optional account argument to a label-capable
// account; with no argument it picks the only one.
func (s *Server) labelAccountID(ctx context.Context, ref string) (string, error) {
	capable, err := s.labelAccounts(ctx)
	if err != nil {
		return "", err
	}
	if len(capable) == 0 {
		return "", fmt.Errorf("no configured account supports labels (Gmail only)")
	}
	if strings.TrimSpace(ref) != "" {
		id, err := s.resolveAccountID(ctx, ref)
		if err != nil {
			return "", err
		}
		for _, a := range capable {
			if a.ID == id {
				return id, nil
			}
		}
		return "", fmt.Errorf("account %q does not support labels", ref)
	}
	if len(capable) == 1 {
		return capable[0].ID, nil
	}
	var emails []string
	for _, a := range capable {
		emails = append(emails, a.Email)
	}
	return "", fmt.Errorf("several accounts support labels (%s); specify 'account' (email or UUID)", strings.Join(emails, ", "))
}

func (s *Server) listLabels(ctx context.Context, acctID string, refresh bool) ([]models.LabelView, error) {
	params := map[string]any{}
	if acctID != "" {
		params["account"] = acctID
	}
	if refresh {
		params["refresh"] = true
	}
	var labels []models.LabelView
	if err := s.callJSON(ctx, "labels.list", params, &labels); err != nil {
		return nil, err
	}
	return labels, nil
}

// findLabel resolves a label name (case-insensitive) or provider ID.
func (s *Server) findLabel(ctx context.Context, ref, acctID string) (models.LabelView, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return models.LabelView{}, fmt.Errorf("'label' (name or ID) is required")
	}
	labels, err := s.listLabels(ctx, acctID, false)
	if err != nil {
		return models.LabelView{}, err
	}
	matches := filterLabels(labels, func(l models.LabelView) bool { return l.ID == ref })
	if len(matches) == 0 {
		matches = filterLabels(labels, func(l models.LabelView) bool { return strings.EqualFold(l.Name, ref) })
	}
	switch len(matches) {
	case 0:
		return models.LabelView{}, fmt.Errorf("no label matches %q (call list_labels)", ref)
	case 1:
		return matches[0], nil
	}
	return models.LabelView{}, fmt.Errorf("label %q exists in several accounts; specify 'account' (email or UUID)", ref)
}

func filterLabels(in []models.LabelView, keep func(models.LabelView) bool) []models.LabelView {
	var out []models.LabelView
	for _, l := range in {
		if keep(l) {
			out = append(out, l)
		}
	}
	return out
}

// applyLabelFilter turns the `label` argument of list_threads/search_mail into
// threads.list params: a unique provider ID becomes label+account, a name
// becomes labelName (which every account that has it contributes to).
func (s *Server) applyLabelFilter(ctx context.Context, ref, acctID string, params map[string]any) error {
	if ref == "" {
		return nil
	}
	labels, err := s.listLabels(ctx, acctID, false)
	if err != nil {
		return err
	}
	if byID := filterLabels(labels, func(l models.LabelView) bool { return l.ID == ref }); len(byID) == 1 {
		params["label"] = byID[0].ID
		params["account"] = byID[0].AccountID
		return nil
	} else if len(byID) > 1 {
		return fmt.Errorf("label ID %q exists in several accounts; specify 'account' (email or UUID)", ref)
	}
	if byName := filterLabels(labels, func(l models.LabelView) bool { return strings.EqualFold(l.Name, ref) }); len(byName) > 0 {
		params["labelName"] = byName[0].Name
		return nil
	}
	return fmt.Errorf("no label matches %q (call list_labels)", ref)
}

// labelNamer maps a thread's provider label IDs to label names. IDs outside
// the user-label catalog (system labels) are dropped. A catalog that cannot be
// read yields empty name lists rather than failing the listing.
func (s *Server) labelNamer(ctx context.Context) func(accountID string, ids []string) []string {
	byKey := map[string]string{}
	if labels, err := s.listLabels(ctx, "", false); err == nil {
		for _, l := range labels {
			byKey[l.AccountID+"\x00"+l.ID] = l.Name
		}
	}
	return func(accountID string, ids []string) []string {
		out := []string{}
		for _, id := range ids {
			if name, ok := byKey[accountID+"\x00"+id]; ok {
				out = append(out, name)
			}
		}
		return out
	}
}

func (s *Server) labelTools() []toolSpec {
	readAnn := toolAnnotations{ReadOnlyHint: true, IdempotentHint: true}
	triageAnn := toolAnnotations{ReadOnlyHint: false, IdempotentHint: true, OpenWorldHint: true}
	manageAnn := toolAnnotations{ReadOnlyHint: false, IdempotentHint: false, OpenWorldHint: true}
	deleteAnn := toolAnnotations{ReadOnlyHint: false, DestructiveHint: true, IdempotentHint: true, OpenWorldHint: true}

	return []toolSpec{
		{
			Name:        "list_labels",
			Description: "List user labels (Gmail accounts only) with label ID, account, color and whether the label is always synced. Label names may contain '/' to nest (Auto/Compras is Compras under Auto).",
			InputSchema: schemaObj(map[string]any{
				"account": propStr("Optional account email address or UUID."),
				"refresh": propBool("Re-read the label catalog from the server first (default false)."),
			}),
			Annotations: readAnn,
			minMode:     ModeReadOnly,
			handler: func(ctx context.Context, args map[string]any) (any, error) {
				acctID, err := s.resolveAccountID(ctx, strArg(args, "account"))
				if err != nil {
					return nil, err
				}
				labels, err := s.listLabels(ctx, acctID, boolArg(args, "refresh", false))
				if err != nil {
					return nil, err
				}
				if labels == nil {
					labels = []models.LabelView{}
				}
				return map[string]any{"count": len(labels), "labels": labels}, nil
			},
		},
		{
			Name:        "label_threads",
			Description: "Add and/or remove labels (names or IDs from list_labels) on threads. A label name is resolved in each thread's own account; accounts without that label are skipped. System labels (INBOX, STARRED, ...) are not labels here; use archive_threads, star_threads and friends.",
			InputSchema: schemaObj(map[string]any{
				"threadIds": propIntArray("Numeric local thread IDs from list_threads or search_mail."),
				"add":       propStrArray("Label names or IDs to add."),
				"remove":    propStrArray("Label names or IDs to remove."),
			}, "threadIds"),
			Annotations: triageAnn,
			minMode:     ModeDraft,
			handler: func(ctx context.Context, args map[string]any) (any, error) {
				ids, err := intSliceArg(args, "threadIds")
				if err != nil {
					return nil, err
				}
				add, remove := stringSliceArg(args, "add"), stringSliceArg(args, "remove")
				if len(add) == 0 && len(remove) == 0 {
					return nil, fmt.Errorf("provide at least one label in 'add' or 'remove'")
				}
				if _, err := s.invoker.Call(ctx, "ops.label", map[string]any{"ids": ids, "add": add, "remove": remove}); err != nil {
					return nil, err
				}
				return map[string]any{"status": "queued", "threadIds": ids, "add": add, "remove": remove}, nil
			},
		},
		{
			Name:        "create_label",
			Description: "Create a user label (Gmail). Use '/' in the name to nest it. Colors, when given, must both come from the Gmail palette as #rrggbb.",
			InputSchema: schemaObj(map[string]any{
				"name":      propStr("Label name, e.g. 'Auto/Compras'."),
				"account":   propStr("Account email address or UUID (optional when only one account supports labels)."),
				"colorBg":   propStr("Optional background color, #rrggbb from the Gmail palette."),
				"colorText": propStr("Optional text color, #rrggbb from the Gmail palette (required with colorBg)."),
			}, "name"),
			Annotations: manageAnn,
			minMode:     ModeFull,
			handler: func(ctx context.Context, args map[string]any) (any, error) {
				name := strArg(args, "name")
				if name == "" {
					return nil, fmt.Errorf("'name' is required")
				}
				acctID, err := s.labelAccountID(ctx, strArg(args, "account"))
				if err != nil {
					return nil, err
				}
				params := map[string]any{"account": acctID, "name": name}
				if bg, text := strArg(args, "colorBg"), strArg(args, "colorText"); bg != "" || text != "" {
					params["colorBg"], params["colorText"] = bg, text
				}
				var created models.LabelView
				if err := s.callJSON(ctx, "labels.create", params, &created); err != nil {
					return nil, err
				}
				return map[string]any{"status": "created", "label": created}, nil
			},
		},
		{
			Name:        "update_label",
			Description: "Rename or recolor a user label (Gmail). Renaming does not rename child labels. Set removeColor to clear the color.",
			InputSchema: schemaObj(map[string]any{
				"label":       propStr("Label name or ID to change."),
				"account":     propStr("Optional account email address or UUID (needed when the name exists in several accounts)."),
				"name":        propStr("New name."),
				"colorBg":     propStr("New background color, #rrggbb from the Gmail palette."),
				"colorText":   propStr("New text color, #rrggbb from the Gmail palette (required with colorBg)."),
				"removeColor": propBool("Remove the label color."),
			}, "label"),
			Annotations: manageAnn,
			minMode:     ModeFull,
			handler: func(ctx context.Context, args map[string]any) (any, error) {
				acctID, err := s.resolveAccountID(ctx, strArg(args, "account"))
				if err != nil {
					return nil, err
				}
				l, err := s.findLabel(ctx, strArg(args, "label"), acctID)
				if err != nil {
					return nil, err
				}
				params := map[string]any{"account": l.AccountID, "id": l.ID}
				changes := 0
				if name := strArg(args, "name"); name != "" {
					params["name"] = name
					changes++
				}
				bg, text := strArg(args, "colorBg"), strArg(args, "colorText")
				switch {
				case boolArg(args, "removeColor", false):
					params["colorBg"] = "none"
					changes++
				case bg != "" || text != "":
					if bg == "" || text == "" {
						return nil, fmt.Errorf("'colorBg' and 'colorText' must be given together")
					}
					params["colorBg"], params["colorText"] = bg, text
					changes++
				}
				if changes == 0 {
					return nil, fmt.Errorf("nothing to change: pass 'name', 'colorBg' + 'colorText', or 'removeColor'")
				}
				var updated models.LabelView
				if err := s.callJSON(ctx, "labels.update", params, &updated); err != nil {
					return nil, err
				}
				return map[string]any{"status": "updated", "label": updated}, nil
			},
		},
		{
			Name:        "delete_label",
			Description: "Delete a user label (Gmail). IRREVERSIBLE: the label is removed from every thread that carries it and cannot be restored. Mail is NOT deleted; threads stay where they are.",
			InputSchema: schemaObj(map[string]any{
				"label":   propStr("Label name or ID to delete."),
				"account": propStr("Optional account email address or UUID (needed when the name exists in several accounts)."),
			}, "label"),
			Annotations: deleteAnn,
			minMode:     ModeFull,
			handler: func(ctx context.Context, args map[string]any) (any, error) {
				acctID, err := s.resolveAccountID(ctx, strArg(args, "account"))
				if err != nil {
					return nil, err
				}
				l, err := s.findLabel(ctx, strArg(args, "label"), acctID)
				if err != nil {
					return nil, err
				}
				if _, err := s.invoker.Call(ctx, "labels.delete", map[string]any{"account": l.AccountID, "id": l.ID}); err != nil {
					return nil, err
				}
				return map[string]any{"status": "deleted", "label": l.Name, "labelId": l.ID, "accountId": l.AccountID}, nil
			},
		},
	}
}
