# Changelog

All notable changes to **Dux Mail** (`dux-mail`, formerly **DankMail** — `dmail` daemon, `dmail mcp` server, Quickshell UI, and the `dankmailUnread` / **Dux Mail Unread** Dux Shell companion) are documented in this file.

## [0.6.4] - 2026-11-24

### Changed
- **Thinner selection border (`TriageWindow.qml`)**: the selected thread/draft row border goes from 2 px to 1 px (`selectionBorderWidth`), and the account-color strip is now inset by the border on the left, top, and bottom instead of only on the left, so it no longer paints over the border at the row's corners. The strip narrows by the border width while selected, keeping its right edge — and the row content — in place.

## [0.6.3] - 2026-11-24

### Changed
- **dcal aesthetic alignment (`TriageWindow.qml`, `Widgets/DankTooltip.qml`)** — approved via `.revision/estetica-dcal-2026-11-23.md` (8/8):
  - Selected thread and draft rows now use the dcal selection pattern: soft `primaryBackground` tint + 2 px `Theme.primary` border (replacing the flat `surfaceSelected` fill), with the account strip inset so the border reads uniformly.
  - Header and label panel are translucent dcal-style veils (`surfaceContainer` at 0.5 / 0.6 over the window surface) instead of solid fills.
  - Filter chips, the Labels chip, label-tree rows, and the message-step chips use the dcal active pattern: `primaryHover` tint (primary α 0.12) with `Theme.primary` Medium text instead of solid `primaryContainer`.
  - Unread threads show a quiet 8 px `Theme.primary` dot beside the sender name, in addition to the bold weight.
  - Tooltips switched to the dcal look: `surfaceContainerHigh` surface, `cornerRadius` 12, faint `outlineLight` border, surface text, and a 200 ms fade-in.
  - Region separators softened from `outlineMedium` to `outlineLight` (outlineMedium stays reserved for menu/toast borders).
