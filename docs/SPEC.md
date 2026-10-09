# oj — specification

Draft 0.1, 5 October 2026. Nothing is implemented yet.

`oj` keeps the agenda of meetings. A **meeting** is a recurring or one-off
body (a board, a steering committee, a weekly team review). Each date it meets
is a **sitting**. People and agents propose **items**; the chair accepts,
orders and times them; the sitting deals with them; what comes out of each
item (a summary, a decision, actions) becomes the **minutes**. An item that is
not dealt with moves to the next sitting on its own.

`oj` is the record of items and their state. It does not hold the
discussion around an item, the files, or the memory of decisions: other tools
do (a dossier manager, an artefact store, a memory). It reaches them through
hooks and identifiers, never by reading their data.

## 1. Principles

1. **One record of items.** An item has one home: its meeting in `oj`. Other
   tools point at it; `oj` points back at where the item came from.
2. **Items survive sittings.** An item keeps its id when it is deferred. Its
   history across sittings stays in one place.
3. **Plain files.** A store is a directory of Markdown files with YAML
   frontmatter. It is readable, diffable and versionable without `oj`.
4. **Spheres stay apart.** Each sphere has its own root. A read may show
   several spheres side by side; a write, a hook or a reference stays in one
   sphere.
5. **Agnostic.** `oj` knows no particular calendar, dossier manager, memory
   or artefact store. It reads dates from a calendar provider and announces
   changes through hooks.
6. **Nothing leaves without the user.** `oj` sends no e-mail and no
   invitation. It renders documents; a human or a hook decides where they go.
7. **AI-native CLI.** `oj` follows `aclemen1/ai-native-cli`: single Go
   binary, embedded skill, `oj schema`, JSON envelope `{ok, result|error}`,
   `--format json|text`. The same actions are served over MCP.

## 2. Overview

```
 human · agent · tool (office, routine, …)
        │ CLI · MCP
        ▼
 ┌──────────────────── oj (Go binary) ─────────────────────┐
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

### 3.4b Recurring items

Some items come back at every sitting of a meeting (travel, a shared meal,
the date of the next sitting). They are declared once, in `meeting.md`
(`standing:`: key, title, place `start` or `end`, and optionally kind,
duration, question, notes), with `oj standing add|ls|rm`.

- Every planned sitting shows one instance of each, accepted, at the start
  or the end of its agenda. Until someone acts on it, the instance is
  virtual: shown (`virtual: true`, `standing: <key>`) but not written, so
  sittings far ahead write nothing.
- The instance is written, as an accepted item with the ref
  `standing:<key>`, at the first change made to it (in the TUI, any gesture
  on it), by `oj standing apply <sitting> [--key]`, or at freeze or hold.
  It then has its own id and its own outcome.
- An instance written and then dropped stays dropped for its sitting.
- At the minutes, an instance with no outcome `next=done` is dropped
  (`not reached`), not deferred: the next sitting has its own.
- `oj standing rm` stops new instances; those already written stay.
- `oj standing from <item> [--place]` makes an item recurring: the
  declaration takes its title, kind, duration, question and notes, and the
  item becomes the instance of its sitting. `oj standing place` changes
  where a recurring item goes.
- TUI: `↻ <key>` marks an instance not written yet; `*` makes the selected
  item recurring; `R` lists the meeting's recurring items (`n` new, `s`
  start or end, `x` stop).
- A held sitting also shows the recurring items it misses (declared after
  its hold), and `oj standing apply` writes them there; the draft minutes
  leave out those never written.

In the TUI, the meetings list flags a held sitting whose minutes are not
approved ("minutes to do"), and `enter` opens it before the next one. `P`
shows the sitting's document as it would be produced now: the agenda, or
the minutes once held.

### 3.5 Identifiers and links

| Object | Id | Example |
|---|---|---|
| meeting | alias | `RDIR` |
| sitting | alias + date | `RDIR-2026-10-08` |
| item | alias + counter | `RDIR-17` |
| outcome | item + sitting | `RDIR-17@RDIR-2026-10-08` |

Other tools cite `oj://<sphere>/<id>`. Ids never change and are never
reused, even after a drop.

