# ordo — specification

Draft 0.1, 5 October 2026. Nothing is implemented yet.

`ordo` keeps the agenda of meetings. A **meeting** is a recurring or one-off
body (a board, a steering committee, a weekly team review). Each date it meets
is a **sitting**. People and agents propose **items**; the chair accepts,
orders and times them; the sitting deals with them; what comes out of each
item (a summary, a decision, actions) becomes the **minutes**. An item that is
not dealt with moves to the next sitting on its own.

`ordo` is the record of items and their state. It does not hold the
discussion around an item, the files, or the memory of decisions: other tools
do (a dossier manager, an artefact store, a memory). It reaches them through
hooks and identifiers, never by reading their data.

## 1. Principles

1. **One record of items.** An item has one home: its meeting in `ordo`. Other
   tools point at it; `ordo` points back at where the item came from.
2. **Items survive sittings.** An item keeps its id when it is deferred. Its
   history across sittings stays in one place.
3. **Plain files.** A store is a directory of Markdown files with YAML
   frontmatter. It is readable, diffable and versionable without `ordo`.
4. **Spheres do not meet.** Each sphere has its own root. No action, search
   or hook crosses spheres.
5. **Agnostic.** `ordo` knows no particular calendar, dossier manager, memory
   or artefact store. It reads dates from a calendar provider and announces
   changes through hooks.
6. **Nothing leaves without the user.** `ordo` sends no e-mail and no
   invitation. It renders documents; a human or a hook decides where they go.
7. **AI-native CLI.** `ordo` follows `aclemen1/ai-native-cli`: single Go
   binary, embedded skill, `ordo schema`, JSON envelope `{ok, result|error}`,
   `--format json|text`. The same actions are served over MCP.

## 2. Overview

```
 human · agent · tool (office, routine, …)
        │ CLI · MCP
        ▼
 ┌──────────────────── ordo (Go binary) ─────────────────────┐
 │ actions: meeting · sitting · item · outcome · render …    │
 │                                                           │
 │ store (Markdown + YAML, per sphere) · index (rebuildable) │
 │ calendar providers · hooks · renderer                     │
 └──────┬──────────────────────┬─────────────────────┬───────┘
        ▼                      ▼                     ▼
   calendar (ICS,         hooks (office notify,   rendered agenda
   command)               mnemo remember,         and minutes
                          artefact put)           (md, html, docx, pdf)
```

## 3. Data model

### 3.1 Meeting

| Field | Meaning |
|---|---|
| `alias` | short upper-case name, unique in the sphere: `RDIR` |
| `title` | `Séance de direction` |
| `sphere` | `perso`, `pro`, … |
| `rrule` | iCalendar RRULE with `DTSTART` and time zone; absent for a one-off meeting |
| `duration` | default length of a sitting: `90m` |
| `chair`, `members` | names or e-mail addresses |
| `calendar` | provider and match rule (§6) |
| `refs` | free links, e.g. `office:U-RDIR` |
| `defaults` | default item duration, default kind |

### 3.2 Sitting

A sitting is one date of a meeting. Its id is `<alias>-<YYYY-MM-DD>`, with a
suffix `-2` for a second sitting on the same day.

| State | Meaning | Entered by |
|---|---|---|
| `planned` | the date exists; the agenda is open | created from the RRULE or by `sitting add` |
| `frozen` | the agenda is final and rendered | `sitting freeze` |
| `held` | the sitting took place; outcomes are being recorded | `sitting hold` |
| `minuted` | the minutes are approved and rendered | `sitting minute` |
| `cancelled` | the sitting does not take place; its items move on | `sitting cancel` |

A sitting carries its date, start time, place, an ordered list of item ids
with their slot (start offset, duration), and attendance once held.

`frozen` can be reopened (`sitting reopen`) until the sitting is held. The
reopening is logged and the next freeze renders a new version.

### 3.3 Item

An item's id is `<alias>-<n>`, a counter per meeting: `RDIR-17`.

