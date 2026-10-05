# oj

The agenda of meetings, for humans and agents: items proposed, accepted,
ordered and deferred across sittings; outcomes, decisions and actions; the
minutes. One Go binary, plain Markdown files, one store per sphere.

```sh
go install ./cmd/oj
oj init --sphere pro --root ~/oj/pro
oj meeting add RDIR --title "Board" --rrule "FREQ=WEEKLY;BYDAY=TH" --start 2026-10-08T09:00 --duration 90m --sphere pro
oj item add RDIR "Budget 2027" --kind decision --duration 20m --accept --sphere pro
oj sitting show RDIR --format text --sphere pro
```

Agents: `oj schema`, then `oj schema <category> <action>`.
Specification: [docs/SPEC.md](docs/SPEC.md).
