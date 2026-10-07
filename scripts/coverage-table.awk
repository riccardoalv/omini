# Turns `go test -cover ./...` output into Markdown table rows.
/coverage:/ {
  pkg = ($1 == "ok") ? $2 : $1
  for (i = 1; i <= NF; i++) if ($i == "coverage:") cov = $(i + 1)
  if (cov == "[no") cov = "no tests"
  printf "| `%s` | %s |\n", pkg, cov
}
/\[no test files\]/ { printf "| `%s` | no tests |\n", $2 }
