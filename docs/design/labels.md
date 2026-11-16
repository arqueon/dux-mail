# Labels (v0.6.0)

Status: shipped in v0.6.0; approved 2026-11-15 (`.revision/etiquetas-2026-11-15.md`). Gmail only in
this release; IMAP and Microsoft do not declare the capability and the UI hides
every label affordance for them.

## Goals

- See the labels of each thread, open a per-label view, add and remove labels
  on one or several threads, and create, rename, recolor and delete labels,
  from the triage window, the CLI and the MCP server.
- Choose in Settings which labels are always synced.
- Move an archived thread back to the inbox from any non-inbox view.

Non-goals: per-message labels (ops stay thread-scoped), IMAP folders or
keywords, Microsoft categories, Gmail filters, moving a label under another
parent by drag and drop.

## Semantics

- A label belongs to one account. Its identity is the provider label ID
  (`Label_203`); its name may contain `/`, which the UI renders as nesting
  (`Auto/Compras` is `Compras` under `Auto`). A parent that does not exist as
  a label of its own is still shown as a non-selectable tree node.
- The same name can exist in several accounts. Views and the picker merge by
  **name** across accounts; the daemon resolves a name to the label ID of each
  account.
- Only user labels are listed. Gmail system labels (`INBOX`, `STARRED`,
  `CATEGORY_*`, ...) never appear as labels.
- Deleting a label removes it from every thread that had it and never deletes
  mail. It cannot be undone, so every surface asks for confirmation.
- Renaming a label does not rename its children (Gmail API behaviour).
- Colors: Gmail only accepts colors from its fixed palette. The palette lives
  in the Gmail provider and is exposed through `labels.palette`; the UI offers
  only those pairs.

## Core

### Provider contract (`core/internal/provider`)

```go
// CapLabels: user labels can be listed, managed and applied to threads.
CapLabels

type Label struct {
    ID        string // provider-native
    Name      string
    ColorBg   string // "#rrggbb" or ""
    ColorText string // "#rrggbb" or ""
}

// LabelManager is optional (assert, never assume) and requires CapLabels.
type LabelManager interface {
    ListLabels(ctx context.Context) ([]Label, error)
    CreateLabel(ctx context.Context, l Label) (Label, error)
    UpdateLabel(ctx context.Context, l Label) (Label, error) // by l.ID; empty fields keep their value
    DeleteLabel(ctx context.Context, labelID string) error
    ModifyLabels(ctx context.Context, threadIDs []string, add, remove []string) error
    LabelPalette() []LabelColor // allowed (background, text) pairs; empty = free colors
}

// LabelPager reads one provider page of the threads carrying a label, newest
// first. Tokens are opaque; changes are backfill and never notify.
type LabelPager interface {
    LabelPage(ctx context.Context, labelID, pageToken string) (Changes, string, error)
}
```

Gmail implements both over `users.labels.*`, `users.threads.modify` and
`users.threads.list(labelIds=...)`. `ModifyLabels` refuses system label IDs.
Errors are classified with `errdefs` like every other provider call. A label
that no longer exists is not an error on delete.

### Storage

New ent schema `Label`: `provider_label_id`, `name`, `color_bg`, `color_text`,
edge to `Account` (cascade delete), unique on (`provider_label_id`, account).
`Thread.labels` keeps holding provider label IDs (unchanged).

The catalog is refreshed from the provider: at daemon start, after every
create/update/delete, when `labels.list` is called with `refresh: true`, and
after a sync when the cached catalog is older than 10 minutes. A refresh that
changes anything publishes `labels.changed`.

### Operation queue

New op type `label` (ent enum + `sync.OpLabel`), batchable, with payload
`Labels{Add, Remove []string}` (provider label IDs). It follows the existing
optimistic pattern: the local `Thread.labels` is updated at enqueue time,
`Prev` keeps the previous state, and a permanently failed op reverts it and
publishes `op.failed`.

### IPC (same conventions as the existing `ops.*` and `threads.*` methods)

