package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/arqueon/dux-mail/core/internal/i18n"
	"github.com/arqueon/dux-mail/core/internal/ipc"
	"github.com/arqueon/dux-mail/core/models"
)

// labelCmd is `dmail label ...`: list, create, rename, recolor, delete labels
// and add/remove them on threads. Gmail only; accounts without the labels
// capability are invisible to it.
func labelCmd() *cobra.Command {
	root := &cobra.Command{Use: "label", Short: "List, manage and apply mail labels (Gmail accounts)"}
	var account string
	accountFlag := func(c *cobra.Command) {
		c.Flags().StringVar(&account, "account", "", "account UUID or email (needed only when the label name is ambiguous)")
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List labels",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withLabelEnv(account, func(c *ipc.Client, env *labelEnv) error {
				params := map[string]any{}
				if env.account != nil {
					params["account"] = env.account.ID
				}
				raw, err := c.Call("labels.list", params)
				if err != nil {
					return err
				}
				if flagJSON {
					fmt.Println(string(raw))
					return nil
				}
				var labels []models.LabelView
				if err := json.Unmarshal(raw, &labels); err != nil {
					return err
				}
				printLabels(os.Stdout, labels, env.emails)
				return nil
			})
		},
	}
	accountFlag(list)

	var color string
	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a label (use / in the name to nest it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withLabelEnv(account, func(c *ipc.Client, env *labelEnv) error {
				acct, err := env.accountForCreate()
				if err != nil {
					return err
				}
				params := map[string]any{"account": acct.ID, "name": args[0]}
				if color != "" {
					bg, text, err := parseColorPair(color, false)
					if err != nil {
						return err
					}
					params["colorBg"], params["colorText"] = bg, text
				}
				raw, err := c.Call("labels.create", params)
				if err != nil {
					return err
				}
				return printLabelResult(raw, i18n.T("Label created: %s (%s)\n"))
			})
		},
	}
	accountFlag(create)
	create.Flags().StringVar(&color, "color", "", "color as <background>:<text>, hex from the Gmail palette (e.g. #16a765:#ffffff)")

	rename := &cobra.Command{
		Use:   "rename <label> <new-name>",
		Short: "Rename a label (children are not renamed)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withLabelEnv(account, func(c *ipc.Client, env *labelEnv) error {
				l, err := env.findLabel(c, args[0])
				if err != nil {
					return err
				}
				raw, err := c.Call("labels.update", map[string]any{"account": l.AccountID, "id": l.ID, "name": args[1]})
				if err != nil {
					return err
				}
				return printLabelResult(raw, i18n.T("Label renamed: %s (%s)\n"))
			})
		},
	}
	accountFlag(rename)

	recolor := &cobra.Command{
		Use:   "color <label> <bg>:<text>|none",
		Short: "Set a label's color, or remove it with none",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			bg, text, err := parseColorPair(args[1], true)
			if err != nil {
				return err
			}
			return withLabelEnv(account, func(c *ipc.Client, env *labelEnv) error {
				l, err := env.findLabel(c, args[0])
				if err != nil {
					return err
				}
				params := map[string]any{"account": l.AccountID, "id": l.ID, "colorBg": bg, "colorText": text}
				raw, err := c.Call("labels.update", params)
				if err != nil {
					return err
				}
				return printLabelResult(raw, i18n.T("Label recolored: %s (%s)\n"))
			})
		},
	}
	accountFlag(recolor)

	var yes bool
	del := &cobra.Command{
		Use:   "delete <label>",
		Short: "Delete a label (irreversible; it is removed from every thread, mail is kept)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withLabelEnv(account, func(c *ipc.Client, env *labelEnv) error {
				l, err := env.findLabel(c, args[0])
				if err != nil {
					return err
				}
				if !yes && !confirmLabelDelete(os.Stdin, os.Stderr, l, env.emails[l.AccountID]) {
					fmt.Println(i18n.T("Cancelled."))
					return nil
				}
				if _, err := c.Call("labels.delete", map[string]any{"account": l.AccountID, "id": l.ID}); err != nil {
					return err
				}
				fmt.Printf(i18n.T("Label deleted: %s (mail untouched).\n"), l.Name)
				return nil
			})
		},
	}
	accountFlag(del)
	del.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")

	apply := func(use, short string, remove bool) *cobra.Command {
		return &cobra.Command{
			Use:   use,
			Short: short,
			Args:  cobra.MinimumNArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				ids, err := parseThreadIDs(args[1:])
				if err != nil {
					return err
				}
				key := "add"
				if remove {
					key = "remove"
				}
				c, err := dial()
				if err != nil {
					return err
				}
				defer c.Close()
				if _, err = c.Call("ops.label", map[string]any{"ids": ids, key: []any{args[0]}}); err != nil {
					return err
				}
				if remove {
					fmt.Printf(i18n.T("Label %s removed from %d thread(s).\n"), args[0], len(ids))
				} else {
					fmt.Printf(i18n.T("Label %s added to %d thread(s).\n"), args[0], len(ids))
				}
				return nil
			},
		}
	}

	root.AddCommand(list, create, rename, recolor, del,
		apply("add <label> <thread-id>...", "Add a label (name or ID) to threads", false),
		apply("remove <label> <thread-id>...", "Remove a label (name or ID) from threads", true))
	return root
}

// labelEnv is what every label subcommand needs: the label-capable accounts
// and the optional --account selection.
type labelEnv struct {
	accounts []models.AccountView // label-capable only
	account  *models.AccountView  // --account, when given
	emails   map[string]string
}

