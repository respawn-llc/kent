package config

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable/edit"
)

const settingsDocumentUTF8BOM = "\xef\xbb\xbf"

type settingsDocument struct {
	path     string
	document *edit.Document
	prefix   []byte
	changed  bool
}

func readSettingsDocument(path string) (*settingsDocument, settingsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read settings file %s: %w", path, err)
	}
	raw, err := decodeSettingsFile(path, data)
	if err != nil {
		return nil, nil, err
	}
	editorInput := data
	var prefix []byte
	if bytes.HasPrefix(data, []byte(settingsDocumentUTF8BOM)) {
		prefix = bytes.Clone(data[:len(settingsDocumentUTF8BOM)])
		editorInput = data[len(settingsDocumentUTF8BOM):]
	}
	document, err := edit.Parse(editorInput)
	if err != nil {
		return nil, nil, fmt.Errorf("parse settings document %s: %w", path, err)
	}
	return &settingsDocument{path: path, document: document, prefix: prefix}, raw, nil
}

func (d *settingsDocument) settings() (settingsFile, error) {
	return decodeSettingsFile(d.path, d.bytes())
}

func (d *settingsDocument) get(path []string) (any, bool) {
	return d.document.Get(path)
}

func (d *settingsDocument) bytes() []byte {
	content := d.document.Bytes()
	if d.prefix == nil {
		return content
	}
	return append(bytes.Clone(d.prefix), content...)
}

func (d *settingsDocument) set(path []string, value any) error {
	if err := d.document.Set(path, value); err != nil {
		return fmt.Errorf("edit setting %q in %s: %w", strings.Join(path, "."), d.path, err)
	}
	d.changed = true
	return nil
}

func (d *settingsDocument) delete(path []string) error {
	if !d.document.Has(path) {
		return nil
	}
	if !d.document.Delete(path) || d.document.Has(path) {
		return fmt.Errorf("delete setting %q from %s: TOML editor left it present", strings.Join(path, "."), d.path)
	}
	d.changed = true
	return nil
}

func (d *settingsDocument) save() error {
	if !d.changed {
		return nil
	}
	if err := replaceSettingsFile(d.path, string(d.bytes())); err != nil {
		return fmt.Errorf("write settings file %s: %w", d.path, err)
	}
	return nil
}
