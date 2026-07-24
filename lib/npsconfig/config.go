package npsconfig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	beeconfig "github.com/astaxie/beego/config"
)

const MaxContentBytes = 1024 * 1024

var (
	assignmentPattern = regexp.MustCompile(`^\s*([A-Za-z0-9_.-]+)\s*=`)
	sectionPattern    = regexp.MustCompile(`^\s*\[([^]]+)]\s*$`)
)

type Document struct {
	Content string
	Values  map[string]string
}

func Parse(content string) (*Document, error) {
	if len(content) > MaxContentBytes {
		return nil, fmt.Errorf("configuration exceeds %d bytes", MaxContentBytes)
	}
	if strings.IndexByte(content, 0) >= 0 {
		return nil, fmt.Errorf("configuration contains a NUL byte")
	}
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	parsed, err := beeconfig.NewConfigData("ini", []byte(content))
	if err != nil {
		return nil, fmt.Errorf("parse nps.conf: %w", err)
	}

	keysBySection := make(map[string]map[string]struct{})
	section := "default"
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if match := sectionPattern.FindStringSubmatch(line); len(match) == 2 {
			section = strings.ToLower(strings.TrimSpace(match[1]))
			continue
		}
		match := assignmentPattern.FindStringSubmatch(line)
		if len(match) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(match[1]))
		if keysBySection[section] == nil {
			keysBySection[section] = make(map[string]struct{})
		}
		keysBySection[section][key] = struct{}{}
	}

	values := make(map[string]string)
	for sectionName, keys := range keysBySection {
		for key := range keys {
			fullKey := key
			lookupKey := key
			if sectionName != "default" {
				fullKey = sectionName + "::" + key
				lookupKey = fullKey
			}
			values[fullKey] = parsed.String(lookupKey)
		}
	}
	return &Document{Content: content, Values: values}, nil
}

func Load(path string) (*Document, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(string(content))
}

func ChangedKeys(before, after *Document) []string {
	all := make(map[string]struct{}, len(before.Values)+len(after.Values))
	for key := range before.Values {
		all[key] = struct{}{}
	}
	for key := range after.Values {
		all[key] = struct{}{}
	}
	changed := make([]string, 0, len(all))
	for key := range all {
		beforeValue, beforeExists := before.Values[key]
		afterValue, afterExists := after.Values[key]
		if beforeExists != afterExists || beforeValue != afterValue {
			changed = append(changed, key)
		}
	}
	sort.Strings(changed)
	return changed
}

func Save(path string, content string) error {
	document, err := Parse(content)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := writeReplacement(path+".bak", original, mode); err != nil {
		return fmt.Errorf("write configuration backup: %w", err)
	}
	if err := writeReplacement(path, []byte(document.Content), mode); err != nil {
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			_ = writeReplacement(path, original, mode)
		}
		return fmt.Errorf("replace configuration: %w", err)
	}
	return nil
}

func writeReplacement(path string, content []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".nps-config-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(mode); err == nil {
		_, err = bytes.NewReader(content).WriteTo(temporary)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(temporaryPath, path); err == nil {
		return nil
	}
	if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
		return err
	}
	return os.Rename(temporaryPath, path)
}