func withLabelEnv(accountRef string, fn func(*ipc.Client, *labelEnv) error) error {
	c, err := dial()
	if err != nil {
		return err
	}
	defer c.Close()
	raw, err := c.Call("accounts.list", nil)
	if err != nil {
		return err
	}
	var all []models.AccountView
	if err := json.Unmarshal(raw, &all); err != nil {
		return err
	}
	env, err := newLabelEnv(all, accountRef)
	if err != nil {
		return err
	}
	return fn(c, env)
}

func newLabelEnv(all []models.AccountView, accountRef string) (*labelEnv, error) {
	env := &labelEnv{emails: map[string]string{}}
	for _, a := range all {
		env.emails[a.ID] = a.Email
		for _, capName := range a.Capabilities {
			if capName == "labels" {
				env.accounts = append(env.accounts, a)
				break
			}
		}
	}
	if len(env.accounts) == 0 {
		return nil, fmt.Errorf("no account supports labels (Gmail only)")
	}
	if accountRef = strings.TrimSpace(accountRef); accountRef != "" {
		for i, a := range env.accounts {
			if a.ID == accountRef || strings.EqualFold(a.Email, accountRef) {
				env.account = &env.accounts[i]
				return env, nil
			}
		}
		return nil, fmt.Errorf("account %q not found or has no label support", accountRef)
	}
	return env, nil
}

func (e *labelEnv) accountForCreate() (*models.AccountView, error) {
	if e.account != nil {
		return e.account, nil
	}
	if len(e.accounts) == 1 {
		return &e.accounts[0], nil
	}
	return nil, fmt.Errorf("--account is required when %d accounts support labels", len(e.accounts))
}

// findLabel loads the catalog and resolves ref (name or provider ID).
func (e *labelEnv) findLabel(c *ipc.Client, ref string) (models.LabelView, error) {
	params := map[string]any{}
	if e.account != nil {
		params["account"] = e.account.ID
	}
	raw, err := c.Call("labels.list", params)
	if err != nil {
		return models.LabelView{}, err
	}
	var labels []models.LabelView
	if err := json.Unmarshal(raw, &labels); err != nil {
		return models.LabelView{}, err
	}
	return pickLabel(labels, e.emails, ref)
}

// pickLabel resolves ref against the catalog: an exact provider ID first, then
// a case-insensitive exact name. A name found in several accounts is ambiguous.
func pickLabel(labels []models.LabelView, emails map[string]string, ref string) (models.LabelView, error) {
	ref = strings.TrimSpace(ref)
	var byID, byName []models.LabelView
	for _, l := range labels {
		if l.ID == ref {
			byID = append(byID, l)
		}
		if strings.EqualFold(l.Name, ref) {
			byName = append(byName, l)
		}
	}
	matches := byID
	if len(matches) == 0 {
		matches = byName
	}
	switch len(matches) {
	case 0:
		return models.LabelView{}, fmt.Errorf("no label matches %q (see: dmail label list)", ref)
	case 1:
		return matches[0], nil
	}
	var where []string
	for _, l := range matches {
		where = append(where, emails[l.AccountID])
	}
	return models.LabelView{}, fmt.Errorf("label %q exists in several accounts (%s); pass --account", ref, strings.Join(where, ", "))
}

// parseColorPair reads "<bg>:<text>"; with allowNone, "none" means clear.
func parseColorPair(s string, allowNone bool) (bg, text string, err error) {
	s = strings.TrimSpace(s)
	if allowNone && strings.EqualFold(s, "none") {
		return "none", "", nil
	}
	bg, text, ok := strings.Cut(s, ":")
	if !ok || !isHexColor(bg) || !isHexColor(text) {
		return "", "", fmt.Errorf("color must be <background>:<text> with #rrggbb values (e.g. #16a765:#ffffff)")
	}
	return strings.ToLower(bg), strings.ToLower(text), nil
}

func isHexColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	_, err := strconv.ParseUint(s[1:], 16, 32)
	return err == nil
}

func parseThreadIDs(args []string) ([]any, error) {
	ids := make([]any, 0, len(args))
	for _, a := range args {
		n, err := strconv.Atoi(a)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("thread-id must be the numeric local id from 'dmail list' (got %q)", a)
		}
		ids = append(ids, float64(n))
	}
	return ids, nil
}

func printLabels(w io.Writer, labels []models.LabelView, emails map[string]string) {
	for _, l := range labels {
		color := "-"
		if l.ColorBg != "" {
			color = l.ColorBg + ":" + l.ColorText
		}
		sync := ""
		if l.Monitored {
			sync = "  " + i18n.T("[always synced]")
		}
		fmt.Fprintf(w, "%-32s  %-16s  %-15s  %s%s\n", l.Name, l.ID, color, emails[l.AccountID], sync)
	}
}

func printLabelResult(raw []byte, format string) error {
	if flagJSON {
		fmt.Println(string(raw))
		return nil
	}
	var l models.LabelView
	if err := json.Unmarshal(raw, &l); err != nil {
		return err
	}
	fmt.Printf(format, l.Name, l.ID)
	return nil
}

func confirmLabelDelete(in io.Reader, out io.Writer, l models.LabelView, email string) bool {
	fmt.Fprintf(out, i18n.T("Delete label %q of %s? It is removed from every thread and cannot be undone; mail is not deleted. [y/N] "), l.Name, email)
	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}