| Field | Meaning |
|---|---|
| `title` | one line |
| `owner` | who brings it |
| `kind` | `info`, `discussion`, `decision` |
| `duration` | planned length: `15m` |
| `expected` | for a decision: the question put to the meeting |
| `attachments` | references: `artefact://pro/01J…`, a path, a URL |
| `refs` | where it comes from or is followed: `office:U-0042`, `gmail:thread/…` |
| `sitting` | the sitting it is planned for, or empty for "next" |
| `state` | see below |
| `history` | one entry per sitting it was planned for, with what happened |

| State | Meaning |
|---|---|
| `proposed` | someone asked for it; the chair has not accepted it |
| `accepted` | on the agenda of its sitting |
| `done` | dealt with; its outcome is recorded |
| `deferred` | not dealt with; planned for the next sitting, back to `accepted` there |
| `dropped` | withdrawn, with a reason |

Rules:

- An item with no `sitting` goes to the next sitting that is still `planned`.
- When a sitting is held, every `accepted` item without an outcome becomes
  `deferred` at `sitting minute`, unless the chair dropped it.
- Cancelling a sitting moves its items to the next sitting, state unchanged.
- `proposed` items appear in a separate list until accepted; `freeze` refuses
  a sitting that still has `proposed` items unless `--leave-proposed` moves
  them to the next sitting.

### 3.4 Outcome

What came out of an item in one sitting.

| Field | Meaning |
|---|---|
| `summary` | short text for the minutes |
| `decision` | the decision taken, for a `decision` item |
| `actions` | list of `{what, who, due}` |
| `next` | `done` or `deferred` |
| `by` | who wrote it: a person, or `agent:<name>` |
| `status` | `draft` or `approved` |

An action can carry a `ref` once a tool follows it (e.g. the office dossier
opened for it).

Outcomes come from two places, often both: the chair notes them live in the
TUI during the sitting, and an agent completes them afterwards from a
transcript. An outcome written by an agent is always a `draft`. `sitting
minute` shows the drafts and approves them all; nothing reaches the minutes
unapproved.

### 3.5 Identifiers and links

| Object | Id | Example |
|---|---|---|
| meeting | alias | `RDIR` |
| sitting | alias + date | `RDIR-2026-10-08` |
| item | alias + counter | `RDIR-17` |
| outcome | item + sitting | `RDIR-17@RDIR-2026-10-08` |

Other tools cite `ordo://<sphere>/<id>`. Ids never change and are never
reused, even after a drop.

## 4. Storage

### 4.1 Store

One root per sphere, declared in the configuration:

```
~/ordo/
  pro/
    .ordo/            index.sqlite (rebuildable), counters, lock
    RDIR/
      meeting.md      frontmatter: §3.1 ; body: free notes
      items/
        RDIR-17.md    frontmatter: §3.3 with outcomes in history ; body: notes
      sittings/
        2026-10-08.md frontmatter: §3.2 ; body: free notes of the chair
      rendered/
        2026-10-08-agenda-v1.md
        2026-10-08-minutes.md
  perso/
    …
```

- Every write is atomic: temporary file, then rename, under the sphere lock.
- Each action appends a line to the item's or sitting's `log` (who, when,
  what), so the history is complete without a VCS.
- Every action reads the files themselves, so a file edited by hand is
  picked up at the next action. An index (SQLite) may come later as a cache
  for large stores; it would never be the source.
- A sphere declares `vcs: jj | git | none` (default `jj`). With a VCS,
  `ordo` commits after each write action, message `<action> <id>`
  (`item defer RDIR-17`), so the history of a meeting is read with the VCS.
  `ordo init` creates the repository when absent.

### 4.2 Sittings from the RRULE

`ordo` creates the files of the next sittings lazily: when an action needs
"the next sitting" or when `sitting ls` looks ahead (default: next 3). Past
sittings never appear on their own: a sitting that was not created is a
sitting that did not happen.

## 5. Public interface

### 5.1 Actions

