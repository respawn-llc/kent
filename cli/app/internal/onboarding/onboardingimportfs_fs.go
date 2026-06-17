package onboarding

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Provider struct {
	ID                    ProviderID
	Label                 string
	HomeEntry             string
	SkillSourceCandidates []string
	SupportsCommandImport bool
}

type CommandItem struct {
	ID                  string
	Provider            ProviderID
	ProviderLabel       string
	SourceFile          string
	TargetFileName      string
	DisplayName         string
	DuplicateSourceNote string
}

func DiscoverProviderSkills(provider Provider, base string) (string, []Item, error) {
	for _, candidate := range provider.SkillSourceCandidates {
		root := filepath.Join(base, candidate)
		exists, err := PathExists(root)
		if err != nil {
			return "", nil, err
		}
		if !exists {
			continue
		}
		items, err := DiscoverDirectSkills(provider, root)
		if err != nil {
			return "", nil, err
		}
		if len(items) == 0 {
			continue
		}
		return root, items, nil
	}
	return "", nil, nil
}

func DiscoverDirectSkills(provider Provider, root string) ([]Item, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("discover %s direct skills: %w", provider.Label, err)
	}
	items := make([]Item, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(root, entry.Name())
		skillFile := filepath.Join(skillDir, "SKILL.md")
		meta, ok := ParseSkillMetadata(skillFile)
		if !ok {
			continue
		}
		info, err := os.Stat(skillFile)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("inspect %s skill %s: %w", provider.Label, skillFile, err)
		}
		itemID := string(provider.ID) + ":" + filepath.ToSlash(skillDir)
		items = append(items, Item{ID: itemID, Provider: provider.ID, ProviderLabel: provider.Label, SourceDir: skillDir, TargetDirName: entry.Name(), SkillName: meta.Name, ModifiedAt: info.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].TargetDirName < items[j].TargetDirName })
	return items, nil
}

func DiscoverProviderCommands(provider Provider, base string) (string, []CommandItem, error) {
	for _, root := range []string{filepath.Join(base, "commands"), filepath.Join(base, "prompts")} {
		exists, err := PathExists(root)
		if err != nil {
			return "", nil, err
		}
		if !exists {
			continue
		}
		items, err := DiscoverDirectCommands(provider, root)
		if err != nil {
			return "", nil, err
		}
		if len(items) > 0 {
			return root, items, nil
		}
	}
	return "", nil, nil
}

func DiscoverDirectCommands(provider Provider, root string) ([]CommandItem, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("discover %s direct commands: %w", provider.Label, err)
	}
	items := make([]CommandItem, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		path := filepath.Join(root, entry.Name())
		targetFileName := entry.Name()
		displayName := strings.TrimSuffix(targetFileName, filepath.Ext(targetFileName))
		itemID := string(provider.ID) + ":" + filepath.ToSlash(path)
		items = append(items, CommandItem{ID: itemID, Provider: provider.ID, ProviderLabel: provider.Label, SourceFile: path, TargetFileName: targetFileName, DisplayName: displayName})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].TargetFileName < items[j].TargetFileName })
	return items, nil
}

// ErrSkillsDirectoryNotFound marks a provider whose skills source directory
// could not be located. It wraps os.ErrNotExist so callers and tests match it
// with errors.Is rather than comparing rendered message text.
var ErrSkillsDirectoryNotFound = fmt.Errorf("%w: no skills directory found", os.ErrNotExist)

func ProviderSkillSourceAtBase(provider Provider, base string) (string, error) {
	for _, candidate := range provider.SkillSourceCandidates {
		preferred := filepath.Join(base, candidate)
		if ok, err := PathExists(preferred); err == nil && ok {
			return preferred, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("%w for %s", ErrSkillsDirectoryNotFound, provider.Label)
}

func ProviderCommandSourceAtBase(provider Provider, base string) (string, error) {
	root, items, candidateErr := DiscoverProviderCommands(provider, base)
	if candidateErr != nil {
		return "", candidateErr
	}
	if strings.TrimSpace(root) != "" && len(items) > 0 {
		return root, nil
	}
	return "", fmt.Errorf("no slash command directory found for %s", provider.Label)
}

func ShouldSkipTarget(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect import target %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return true, nil
	}
	if !info.IsDir() {
		return true, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, fmt.Errorf("read import target %s: %w", path, err)
	}
	return len(entries) > 0, nil
}

func ShouldSkipCommandImport(globalRoot string) (bool, error) {
	for _, path := range []string{filepath.Join(globalRoot, "commands"), filepath.Join(globalRoot, "prompts")} {
		skip, err := ShouldSkipTarget(path)
		if err != nil {
			return false, err
		}
		if skip {
			return true, nil
		}
	}
	return false, nil
}

func ExecuteSymlink(targetRoot string, sourcePath string, kind string, sourceLabel string) ([]string, error) {
	if err := RequireSourceDirectory(sourcePath, sourceLabel); err != nil {
		return nil, err
	}
	if err := PrepareEmptyDirectorySymlinkTarget(targetRoot, kind); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(targetRoot), 0o755); err != nil {
		return nil, fmt.Errorf("create %s parent root: %w", kind, err)
	}
	if err := os.Symlink(sourcePath, targetRoot); err != nil {
		return nil, fmt.Errorf("symlink %s: %w", sourceLabel, err)
	}
	return []string{targetRoot}, nil
}

func RollbackCreatedPaths(paths []string) error {
	var rollbackErr error
	for index := len(paths) - 1; index >= 0; index-- {
		path := strings.TrimSpace(paths[index])
		if path == "" {
			continue
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("rollback import path %s: %w", path, err))
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("rollback import path %s: %w", path, err))
			}
			continue
		}
		if err := os.RemoveAll(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("rollback import path %s: %w", path, err))
		}
	}
	return rollbackErr
}

func PrepareEmptyDirectorySymlinkTarget(path string, kind string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s symlink target %s: %w", kind, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s symlink target already exists: %s", kind, path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("read %s symlink target %s: %w", kind, path, err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s symlink target already exists: %s", kind, path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove empty %s symlink target %s: %w", kind, path, err)
	}
	return nil
}

// ErrSourceDirectoryInvalid marks an import source path that could not be
// validated as a usable directory (missing, unreadable, or not a directory).
// Callers and tests match this with errors.Is rather than comparing rendered
// message text.
var ErrSourceDirectoryInvalid = errors.New("import source directory is invalid")

func RequireSourceDirectory(path string, label string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w: %w", label, ErrSourceDirectoryInvalid, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory: %s: %w", label, path, ErrSourceDirectoryInvalid)
	}
	return nil
}

func PathExists(path string) (bool, error) {
	if _, err := os.Lstat(path); err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
}