## 4. Storage

### 4.1 Store

One root per sphere, declared in the configuration:

```
~/oj/
  pro/
    .oj/            index.sqlite (rebuildable), counters, lock
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
  `oj` commits after each write action, message `<action> <id>`
  (`item defer RDIR-17`), so the history of a meeting is read with the VCS.
  `oj init` creates the repository when absent.

### 4.2 Sittings from the RRULE

`oj` creates the files of the next sittings lazily: when an action needs
"the next sitting" or when `sitting ls` looks ahead (default: next 3). Past
sittings never appear on their own: a sitting that was not created is a
sitting that did not happen.

## 5. Public interface

### 5.1 Actions

| Action | Effect |
|---|---|
| `meeting add <alias> --title … [--rrule …] [--duration] [--chair] [--member]… [--calendar …] [--ref]…` | creates a meeting |
| `meeting ls`, `meeting show <alias>`, `meeting edit <alias> --set <field>=<value>` | reads and edits |
| `sitting ls [<alias>] [--state] [--ahead N] [--since] [--with-items]` | lists sittings; with `--with-items`, each meeting with its sittings' agendas and its items with no sitting |
| `sitting show <sitting\|alias>` | the agenda: ordered items, slots, total time against `duration`, proposed items apart; an alias means its next sitting |
| `sitting add <alias> --date … [--time] [--place]`, `sitting move <sitting> --date …`, `sitting cancel <sitting> [--reason]` | one-off changes |
| `sitting freeze <sitting> [--leave-proposed]`, `sitting reopen <sitting>` | closes or reopens the agenda; freeze renders the agenda |
| `sitting hold <sitting> [--present …] [--excused …]` | the sitting took place |
| `sitting minute <sitting>` | approves the minutes, defers what is left, renders the minutes |
| `sitting unhold <sitting>`, `sitting unminute <sitting>`, `sitting restore <sitting>` | take back a hold (frozen again, or planned), approved minutes (held again, items back as before, agent outcomes drafts again, rendered minutes removed; refused if an item changed since), a cancellation (planned again) |
| `item create-ref <id> [--scheme office]` | creates a target for the item with the sphere's `refs.<scheme>.create` command (e.g. a dossier), adds its ref, then runs `refs.<scheme>.link` |
| `item restore <id> [--accept]`, `item undefer <id>` | take back a drop (proposed, or accepted) or a done (on the agenda again); take back a deferral (back to the sitting it came from when still open, accepted) |
| `item add <alias> <title> [--owner] [--kind] [--duration] [--expected] [--attach]… [--ref]… [--sitting] [--accept]` | proposes an item (accepted at once with `--accept`); returns its id |
| `item ls [<alias>] [--state] [--owner] [--ref <ref>] [--search <text>]` | lists items; `--ref office:U-0042` finds the item of a dossier |
| `item show <id>`, `item edit <id> --set <field>=<value>` | reads and edits |
| `item accept <id>…`, `item defer <id> [--to <sitting>]`, `item drop <id> --reason …` | moves items in their cycle |
| `item move <id> <sitting>` | puts the item on another planned sitting of its meeting, earlier or later, keeping its state (a deferred item becomes accepted); in the TUI, `M` picks the sitting |
| `item order <sitting> <id>…` | sets the order; ids not named keep their relative order after |
| `outcome set <id> [--sitting] [--summary] [--decision] [--action "what\|who\|due"]… [--next done\|deferred] [--by]` | records what came out of an item; `--by agent:<name>` writes a draft |
| `render <sitting> --doc agenda\|minutes [--to md\|html\|docx\|pdf] [--out]` | renders a document (§8) |
| `actions ls [<alias>] [--who] [--state open\|done\|all] [--due-before]` | actions across sittings of a meeting or the sphere, open first by due date |
| `actions done <item> <n> [--sitting] [--undo]` | marks action n of an outcome done, or open again; rewriting the outcome keeps it. An action can also be written done: fourth field of `what\|who\|due\|done` (`done`, `fait`, `x`, `yes`, `oui`, `✓`) |

Every action takes `--sphere` and returns the envelope. A read covers every
sphere unless `--sphere` narrows it; a write needs a sphere (§9).
Errors name the canonical invocation, echo the offending values and show an
example.

### 5.2 Operation and meta commands

| Command | Role |
|---|---|
| `oj init --sphere <name> --root <dir>` | declares a sphere |
| `oj import gtasks …` | one-off import (§10) |
| `oj reindex`, `oj doctor` | upkeep; doctor checks calendars, hooks, renderer |
| `oj mcp [--spheres pro]` | MCP over stdio, limited to the spheres given |
| `oj tui` | terminal interface (§11) |
| `oj schema`, `oj skill show\|install`, `oj version` | AI-native CLI |

### 5.3 Surfaces

The actions are defined once (action specs) and served as CLI commands and as
MCP tools (one tool per action, named `<category>_<action>`: `item_add`).
Sittings and items are also MCP resources `oj://<sphere>/<id>`.

