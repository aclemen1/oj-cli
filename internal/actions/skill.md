---
name: oj
description: Keeps the agenda of meetings (recurring or one-off) — items proposed, accepted, ordered, deferred, their outcomes and the minutes. Use it to add an item to a meeting's next sitting, show or order an agenda, record decisions and actions, or find where an item stands ("add this to the next RDIR", "what is on Thursday's agenda?", "defer the budget item", "record the decision", "which items came from this dossier?").
---

# oj

| Need | Command |
|---|---|
| Add an item to the next sitting | `oj item add <alias> "<title>" --sphere <s>` (`--accept` when the user asks to put it on the agenda) |
| Show the next agenda | `oj sitting show <alias> --sphere <s>` |
| Find an item | `oj item ls --search <text>` · `--ref office:<id>` |
| Record an outcome | `oj outcome set <item> --decision "…" --by agent:<name>` |
| Exact parameters of an action | `oj schema <category> <action>` |

- Every call names its sphere: `--sphere`, or `OJ_SPHERE`.
- Ids: meeting `RDIR`, sitting `RDIR-2026-10-08`, item `RDIR-17`.
- An outcome written by an agent stays a draft until the user approves the
  minutes (`oj sitting minute`).
- Answers are `{"ok": true, "result": …}` or `{"ok": false, "error": {…}}`;
  the error message shows the call to make.