| Method | Params | Result |
|---|---|---|
| `labels.list` | `account?` (uuid), `refresh?` (bool) | `[LabelView]` for every account whose provider has `CapLabels` |
| `labels.palette` | `account` | `[{background, text}]` |
| `labels.create` | `account`, `name`, `colorBg?`, `colorText?` | `LabelView` |
| `labels.update` | `account`, `id`, `name?`, `colorBg?`, `colorText?` | `LabelView` |
| `labels.delete` | `account`, `id` | `{ok: true}` |
| `ops.label` | thread selection exactly as `ops.archive` takes it, plus `add?: [string]`, `remove?: [string]` | as `ops.archive` |
| `threads.fetchLabel` | `labelName` or (`account` + `label`), `next?` | same shape as `threads.fetchArchived` |
| `accounts.setMonitoredLabels` | `account`, `labels: [id]` | `{ok: true}`; persists the account config, rebuilds the provider and triggers a sync |

`ops.label` accepts label IDs **or names** in `add`/`remove`. A name is
resolved per thread account; a thread whose account has no label of that name
is skipped for that name (not an error). Unknown IDs are rejected.

`threads.list` keeps its `label` filter (a provider label ID, meant to be
combined with `account`) and gains `labelName`, which matches, in each account,
the label of that name. Both imply "not inbox-only", like today.

```jsonc
// LabelView
{
  "id": "Label_203",          // provider label ID
  "accountId": "…uuid…",
  "name": "Auto/Compras",
  "colorBg": "#16a765",       // "" when the label has no color
  "colorText": "#ffffff",
  "monitored": false          // listed in the account's always-synced labels
}
```

Events on the bus: `labels.changed` `{accountId}`.

`accounts.list` already tells the UI what each account can do; it must expose
the labels capability the same way the existing capabilities are exposed.

### CLI

```
dmail label list [--account <id|email>] [--json]
dmail label create <name> [--account <id|email>] [--color <bg>:<text>]
dmail label rename <label> <new-name> [--account <id|email>]
dmail label color <label> <bg>:<text>|none [--account <id|email>]
dmail label delete <label> [--account <id|email>] [--yes]
dmail label add <label> <thread-id>...
dmail label remove <label> <thread-id>...
```

`<label>` is a name (exact, case-insensitive) or a provider label ID.
`--account` is required only when the name is ambiguous or when creating with
more than one label-capable account. `delete` asks for confirmation unless
`--yes`.

### MCP

| Tool | readonly | draft-strict | draft | full |
|---|---|---|---|---|
| `list_labels` | yes | yes | yes | yes |
| `label_threads` (add / remove) | no | no | yes | yes |
| `create_label`, `update_label` (rename, color) | no | no | no | yes |
| `delete_label` | no | no | no | yes |

`list_threads` and `search_mail` accept a `label` argument (name or ID) and
every thread they return carries its label **names**. `label_threads` takes the
numeric local thread IDs the other tools use and label names or IDs.
`delete_label` states in its description that it is irreversible and removes
the label from every thread without deleting mail.

## Triage window (QML)

Everything below is hidden for accounts without the labels capability.

1. **Chips.** Each thread row and the open thread header show the thread's user
   labels: leaf name, full path in the tooltip, the label color when it has one
   and a Theme color otherwise. Long sets collapse to "+N".
2. **Label panel.** A collapsible tree of labels (merged by name across
   accounts, nested by `/`) next to the existing view switcher. Choosing a
   label opens its view: `threads.list {labelName}` for what is cached, plus
   `threads.fetchLabel` paging on demand, reusing the archived-history paging
   pattern (`Common/ArchiveHistory.js`).
3. **Label picker.** Opens with `l` on the current selection (one or several
   threads). A search field filters the labels; Space or click toggles; Enter
   applies through `ops.label`; when the text matches no label, the first row
   offers to create it (`labels.create`, then apply). Escape cancels.
4. **Move to inbox.** In every non-inbox view (label views, archived, search
   results) archived threads offer "Move to inbox" (button and shortcut),
   through the existing `ops.unarchive`.
5. **Manage labels** (Settings): per account, create, rename, recolor from
   `labels.palette`, delete with a confirmation that says mail is not deleted,
   and an "Always sync" toggle per label (`accounts.setMonitoredLabels`).
6. The window listens to `labels.changed` and refreshes its catalog.

## Project rules that apply

- English is the source for every user-visible string; add the `es` and `pt`
  overrides everywhere (`core/internal/i18n`, `quickshell/translations/*.json`);
  `make -C core test` must pass, including `scripts/check-i18n.sh`.
- Tests first for the Gmail label mapping against the fake API, the queue
  revert path, the name resolution in `ops.label`, and the MCP mode gating.
- No regression of the `dankmail` compatibility paths.