| Action | Effect |
|---|---|
| `meeting add <alias> --title … [--rrule …] [--duration] [--chair] [--member]… [--calendar …] [--ref]…` | creates a meeting |
| `meeting ls`, `meeting show <alias>`, `meeting edit <alias> --set <field>=<value>` | reads and edits |
| `sitting ls [<alias>] [--state] [--ahead N] [--since]` | lists sittings |
| `sitting show <sitting\|alias>` | the agenda: ordered items, slots, total time against `duration`, proposed items apart; an alias means its next sitting |
| `sitting add <alias> --date … [--time] [--place]`, `sitting move <sitting> --date …`, `sitting cancel <sitting> [--reason]` | one-off changes |
| `sitting freeze <sitting> [--leave-proposed]`, `sitting reopen <sitting>` | closes or reopens the agenda; freeze renders the agenda |
| `sitting hold <sitting> [--present …] [--excused …]` | the sitting took place |
| `sitting minute <sitting>` | approves the minutes, defers what is left, renders the minutes |
| `sitting unhold <sitting>`, `sitting unminute <sitting>`, `sitting restore <sitting>` | take back a hold (frozen again, or planned), approved minutes (held again, items back as before, agent outcomes drafts again, rendered minutes removed; refused if an item changed since), a cancellation (planned again) |
| `item restore <id> [--accept]`, `item undefer <id>` | take back a drop (proposed, or accepted) or a done (on the agenda again); take back a deferral (back to the sitting it came from when still open, accepted) |
| `item add <alias> <title> [--owner] [--kind] [--duration] [--expected] [--attach]… [--ref]… [--sitting] [--accept]` | proposes an item (accepted at once with `--accept`); returns its id |
| `item ls [<alias>] [--state] [--owner] [--ref <ref>] [--search <text>]` | lists items; `--ref office:U-0042` finds the item of a dossier |
| `item show <id>`, `item edit <id> --set <field>=<value>` | reads and edits |
| `item accept <id>…`, `item defer <id> [--to <sitting>]`, `item drop <id> --reason …` | moves items in their cycle |
| `item order <sitting> <id>…` | sets the order; ids not named keep their relative order after |
| `outcome set <id> [--sitting] [--summary] [--decision] [--action "what\|who\|due"]… [--next done\|deferred] [--by]` | records what came out of an item; `--by agent:<name>` writes a draft |
| `render <sitting> --doc agenda\|minutes [--to md\|html\|docx\|pdf] [--out]` | renders a document (§8) |
| `actions ls [<alias>] [--who] [--state open\|done\|all] [--due-before]` | actions across sittings of a meeting or the sphere, open first by due date |
| `actions done <item> <n> [--sitting] [--undo]` | marks action n of an outcome done, or open again; rewriting the outcome keeps it |

Every action takes `--sphere` (or `ORDO_SPHERE`) and returns the envelope.
Errors name the canonical invocation, echo the offending values and show an
example.

### 5.2 Operation and meta commands

| Command | Role |
|---|---|
| `ordo init --sphere <name> --root <dir>` | declares a sphere |
| `ordo import gtasks …` | one-off import (§10) |
| `ordo reindex`, `ordo doctor` | upkeep; doctor checks calendars, hooks, renderer |
| `ordo mcp [--spheres pro]` | MCP over stdio, limited to the spheres given |
| `ordo tui` | terminal interface (§11) |
| `ordo schema`, `ordo skill show\|install`, `ordo version` | AI-native CLI |

### 5.3 Surfaces

The actions are defined once (action specs) and served as CLI commands and as
MCP tools (one tool per action, named `<category>_<action>`: `item_add`).
Sittings and items are also MCP resources `ordo://<sphere>/<id>`.

- `ordo mcp --spheres <list>` serves only those spheres; with one sphere,
  `sphere` defaults to it.
- Everything done through MCP is logged as `agent:…`, and an outcome set
  through MCP is always a draft: a `by` without the `agent:` prefix gets it.

## 6. Dates and calendars

The RRULE of a meeting says when it meets. The calendar says when it really
meets: a sitting moved in the calendar is moved in `ordo`.

- A meeting's `calendar` names a provider and a match rule (event title
  pattern, or the event's recurring id).
- Sources are named under `calendars:` in the configuration: `ics` (`path`
  or `url`; recurring events expanded, EXDATE and RECURRENCE-ID applied) and
  `command` (`run: [program, args…]`, called with `ORDO_FROM` and `ORDO_TO`
  in RFC 3339, printing `[{uid, start, end, title, location, status,
  recurring_id, original_start}]`). The command lets a user plug a CLI such
  as `gws` without `ordo` knowing it.
