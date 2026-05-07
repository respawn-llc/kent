package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

const (
	builderDirName   = ".builder"
	generatedDirName = ".generated"
	promptsDirName   = "prompts"
	commandsDirName  = "commands"
)

type filePromptCommand struct {
	Name        string
	Description string
	Content     string
}

func NewDefaultRegistryWithFilePrompts(workspaceRoot, globalRoot string) (*Registry, error) {
	r := NewDefaultRegistry()
	prompts, err := loadFilePromptCommands(workspaceRoot, globalRoot)
	if err != nil {
		return nil, err
	}
	specs := make([]promptCommandSpec, 0, len(prompts))
	for _, prompt := range prompts {
		specs = append(specs, promptCommandSpec{
			Name:        prompt.Name,
			Description: prompt.Description,
			Prompt:      prompt.Content,
		})
	}
	registerPromptCommands(r, specs)
	return r, nil
}

func loadFilePromptCommands(workspaceRoot, globalRoot string) ([]filePromptCommand, error) {
	dirs, err := filePromptSearchDirs(workspaceRoot, globalRoot)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	commands := make([]filePromptCommand, 0)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read prompt directory %s: %w", dir, err)
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if filepath.Ext(name) != ".md" {
				continue
			}
			base := strings.TrimSuffix(name, ".md")
			commandID := normalizeFilePromptCommandID(base)
			if commandID == "" {
				continue
			}
			commandName := "prompt:" + commandID
			if seen[commandName] {
				continue
			}
			fullPath := filepath.Join(dir, name)
			content, err := os.ReadFile(fullPath)
			if err != nil {
				return nil, fmt.Errorf("read prompt file %s: %w", fullPath, err)
			}
			if strings.TrimSpace(string(content)) == "" {
				continue
			}
			seen[commandName] = true
			commands = append(commands, filePromptCommand{
				Name:        commandName,
				Description: "Run custom Markdown prompt",
				Content:     string(content),
			})
		}
	}
	return commands, nil
}

func normalizeFilePromptCommandID(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(trimmed))
	lastUnderscore := false
	for _, r := range trimmed {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			builder.WriteRune(unicode.ToLower(r))
			lastUnderscore = false
		case unicode.IsSpace(r) || r == '_':
			if builder.Len() == 0 || lastUnderscore {
				continue
			}
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(builder.String(), "_")
}

func filePromptSearchDirs(workspaceRoot, globalRoot string) ([]string, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return nil, errors.New("workspace root is required")
	}
	globalRoot = strings.TrimSpace(globalRoot)
	if globalRoot == "" || globalRoot == "." {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		globalRoot = filepath.Join(home, builderDirName)
	} else {
		resolved, err := filepath.Abs(globalRoot)
		if err != nil {
			return nil, fmt.Errorf("resolve global prompt root: %w", err)
		}
		globalRoot = resolved
	}

	localBuilder := filepath.Join(workspaceRoot, builderDirName)
	return []string{
		filepath.Join(localBuilder, promptsDirName),
		filepath.Join(localBuilder, commandsDirName),
		filepath.Join(globalRoot, promptsDirName),
		filepath.Join(globalRoot, commandsDirName),
		filepath.Join(globalRoot, generatedDirName, promptsDirName),
		filepath.Join(globalRoot, generatedDirName, commandsDirName),
	}, nil
}