- **dcal aesthetic alignment, Dux Shell popout (`dms-plugin/DankmailWidget.qml`)**: Inbox/Starred/Drafts tabs use the same dcal pattern (primary tint α 0.12 with primary Medium text instead of a solid primary fill); thread rows get a primary state layer (hover α 0.08 / press α 0.12, 100 ms OutCubic) instead of a surface hover; and each row now carries a Gmail-style sender avatar (initial over a stable per-sender accent derived from `Theme.accents`, mirroring the app's `senderColor` hash without hard-coded hexes) plus a 6 px account-color strip reusing the account-dot accent function.
- **Richer body rendering (`Common/BodyFormatter.js`, `TriageWindow.qml`, `ComposeModal.qml`)**: `format()` now accepts validated presentation tokens and the four render call sites pass them from the live theme — headings render in `Theme.primary`; fenced code blocks get a `Theme.surfaceContainer` background plus the theme's monospace family and size; inline code and `[image: …]` chips use the monospace family; tables drop the HTML4 1 px grid for a borderless layout with a `Theme.surfaceContainerHigh` tinted header row; and nested quote chains indent progressively (16 + 8 px per level) so reply histories read as a staircase. All options validate the same way as the existing colors and degrade cleanly when absent.
- **Icon family alignment with dcal / DankCalendar (`quickshell/assets/duxmail.svg`, `quickshell/assets/duxmail-tray.svg`)**: redesigned the app and tray icons as siblings of `dankcalendar.svg` / `dankcalendar-tray.svg` — same 64×64 canvas, light `#F5F0FF→#D0BCFF` gradient body, `#553C9A` header band, purple envelope-fold chevron, and a `#44337A` accent square echoing the calendar's highlighted "today" cell. Replaces the solid blue tile with red notification dot.

### Fixed
- **Reading pane clipped lines mid-word at the right edge (`TriageWindow.qml`)** — the actual root cause of the long-standing "text overflows to the right" report, which only showed at certain window widths. The preview column's implicit width was driven by its members' unwrapped implicit widths: the subject row (~667 px), the from-line chip row (~922 px), and the horizontally-scrolling message-selector flickable all reported their full unwrapped content as their layout preferred size. The column layout then never shrank below that poisoned value, every fillWidth sibling — including the body flickable — inherited it, and the editor rendered wider than the visible pane, clipping every line mid-word at the window edge (verified by OCR of the live window: 372 text rows ended exactly at the window edge; after the fix, zero). Fixes: `Layout.preferredWidth: 0` on the subject row, from row, message-selector, recipients, and attachments flickables (content-sized rows must never set the column's preferred width); `Layout.minimumWidth: 0` on the body and draft flickables; and the body/draft editors now use a fixed `x: Theme.spacingL` offset instead of centering against the content item, whose width can lag window resizes. Verified end-to-end with a qml6 layout harness, live geometry dumps, and OCR + pixel analysis of the captured window.
- **Long-token body wrapping (`quickshell/Common/BodyFormatter.js`)**: message bodies containing unbroken tokens (raw URLs, tracking IDs, base64 blobs, ASCII tables) overflowed past the right edge of the reading pane because Qt word wrap never splits a single token and the pane has no horizontal scroll. Inserted zero-width break opportunities (U+200B, a mandatory UAX #14 break point honored by Qt) every 24 characters into unbroken runs of 48+ characters, across paragraphs, inline `<code>` spans, and `<pre>` blocks. Display text only — `href` targets and other attributes stay byte-exact, and softening is idempotent for nested quotes. Additionally, fenced code blocks now render with `white-space:pre-wrap` because Qt lays out `<pre>` as non-wrapping, so pre-formatted lines overtook the pane edge regardless of break points. Verified empirically against Qt 6.12 (`qmlscene` harness): TextEdit honors U+200B in paragraphs, inline code, and table cells; real re-rendered bodies of three marketing emails now measure within the pane width.

## [0.6.2] - 2026-11-23

### Fixed
- **Draft attachment preservation & fail-closed MIME**:
  - Preserved `messageId` and `attachmentId` on `provider.AttachmentFile`, `gmail.draftItem`, and `drafts.MergeRemote` (reconciling local file paths on disk when still present).
  - Added synchronous outgoing attachment hydration (`hydrateOutgoingAttachments`) before `ops.compose`, `ops.reply`, and `drafts.save`, downloading remote draft attachments to local cache when needed and refusing to save or send (or delete the draft) if an attachment cannot be resolved or has an empty payload.
  - Made `mailmime.resolveAttachment` fail closed when both `Path` and `Data` are missing so 0-byte attachments can never be silently MIME-encoded and sent.
- **Direct attachment opening & error visibility (`TriageWindow.qml`, `ComposeModal.qml`, `DankMailService.qml`)**:
  - Extended `attachments.open` to open local file paths and draft attachments directly without requiring `threadId`.
  - Made attachment chips clickable inside draft previews and the compose modal (`openDraftAttachment`), flagged empty 0-byte attachments (`empty (0 B)`), and surfaced actionable error messages instead of silently falling back to webmail (reserving webmail fallback for providers without direct attachment download).

## [0.6.1] - 2026-11-23

### Changed
- **Thread list sender hierarchy & message counts**:
  - Added `lastSender` to `models.ThreadSummary` (populated from the newest cached message in `ListThreads` and `GetThread`, keeping the Sent view's outgoing recipients intact), so round-trip conversations (`A → B → A`) always identify the latest sender instead of the last first-appearance participant.
  - Formatted multi-message thread rows across the triage window (`quickshell/Common/Senders.js` + `TriageWindow.qml`), the Dux Shell companion (`dms-plugin/DankmailWidget.qml`), and `dmail list` with Gmail-style comma-separated participants: localized `"me"` (`"yo"` / `"eu"`), shortened earlier participants, emphasized latest sender, a compact thread message count badge when `messageCount > 1`, full chronological participant hover tooltips, and row avatars using the latest non-self participant when available.

### Fixed
- **In-place snooze preset chips (`TriageWindow.qml`)**: sized each preset pill from `Math.ceil(implicitWidth) + Theme.spacingS * 2` with `Text.NoWrap` so `In 1 hour`, `This evening`, `Tomorrow`, `This weekend`, and `Next week` no longer truncate inside the row hover overlay.

## [0.6.0] - 2026-11-15

### Added
- **Labels (Gmail accounts)** — see `docs/design/labels.md`:
  - New provider capability `CapLabels` with the optional `provider.LabelManager` (list, create, update, delete, modify, palette) and `provider.LabelPager` interfaces. Only the Gmail provider implements them (over `users.labels.*`, `users.threads.modify` and `users.threads.list(labelIds)`); IMAP and Microsoft do not declare the capability. `ModifyLabels` refuses system label IDs, and deleting a label removes it from every thread without ever deleting mail. The label color palette is the fixed Gmail set, exposed through `labels.palette`.
  - New `Label` ent schema (additive migration) caching each account's label catalog; refreshed at daemon start, after every create/update/delete, on `labels.list {refresh: true}` and after a sync when the cached catalog is older than 10 minutes. Publishes the `labels.changed` bus event.
  - New `label` pending-op type with optimistic update of the thread labels and revert (plus `op.failed`) on permanent failure. A batch that fails is now unwound newest-first, so every op type restores the oldest snapshot.
  - IPC: `labels.list`, `labels.palette`, `labels.create`, `labels.update`, `labels.delete`, `ops.label` (label IDs or names, resolved per account), `threads.fetchLabel`, `accounts.setMonitoredLabels`; `threads.list` gains `labelName`; `accounts.list` reports each account's `capabilities` (`["labels"]` for Gmail).
  - CLI: `dmail label list|create|rename|color|delete|add|remove`.
  - MCP: `list_labels`, `label_threads` (`draft`/`full`), `create_label`, `update_label`, `delete_label` (`full` only); `list_threads` and `search_mail` accept `label` and return `labelNames`.
  - English source strings with `es` and `pt` overrides for the new CLI output.
  - Triage window: label chips on thread rows and in the open thread header; a **Labels** panel with the label tree (merged by name across accounts, nested by `/`) whose views page archived threads on demand; a label picker on `l` for one or several threads (search, toggle, apply, create inline); **Manage labels** in Settings (create, rename, recolor from the Gmail palette or remove the color, delete with confirmation, and an **Always sync** toggle per label). All of it is hidden for accounts without the labels capability.
  - Keyboard shortcut `i` moves the selected archived threads back to the inbox in any non-inbox view.
- **Select all** in the triage list: once a thread is checked, the bulk toolbar offers a select-all button (also `Ctrl+A`) that checks every loaded thread of the current view; pressing it again clears them.

### Changed
- The strip that marks which account a thread or draft row belongs to is twice as wide (6 px).

## [0.5.1] - 2026-11-13

### Fixed
- **Dux Shell companion theme tokens, native tooltips, and manifest clarity**:
  - Replaced the hardcoded hex `accountPalette` and `contrastOnDot` in `dms-plugin/DankmailWidget.qml` with harmonized `Theme.accents` (`container` / `onContainer` pairs with `Theme.primary` / `Theme.onPrimary` fallback) so per-account filter dots follow the active Dux Shell theme in both dark and light modes.
  - Replaced the custom `accountTooltip` `Rectangle` and timer in `dms-plugin/DankmailWidget.qml` with `StateLayer` (`qs.DCommon.Widgets`), using Dux Shell's built-in tooltip host and state/ripple feedback.
  - Replaced remaining literal pixel sizes and margins in `dms-plugin/DankmailWidget.qml` with `Theme.*` tokens and `popoutHeight`-derived layout bounds.
  - Updated `dms-plugin/plugin.json` and `dms-plugin/README.md` to clearly describe the bar badge, per-account filter, and triage popout while noting that the Dux Mail desktop app and `dmail` daemon are installed separately.

## [0.5.0] - 2026-11-13

### Added
- **Renamed to Dux Mail (`dux-mail`) with zero-breakage migration**:
  - Renamed application branding, desktop entry (`org.arqueon.duxmail.desktop`), icons (`duxmail.svg`, `duxmail-tray.svg`), Go module (`github.com/arqueon/dux-mail/core`), repository (`arqueon/dux-mail`), and AUR packages (`dux-mail`, `dux-mail-git`, `dms-shell-plugin-dux-mail` with `provides`/`conflicts`/`replaces` for `dankmail*`).
  - Preserved `dmail` binary, `dmail.service`, and internal DMS widget ID `"dankmailUnread"`, with automatic fallback to existing `~/.config/dankmail`, `~/.local/share/dankmail`, keyring service `dankmail`, and a `dankmail.sock -> duxmail.sock` socket symlink.
- **Native Model Context Protocol server (`dmail mcp`) & headless agent backend**:
  - Added `dmail mcp` (`stdio` JSON-RPC 2.0, protocol `2024-11-05`) exposing 27 tools across accounts, local/remote search, thread & message inspection, silent attachment fetching/saving, triage operations, contacts, drafts, and sending, with `--mode full` (default), `draft`, `draft-strict`, and `readonly` permission profiles.
  - Wrapped untrusted message bodies, snippets, and attachment text previews in `<untrusted_email_content>` / `<untrusted_attachment_content>` tags and ensured zero secret/token exposure over IPC or MCP.
  - Added daemon auto-start in `dmail mcp` and headless mode in `dmail run` (skipping Quickshell `qs` launch when `WAYLAND_DISPLAY` and `DISPLAY` are unset, e.g., over SSH).
  - Added silent `attachments.fetch` IPC (`base64` / cache path + automatic UTF-8 `textPreview` for text/Markdown/CSV/JSON/XML/HTML attachments) alongside interactive `attachments.open` and `attachments.saveAs`.
  - Added server-side full-history `SearchRemote` (`provider.RemoteSearcher`) for **Microsoft Graph** (`$search` KQL with message-to-conversation grouping) and **IMAP** (`UID SEARCH CHARSET UTF-8 TEXT` with `ASCII` fallback and thread grouping), completing parity with Gmail remote search.
- **Rich Markdown & HTML email sending, draft formatting toolbar, and `dmail send` CLI**:
  - Upgraded `core/internal/mailmime` with full Markdown-to-HTML rendering (headings `#..######`, bold, italic, strikethrough `~~`, links, nested bulleted/numbered lists, blockquotes, inline & fenced code blocks, horizontal dividers, and aligned pipe tables) and `SanitizeOutgoingHTML` for custom `htmlBody` payloads (stripping scripts/iframes/forms/event handlers/`javascript:` URLs and auto-distilling a `text/plain` fallback when `body` is omitted).
  - Added `htmlBody` and `format` (`auto` | `markdown` | `html` | `plain`) across `ops.compose`, `ops.reply`, `ops.forward`, `drafts.save`, local `drafts.json` persistence (preserving local Markdown source across Gmail `MergeRemote` syncs), and MCP compose/reply/forward/draft tools.
  - Added `dmail send` CLI (`--account`, `--to`, `--cc`, `--bcc`, `--subject`, `--body`, `--body-file`, `--html`, `--html-file`, `--format`, `--attach`).
  - Upgraded `ComposeModal`, inline quick reply in `TriageWindow`, and the **Drafts** detail pane with a full Markdown formatting toolbar, `Markdown` / `HTML` / `Plain text` mode selector, sanitized rich preview, format badges, and one-click **Send** from saved drafts.
- **Compact filterable account dots in the DMS bar companion**:
  - Replaced the vertical per-account text rows in `dms-plugin/DankmailWidget.qml` with compact `20×20` color-coded filterable dots with unread hover tooltips, matching the desktop triage window.

## [0.4.1] - 2026-11-12

### Fixed
- **Dux Shell 2.0+ `DCommon` widget migration (`#14`)**:
  - Migrated `dms-plugin/DankmailWidget.qml` and `dms-plugin/DankmailSettings.qml` from the deprecated `qs.Widgets` `Dank*` shims (`DankActionButton`, `DankFlickable`, `DankIcon`) to `DActionButton`, `DFlickable`, and `DIcon` from `qs.DCommon.Widgets`.
  - Updated `dms-plugin/plugin.json` (`requires_dms: ">=2.0.0"`) and `dms-plugin/README.md` so DMS 1.x installations are not offered an incompatible companion update.

## [0.4.0] - 2026-11-12

### Added
- **One-click OAuth (`dankcalendar` parity), CLI account setup & account filter tooltips**:
  - Added Step 0 one-click sign-in (`Connect Google Account` / `Connect Microsoft Account`) with PKCE S256 in `AccountAddModal`, reusable OAuth client credential resolution (system keyring + `0600` local fallback + existing accounts + env + `-ldflags -X`), and a native `Import client_secret.json` file picker button.
  - Added `dmail account setup google|microsoft` and `dmail account add google|microsoft|imap` CLI commands, plus hover tooltips (`DankTooltip`) showing each account's email address and unread count on the header account filter dots.
- **Full IMAP + SMTP provider**:
  - Added `core/internal/provider/imap` (`go-imap/v2` + `net/smtp` STARTTLS/TLS, with loopback self-signed TLS tolerance for Proton Mail Bridge) supporting sync, archive/sent pagination, flag/folder triage, replies, compose, and attachments, automatically un-parking previously stored IMAP accounts.
- **OS-native attachment opening & Save as**:
  - Clicking an attachment chip downloads the part on demand into `~/.cache/dankmail/attachments/` (`0600`) and opens it via `xdg-open` (`attachments.open`), with a secondary **Save as…** folder picker button (`attachments.saveAs`) and webmail fallback on error.
- **Snoozed mailbox view, weekend preset & Move to Inbox**:
  - Added a **Snoozed** filter chip ordered by wake time (`SnoozedUntil ASC`), wake-time badges on snoozed threads, **Unsnooze** actions (`ops.unsnooze`), and a **This weekend** (Saturday 09:00) snooze preset, wrapping inline row snooze presets within the thread column margins so options never clip at the window edge.
  - Added **Move to Inbox** (`ops.unarchive`) across the preview toolbar, thread row hover actions, bulk selection bar, and the `dankmailUnread` bar popout for archived and other non-inbox threads, flushing pending undoable actions immediately on filter switches or window close.
- **HTML-to-Markdown message distillation & full body resync**:
  - Added `core/internal/mailtext` (`html-to-markdown/v2`) across Gmail, Microsoft Graph, and IMAP so HTML messages retain readable link labels, headings, lists, and multi-column data tables while unwrapping layout tables, sanitizing `<img>` / linked-banner nodes, and stripping hidden preheaders and tracking pixels, plus a per-account full resync button in `SettingsModal`.
  - Replaced regex-only Markdown link/image parsing in `BodyFormatter.js` with balanced token scanners so cached and incoming links/images with optional titles, parentheses or spaces in URLs, multi-line block wrappers, and `data:` URIs never leak raw `!` or `(...)` syntax.
- **Selectable & copyable `Subject`**:
  - Replaced static subject labels in thread and draft previews with read-only mouse-selectable `TextEdit` controls and one-click clipboard copy buttons.

### Fixed
- **Dux Shell (`dms-theme-sync`) theme roles, typography, and WCAG contrast**:
  - Updated `Theme.qml` to watch `~/.cache/dms/dms-colors.json` and `~/.cache/dms/dms-typography.json` (with `~/.cache/DankMaterialShell/` fallback), honor `"mode": "dark"|"light"`, load full Material 3 `on_*` / `secondary_container` / `tertiary_container` / `inverse_surface` roles, and enforce WCAG contrast safeguards.
  - Replaced all 30 `primaryContainer`+`primary` low-contrast buttons/chips across `quickshell/` and `dms-plugin/` with `Theme.primary`+`Theme.primaryText` (filled actions) or `Theme.primaryContainer`+`Theme.onPrimaryContainer` (tonal chips), and removed hardcoded hex colors in `TriageWindow.qml`.

## [0.3.12] - 2026-10-15

### Added
- **Drafts section & draft editing**:
  - Added a **Drafts** view in both the main `TriageWindow` filter bar and the `dankmailUnread` DankMaterialShell bar popout.
  - Added local draft persistence (`drafts.json`) synchronized with Gmail `users.drafts` (`ListDrafts`, `GetDraft`, `CreateDraft`, `UpdateDraft`, `DeleteDraft`) over IPC (`drafts.list`, `drafts.save`, `drafts.delete`, `ui.editDraft`).
  - Clicking a draft in the bar popout or clicking **Edit draft** in the triage window opens `ComposeModal` pre-populated with its recipients (`To`, `Cc`, `Bcc`), subject, body, and attachments.
- **Rich composer, URL links, `Cc`/`Bcc`, and file attachments**:
  - Added `Cc / Bcc` recipient fields with contact autocomplete, a formatting and URL link insertion toolbar (`**bold**`, `*italic*`, `[label](url)`, bulleted/numbered lists, quotes, code, horizontal rules), and a live rich-text **Preview** toggle in `ComposeModal`.
  - Added a native DankMail-styled multi-file attachment picker (`FilePickerModal.qml` backed by `fs.list` IPC) in both `ComposeModal` and inline quick reply, avoiding unstyled `QtQuick.Dialogs` fallbacks under `QT_QUICK_CONTROLS_STYLE=Material`.
  - Updated `mailmime` to build `multipart/mixed` base64 attachments and `multipart/alternative` (`text/plain` + `text/html`) when messages contain Markdown links or formatting.
- **Thread message selector & copyable sender/recipient addresses**:
  - Added a horizontal message selector bar in the preview pane for multi-message threads so any message in a conversation can be inspected directly.
  - Added RFC 2047 MIME word decoding (`=?utf-8?q?...?=`, `=?utf-8?b?...?=`) and interactive address chips for sender (`From`) and recipients (`To`, `Cc`) with one-click clipboard copy and a **Copy all emails** action.

### Fixed
- **Reader formatting & link activation**:
  - Unwrapped Gmail redirect URLs (`https://www.google.com/url?q=...`), merged soft-wrapped `label <https://...>` plain-text links, linkified bare domains (such as `plataformas.udgplus.udg.mx`) and `<email@domain>` addresses, and improved multi-line `*...*` emphasis and nested list parsing in `BodyFormatter.js`.
  - Fixed link hover cursor detection and click handling on `TextEdit` (`selectByMouse: true`) using `TapHandler` + `linkAt` and daemon-side `ui.openUrl` (`xdg-open`).
  - Fixed chip width calculations (`Layout.preferredWidth` / `contentWidth` with `Text.NoWrap`) in `RowLayout` headers and suppressed false-positive BCC warnings on forwarded mail when recipients are present.

## [0.3.11] - 2026-10-02

### Added
- Resumable mailbox sync checkpoints across quota pauses and restarts.
- **Sent** mailbox filter with on-demand pagination (`threads.fetchSent`) for Gmail and Microsoft Graph.
- **Not spam** rescue action (`ops.unspam`) for single threads and cross-account bulk selections.

### Fixed
- Companion socket reconnection recovery and translation installation in `dms-plugin`.

## [0.3.10] - 2026-09-30

### Fixed
- Paginated Gmail archive history (`threads.fetchArchived`) so **Archived** loads unstarred archived conversations beyond the local cache.

## [0.3.9] - 2026-09-29

### Added
- **Archived** quick filter in the triage window.