- `oj mcp --spheres <list>` serves only those spheres. `sphere` is never a
  required argument: a read covers every served sphere, a write takes
  `sphere`, a qualified id or the server's `OJ_SPHERE`; with one sphere,
  `sphere` defaults to it.
- Everything done through MCP is logged as `agent:…`, and an outcome set
  through MCP is always a draft: a `by` without the `agent:` prefix gets it.

## 6. Dates and calendars

The RRULE of a meeting says when it meets. The calendar says when it really
meets: a sitting moved in the calendar is moved in `oj`.

- A meeting's `calendar` names a provider and a match rule (event title
  pattern, or the event's recurring id).
- Sources are named under `calendars:` in the configuration: `ics` (`path`
  or `url`; recurring events expanded, EXDATE and RECURRENCE-ID applied) and
  `command` (`run: [program, args…]`, called with `OJ_FROM` and `OJ_TO`
  in RFC 3339, printing `[{uid, start, end, title, location, status,
  recurring_id, original_start}]`). The command lets a user plug a CLI such
  as `gws` without `oj` knowing it.
- `oj meeting calendar <alias> --source <name> --match <regex>
  [--recurring-id <id>]` links a meeting to its events.
- `oj sitting sync [<alias>] [--days 90]` reads the events and reconciles.
  It is explicit, not run at every read: a routine runs it on a schedule.
  An event finds its sitting by the uid recorded on it, then by its
  `original_start`, then by its start date.

| Calendar | `oj` |
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

`oj` announces events to commands named in the sphere's configuration. A
hook receives the event as JSON on standard input — `event`, `sphere`,
`meeting`, `at`, `by` and the payload — with `OJ_SPHERE`, `OJ_EVENT`,
`OJ_MEETING` and `OJ_HOOK_DEPTH` in its environment, in the store's
directory, with a timeout (30 s by default).

- Hooks run after the commit, once the lock is released, so a hook may call
  `oj` again. A hook started from a hook has a greater depth; at depth 3
  hooks are skipped.
- A failing hook never undoes the action: the action succeeds and the
  failure comes back in `warnings` of the envelope (stderr in text).

| Event | Payload | Typical use |
|---|---|---|
| `item.added`, `item.accepted`, `item.dropped` | item (and reason) | tell the dossier the item came from |
| `item.deferred`, `item.moved` | item, from, to | tell the dossier its item moved |
| `outcome.set` | item, sitting, outcome | tell the dossier what was decided |
| `action.added` | item, sitting, n, action — only for an action new in the outcome | record the action in a task tool |
| `action.done`, `action.reopened` | item, sitting, n, action — only when the state changes | mark the task done or open in a task tool |
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
        run: ["office-notify-from-oj"]
        timeout: 20s
