# ordo

The agenda of meetings, for humans and agents: items proposed, accepted,
ordered and deferred across sittings; outcomes, decisions and actions; the
minutes. One Go binary, plain Markdown files, one store per sphere.

```sh
go install ./cmd/ordo
ordo init --sphere pro --root ~/ordo/pro
ordo meeting add RDIR --title "Board" --rrule "FREQ=WEEKLY;BYDAY=TH" --start 2026-10-08T09:00 --duration 90m --sphere pro
ordo item add RDIR "Budget 2027" --kind decision --duration 20m --accept --sphere pro
ordo sitting show RDIR --format text --sphere pro
```

Agents: `ordo schema`, then `ordo schema <category> <action>`.
Specification: [docs/SPEC.md](docs/SPEC.md).
