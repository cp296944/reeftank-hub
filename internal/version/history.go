package version

import (
	_ "embed"
	"strings"
)

//go:embed CHANGELOG.md
var changelog string

// LocalHistory is bundled with the binary, independently of GitHub access.
func LocalHistory() []map[string]any {
	out := []map[string]any{}
	for _, part := range strings.Split(changelog, "\n## ")[1:] {
		header, body, ok := strings.Cut(part, "\n")
		if !ok {
			continue
		}
		fields := strings.Split(header, " | ")
		if len(fields) < 2 {
			continue
		}
		tag, date := strings.TrimSpace(fields[0]), strings.TrimSpace(fields[1])
		local := len(fields) < 3 || strings.TrimSpace(fields[2]) != "github"
		out = append(out, map[string]any{
			"tag": tag, "name": tag, "notes": strings.TrimSpace(body), "date": date,
			"local": local, "prerelease": local, "url": "", "embedded": true,
		})
	}
	return out
}