```

## 8. Rendering

- `render <sitting> --doc agenda|minutes --to md|html|docx|pdf [--out]`
  produces a document from a sitting and its items, with a Go template per
  document. Built-in templates exist in English and French (`lang` of the
  meeting, then of the sphere, then `en`); a sphere can name its own
  templates (`render.templates.agenda`, `.minutes`).
- `md` and `html` are rendered by `oj` itself. `docx` and `pdf` go through
  pandoc (`render.reference_doc` of the sphere for docx, `pdf_engine` for
  PDF, default xelatex).
- `freeze` writes `rendered/<date>-agenda-vN.md` (`v2` after a reopen) and
  `minute` writes `rendered/<date>-minutes.md`, in the same commit; they are
  never overwritten, and their paths go to the hooks. The sphere's
  `render.formats` (e.g. `[docx]`) are rendered next to them in the same
  commit; a format that fails is a warning, the Markdown stands.
- A sphere can name its own converter for a format (`render.converters.docx:
  [command, …, "{in}", "{out}"]`), used instead of pandoc, e.g. a script that
  fills an institution's Word template.
- `render` of a frozen agenda or approved minutes starts from that final
  Markdown, and a docx or PDF made from it is committed next to it.
  Anything else is a draft, marked as such, written to `--out` or to a
  temporary directory outside the store.

## 9. Spheres and access

- A sphere is a root and a name; there is no default sphere in the
  configuration.
- A read (`ls`, `show`) covers every configured sphere, or the spheres served
  by `oj mcp`, unless `--sphere` or a qualified id narrows it. When the
  output holds several spheres, each object shows its sphere, and the text
  output prefixes it: `pro:RDIR-3`.
- An id found in several spheres is an error that names the qualified forms
  (`pro:RDIR`, `perso:RDIR`).
- A write needs a sphere, taken in this order: a qualified id
  (`pro:RDIR-3`), `--sphere`, `OJ_SPHERE`. A qualified id that names another
  sphere than `--sphere` is an error. `OJ_SPHERE` applies to writes only.
- `oj mcp --spheres <list>` serves only those spheres; a call naming
  another sphere, by `sphere` or by a qualified id, fails.
- Hooks run with the sphere of the event; a hook configured for one sphere is
  never run for another.
- `oj://` references and `refs` are kept as text; `oj` never follows a
  reference into another sphere.

## 10. Import from Google Tasks

A one-off import, for users who kept one task list per meeting.

```
oj import gtasks --sphere pro --meeting RDIR --from tasks.json [--dry-run]
```

- Input: the JSON a task CLI prints for one list (`title`, `notes`, `due`,
  `status`, `links`). `oj` does not call Google itself.
- Each open task becomes a `proposed` item; title and notes are kept; a link
  to an e-mail becomes a `ref`; completed tasks are skipped unless
  `--with-completed`.
- `--dry-run` prints the items without writing them.

## 11. TUI

`oj tui`, Bubble Tea v2, keys of the ecosystem's common convention.
`esc` goes back to the previous view and never quits; `q` quits from any
view, except in a text field, where it is typed.

| Keys | Everywhere |
|---|---|
| `j k`, `gg G` | next / previous line; top, end |
| `enter l`, `esc h` | open; back (esc first clears the filter) |
| `/` | filter the list (meetings, agenda, actions, sittings) |
| `tab`, `J K` | show or hide the item pane; scroll it |
| `1 2 3` | meetings, actions, every sitting |
| `r`, `?`, `q` | reload; help; quit |

