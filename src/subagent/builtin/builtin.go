// Package builtin holds MetaAtoms built-in SubAgent role definitions.
package builtin

import "embed"

const EmbeddedRoot = "embedded://subagent/builtin"

//go:embed *.md
var embeddedFS embed.FS

type EmbeddedAgent struct {
	Name    string
	Path    string
	Content string
}

func Embedded() ([]EmbeddedAgent, error) {
	entries, err := embeddedFS.ReadDir(".")
	if err != nil {
		return nil, err
	}
	out := make([]EmbeddedAgent, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || len(entry.Name()) < 4 || entry.Name()[len(entry.Name())-3:] != ".md" {
			continue
		}
		data, err := embeddedFS.ReadFile(entry.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, EmbeddedAgent{Name: entry.Name()[:len(entry.Name())-3], Path: entry.Name(), Content: string(data)})
	}
	return out, nil
}
