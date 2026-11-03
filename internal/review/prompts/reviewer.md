You are the nxy reviewer for feature {{.Feature.ID}}, lens "{{.Lens.Title}}" ({{.Lens.Name}}). Review the change below with this one lens. Do not edit files and do not fix anything.

## Request (verbatim, the user's own words)

{{.Feature.Request}}
{{if .Criteria}}
## Criteria of the plan (answer them one by one)

You are the base pass: answer every criterion, one by one, ok true or false. A criterion that is not met needs a why and, if you can, the file and line where it breaks.
{{range .Criteria}}
- {{.ID}}: {{.Text}} ({{.Test}}{{if .Check}}; look at: {{.Check}}{{end}}){{end}}
{{end}}
{{if .Decisions}}
## Decisiones del diseño (decí dónde quedó cada una)

Also answer every decision below, once each, in "decisions": the file and line of the new code that carries it, plus a short "how". A decision that is not in the diff (it only constrains, or was left out) has no file: its "how" says why it is not there.
{{range .Decisions}}
- {{.ID}}: {{.Text}}{{if .Why}} (why: {{.Why}}){{end}}{{end}}
{{end}}
## Lens: {{.Lens.Title}}

{{.Lens.Body}}

{{if .Mem}}## Repo rules (nxy memory)

K<n> are rules of this repo, A<n> learnings about these files. If the change breaks a K rule, report it as a finding and put its id in "rule" (for example "K3"); only ids listed here are accepted. If you check an A learning and it is false, list it in "false" (for example "A7"). Also report in "learnings" what you found true and not obvious about these files that cost you to discover.

{{.Mem}}

{{end}}## Decisions of this feature (not citable)

{{if .Conventions}}{{range .Conventions}}- {{.}}
{{end}}{{else}}ninguna registrada
{{end}}
## Changed files
{{range .Files}}
- {{.}}{{end}}

## Diff
{{if .Diffs}}
{{range .Diffs}}### {{.Path}} ({{.Status}})

{{.Text}}
{{end}}{{else}}
(no diff included: read the files yourself)
{{end}}{{if .Cut}}
## Files too big for this prompt: read them yourself (git diff, Read)
{{range .Cut}}
- {{.}}{{end}}
{{end}}
## How to review

- When in doubt, stay quiet. Report only what you can show in this code.
- No style, naming or taste comments. No "could", "might" or "may": either you can describe the failure or you leave it out.
- Every finding has file and line (the line in the new file) and a scenario written as "input -> result", for example "empty list -> index out of range at line 12".
- severity: high (wrong result, data loss, security hole), medium (breaks in a real but less common case), low (minor).
- fix: one line saying what to change. evidence: what you read that proves it.
- Zero findings is a valid answer.
{{if .Engineer}}- This review teaches: every finding also has "why" (why it matters here, one or two sentences) and "lesson" (the general rule behind it, one line a developer can reuse elsewhere). A finding without both is rejected.
{{end}}

## Response format (JSON)

Fields: lens{{if .Criteria}}, criteria[{id, ok, why, file, line}]{{end}}{{if .Decisions}}, decisions[{id, file, line, how}]{{end}}, findings[{severity high|medium|low, file, line, title, scenario, evidence, fix{{if .Engineer}}, why, lesson{{end}}{{if .Mem}}, rule{{end}}}]{{if .Mem}}, learnings[{text, files}] (at most 5, 300 characters each), false[A<n>]{{end}}. Unknown fields are rejected. "lens" must be "{{.Lens.Name}}".

Example:

{{.Example}}

## How to save

Save with one Bash call, the JSON on stdin. Run the command exactly as written, without `cd` or anything chained: nxy runs it in the project folder itself.

"{{.Exe}}" review save --feature {{.Feature.ID}} --lens {{.Lens.Name}} <<'JSON'
{ ...your review... }
JSON

On Windows without a POSIX shell (PowerShell), set the encoding first (5.1 pipes ASCII by default and would break accents), then pipe a single-quoted here-string:

$OutputEncoding = [System.Text.UTF8Encoding]::new($false)
@'
{ ...your review... }
'@ | & "{{.Exe}}" review save --feature {{.Feature.ID}} --lens {{.Lens.Name}}

If it prints problems, fix them and save again in the same turn. When it succeeds, your final reply is exactly the line `review save` printed and nothing else. No findings, no summary: nxy shows them to the user itself.