| Keys | Agenda | Live | Actions |
|---|---|---|---|
| `c` | new item (`n`) | new action | |
| `E`, `N` | edit the item; add to its notes | summary | |
| `z` | defer (`d`) | not reached (`-`) | |
| `x` | drop | | |
| `o`, `O` | go to the item's ref; create one | go to the ref | go to the ref |
| `space`, `e` | | timer | done / open; done |
| `H`, `L`, `F` | hold; live; reopen the agenda | hold | |
| `R` | ask the meeting's agent for outcomes | | |
| `g r` | recurring items (`c` new, `p` start/end, `x` stop) | | |
| `ctrl+j ctrl+k` | move the item down / up | | |
| `f`, `m`, `P`, `M`, `*`, `+ -`, `u U`, `[ ]`, `a` | freeze, minutes, preview, move to, make recurring, ±5 min, take back, sittings, accept | | `f`: show done |

Keys in parentheses are kept from before the convention. In the live
view, `space` stays the timer: there is nothing to mark done there.

The TUI follows changes made by other processes (office, agents, the
calendar sync): every 2 seconds it compares a fingerprint of the stores'
files (count, size, latest change) and reloads the view when it changed,
keeping the selection. A change waits while the user types or picks a
sitting.

The TUI also follows its own binary (mtime and inode, at the same pace).
After a rebuild, an idle TUI saves its state and replaces itself with the
new binary (exec), which restores the view, the selection and the filter
and shows « rechargé v… ». While the user types, picks a sitting or edits
an item, a « nouvelle version » badge waits in the header and the reload
happens once idle. SIGUSR1 asks for the same reload, under the same rules.
The header shows the version and the build time of the running binary.

A help panel, open by default (`?` hides it), says where the user is and
what can be done next: in the agenda, the sitting on its cycle (planned →
frozen → held → minuted) with what that state means and the next step, the
actions open on the sitting and on the selected item for their states, and
how to navigate. It sits at the right on a wide terminal, below otherwise,
in French for a sphere rendered in French, in English otherwise.

The TUI shows every sphere (or the one given by `--sphere`). With several,
the meetings list, the sittings view of every meeting and the actions view
prefix each line with its sphere, and `s` cycles the filter: all spheres,
then each sphere in turn. Opening a meeting makes its sphere the one changes
go to; the header names it.

The agenda also lists, under "Deferred", the items deferred from this
sitting to a later one, so that `u` (undefer) can bring them back. `S`
opens the sittings view: recent and upcoming sittings, each with its items,
then items with no sitting; from the meetings list for every meeting, from
an agenda for its meeting. `M` moves the selected item to a planned sitting
picked from a list; `o` jumps to its ref with the sphere's `open` command
(for an office dossier: focus its session).

The TUI keeps its state in `$XDG_STATE_HOME/oj/tui.json`, or
`tui-<sphere>.json` when it shows one sphere (`~/.local/state/oj/…`): sphere
filter, sphere of the open meeting, view, sitting, selected item, item opened, help
shown or hidden, actions filter, sittings view scope, and in a live sitting
the current item and the timers (a running timer keeps counting). It comes
back as it was at the next start.