- `ordo meeting calendar <alias> --source <name> --match <regex>
  [--recurring-id <id>]` links a meeting to its events.
- `ordo sitting sync [<alias>] [--days 90]` reads the events and reconciles.
  It is explicit, not run at every read: a routine runs it on a schedule.
  An event finds its sitting by the uid recorded on it, then by its
  `original_start`, then by its start date.

| Calendar | `ordo` |
|---|---|
| event on an RRULE date | sitting keeps its id; takes the event's time and place when they differ |
| event moved to another day | the sitting moves; its id keeps the original date |
| event cancelled | sitting cancelled with reason `calendar`, its items moved on; it comes back if the event does |
| event with no RRULE date | one-off sitting added, carrying the event uid |
| RRULE date with no event | sitting kept, reported as `unconfirmed` in the result |

- A sitting already `held` or `minuted` is never changed by the calendar.
- A sync that changes nothing writes and commits nothing.
- Without a calendar, the RRULE alone decides.

## 7. Hooks

`ordo` announces events to commands named in the sphere's configuration. A
hook receives the event as JSON on standard input — `event`, `sphere`,
`meeting`, `at`, `by` and the payload — with `ORDO_SPHERE`, `ORDO_EVENT`,
`ORDO_MEETING` and `ORDO_HOOK_DEPTH` in its environment, in the store's
directory, with a timeout (30 s by default).

- Hooks run after the commit, once the lock is released, so a hook may call
  `ordo` again. A hook started from a hook has a greater depth; at depth 3
  hooks are skipped.
- A failing hook never undoes the action: the action succeeds and the
  failure comes back in `warnings` of the envelope (stderr in text).

| Event | Payload | Typical use |
|---|---|---|
| `item.added`, `item.accepted`, `item.dropped` | item (and reason) | tell the dossier the item came from |
| `item.deferred`, `item.moved` | item, from, to | tell the dossier its item moved |
| `outcome.set` | item, sitting, outcome | tell the dossier what was decided |
| `action.added` | item, sitting, action — only for an action new in the outcome | open a dossier for the action |
| `sitting.moved`, `sitting.cancelled` | sitting (moved items; `source: calendar` from a sync) | tell the meeting's dossier |
| `sitting.frozen` | sitting, rendered agenda path, items | deposit the agenda in an artefact store |
| `sitting.held` | sitting | |
| `sitting.minuted` | sitting, rendered minutes path, done, deferred | deposit the minutes; remember the decisions |

Hooks are filtered by event (`*` for all) and by meeting:

```yaml
spheres:
  pro:
    hooks:
      - on: [outcome.set, item.deferred]
        meetings: [RDIR, OPS]
        run: ["office-notify-from-ordo"]
        timeout: 20s
```

## 8. Rendering

- `render <sitting> --doc agenda|minutes --to md|html|docx|pdf [--out]`
  produces a document from a sitting and its items, with a Go template per
  document. Built-in templates exist in English and French (`lang` of the
  meeting, then of the sphere, then `en`); a sphere can name its own
  templates (`render.templates.agenda`, `.minutes`).
- `md` and `html` are rendered by `ordo` itself. `docx` and `pdf` go through
  pandoc (`render.reference_doc` of the sphere for docx, `pdf_engine` for
  PDF, default xelatex).
- `freeze` writes `rendered/<date>-agenda-vN.md` (`v2` after a reopen) and
  `minute` writes `rendered/<date>-minutes.md`, in the same commit; they are
  never overwritten, and their paths go to the hooks.
- `render` of a frozen agenda or approved minutes starts from that final
  Markdown, and a docx or PDF made from it is committed next to it.
  Anything else is a draft, marked as such, written to `--out` or to a
  temporary directory outside the store.

## 9. Spheres and access

- A sphere is a root and a name. `--sphere` (or `ORDO_SPHERE`) is required by
  every action; there is no default sphere in the configuration.
- `ordo mcp --spheres <list>` serves only those spheres; a call naming
  another sphere fails.
- Hooks run with the sphere of the event; a hook configured for one sphere is
  never run for another.
- `ordo://` references and `refs` are kept as text; `ordo` never follows a
  reference into another sphere.

## 10. Import from Google Tasks

A one-off import, for users who kept one task list per meeting.

```
ordo import gtasks --sphere pro --meeting RDIR --from tasks.json [--dry-run]
```

- Input: the JSON a task CLI prints for one list (`title`, `notes`, `due`,
  `status`, `links`). `ordo` does not call Google itself.
- Each open task becomes a `proposed` item; title and notes are kept; a link
  to an e-mail becomes a `ref`; completed tasks are skipped unless
  `--with-completed`.
- `--dry-run` prints the items without writing them.

## 11. TUI

`ordo tui`, Bubble Tea v2, keys aligned with the TUIs of `office` and
`routine`.

| View | Shows | Actions |
|---|---|---|
| Meetings | alias, next sitting, items on the agenda and proposed | open (`enter`), actions (`A`) |
| Agenda | a sitting: ordered items, slots, outcome marks, total against duration, proposed items apart; under the list, a pane with the selected item (owner, deferrals, question, attachments, refs, notes, outcome, and what the ref commands say about the refs' targets) | `n` new, `a` accept, `d` defer, `x` drop, `J`/`K` move, `+`/`-` 5 min, `e` edit, `f` freeze, `r` reopen, `h` hold, `m` minutes, `[`/`]` other sitting, `l` live |
| Item | fields, notes, history with outcomes and actions, the full text of each ref's target, log | `e` edit in `$EDITOR` (then committed) |
| Sitting (live) | the current item, a timer per item, elapsed against plan, the same pane for the current item | `space` timer, `n`/`p` item, `s` summary, `D` decision, `t` action, `-` defer, `h` hold |
| Actions | actions by due date, overdue in red | `space` done or open, `o` show done |

## 12. Configuration

`~/.config/ordo/config.yaml`:

```yaml
spheres:
  perso: { root: ~/ordo/perso, vcs: jj }
  pro:
    root: ~/ordo/pro
    vcs: jj
    render:
      lang: fr
      reference_doc: ~/templates/house-style.docx
    hooks:
      - on: [outcome.set, item.deferred]
        run: ["office-notify-from-ordo"]
    refs:
      office:
        show: ["office", "show", "{id}", "--format", "text"]
calendars:
  work:
    type: command
    run: ["gws-events", "--profile", "work"]
render:
  pandoc: pandoc
  pdf_engine: xelatex
```

Hooks belong to a sphere, so a hook of one sphere never sees another.

`refs` say how to summarise the target of an item's ref, by scheme: for
`office:U-0042`, `{id}` becomes `U-0042`. The TUI shows that text for the
selected item, rendered as Markdown and refreshed after a minute (item
notes are rendered as Markdown too), and `ordo item show --with-refs`
returns it to agents. Refs of a scheme with no command stay plain text.

A meeting's own settings live in its `meeting.md`.

## 13. Phases

1. **Core**: store, meetings, sittings from RRULE, items and their cycle,
   outcomes, `schema`, `skill`, CLI. Tests on a throwaway store.
   (Written 5 October 2026; `actions ls` comes with the TUI.)
2. **Render**: Markdown and HTML agenda and minutes, templates, pandoc.
   (Written 5 October 2026: built-in templates in English and French,
   `lang` per meeting or sphere.)
3. **MCP**: `ordo mcp`, resources. (Written 5 October 2026.)
4. **Calendars**: `ics` and `command` providers, reconciliation, `sync`.
   (Written 5 October 2026.)
5. **Hooks**. (Written 5 October 2026.)
6. **TUI**, live sitting view included. (Written 5 October 2026, with
   `actions ls` and `actions done`; the timer lives in the TUI's memory only.)
7. **Import** from Google Tasks. (Written 5 October 2026: replayable, refs
   `gtasks:<id>` and `gmail:message/<id>`, due date kept in the notes.)

## 14. References

- `aclemen1/ai-native-cli` — the CLI conventions.
- RFC 5545 — iCalendar, RRULE.
- Robert's Rules of Order — vocabulary of agenda, motions and minutes.