| View | Shows | Actions |
|---|---|---|
| Meetings | alias, next sitting, items on the agenda and proposed | open (`enter`), actions (`A`), sphere filter (`s`) |
| Agenda | a sitting: ordered items, slots, outcome marks, total against duration, proposed items apart; under the list, a pane with the selected item (owner, deferrals, question, attachments, refs, notes, outcome, what the ref commands say about the refs' targets, and the sections of the cited block) | see the key tables above |
| Item | fields, notes, history with outcomes and actions, the full text of each ref's target, the cited sections, log | `E` edit in `$EDITOR` (then committed), `N` add a note |
| Sitting (live) | the current item, a timer per item, elapsed against plan, the same pane for the current item | `space` timer, `n`/`p` item, `E` summary, `D` decision, `c` action, `z` defer, `H` hold |
| Actions | actions by due date, overdue in red | `space` done or open, `e` done, `enter` item, `o` ref, `f` show done, `s` sphere filter |

Every input but the filter opens in a modal (tuikit): a text area, a full
editor, the action form (what, who with completion, due date, done), a
choice or a yes/no. The modal takes every key; `ctrl+s` saves, `esc` cancels.

## 12. Configuration

`~/.config/oj/config.yaml`:

```yaml
spheres:
  perso: { root: ~/oj/perso, vcs: jj }
  pro:
    root: ~/oj/pro
    vcs: jj
    render:
      lang: fr
      reference_doc: ~/templates/house-style.docx
    hooks:
      - on: [outcome.set, item.deferred]
        run: ["office-notify-from-oj"]
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
notes are rendered as Markdown too), and `oj item show --with-refs`
returns it to agents. Refs of a scheme with no command stay plain text.

A scheme can also say how to create a target for an item (`create`, which
prints the new id, plain or as JSON `result.id`) and how to attach it
(`link`, with `{id}` and `{meeting_ref}`, the id of the meeting's own ref of
that scheme). `item add` with a ref that an open item of the meeting already
carries returns that item: a tool that registers the new target as an item
finds the existing one. Finally `open` (with `{id}`) jumps to the target,
e.g. `office attach {id}` focuses the dossier's session.

A scheme's `ask` command (with `{id}` and `{text}`) sends a request to the
target, and the sphere's `asks` names request texts, with `{sitting}`,
`{meeting}`, `{meeting_title}`, `{date}`, `{time}`, `{sphere}` and `{items}`
(the agenda, recurring items not written yet included). `oj sitting ask
<sitting> [--request outcomes]`, or `G` in the TUI, sends the request
`outcomes` to the first meeting ref whose scheme has an `ask` command: for
instance, asking the meeting's agent to write the outcomes from a meeting
transcript. oj writes nothing itself; outcomes written by an agent are drafts.

```yaml
    refs:
      office:
        ask: ["office", "prompt", "{id}", "--text", "{text}"]
    asks:
      outcomes: |
        Write the outcomes of {sitting} from its transcript, as drafts.
        {items}
```

`refs` show what an item cites; `cited` shows what cites the item. Each
entry of the sphere's `cited` block is a titled command run with `{ref}`
(`oj:RDIR-17`) and `{sphere}`, and `OJ_SPHERE` in its environment; it prints
Markdown about the object, or nothing. The TUI shows each non-empty section
in the item pane and the item view, loaded in the background and refreshed
after a minute; a failing command shows its error in one muted line.
`oj item show --cited` returns the sections to agents. oj names no other
tool: notes, tasks or dates come from whatever commands the configuration
lists.

Notes of an item stay in its body unless the sphere declares `notes.add`, a
command with `{ref}`, `{sphere}` and `{text}` that also gets the text on its
standard input. Then `N` in the TUI, `item add --notes` and `item edit
--notes` add a note through it, and the item's file is left as it is; the
notes come back through a `cited` section. `item edit --clear-notes` empties
the body, e.g. after the notes moved elsewhere. Rendered agendas and minutes
never use the notes.

```yaml
    notes:
      add: ["note", "add", "-", "--ref", "{ref}", "--sphere", "{sphere}"]
    cited:
      - title: Tasks
        run: ["task", "ls", "--ref", "{ref}", "--format", "text"]
```

Every file keeps the fields this version does not know (`Extra`, inline):
an older binary that rewrites a meeting, a sitting or an item no longer
drops what a newer one wrote.

A meeting's own settings live in its `meeting.md`.

## 13. Phases

1. **Core**: store, meetings, sittings from RRULE, items and their cycle,
   outcomes, `schema`, `skill`, CLI. Tests on a throwaway store.
   (Written 5 October 2026; `actions ls` comes with the TUI.)
2. **Render**: Markdown and HTML agenda and minutes, templates, pandoc.
   (Written 5 October 2026: built-in templates in English and French,
   `lang` per meeting or sphere.)
3. **MCP**: `oj mcp`, resources. (Written 5 October 2026.)
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
