// SPDX-License-Identifier: MIT

package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DefaultPath is where a destination repository keeps its configuration. The
// installer writes it; this repository never contains one.
const DefaultPath = ".review/config.yaml"

// Resolve produces the configuration for one run: defaults, overlaid with the
// file if there is one, overlaid with the environment, then validated.
//
// A repository with no config file is valid and yields Defaults — the tool must
// do something useful before anyone configures it.
func Resolve(root, path string, getenv func(string) string) (Config, Secrets, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return Config{}, Secrets{}, fmt.Errorf("resolving repository root: %w", err)
	}

	c := Defaults()
	c.Root = abs

	if path == "" {
		path = filepath.Join(abs, DefaultPath)
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(abs, path)
	}

	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// No file is not an error; see the doc comment.
	case err != nil:
		return Config{}, Secrets{}, fmt.Errorf("reading %s: %w", path, err)
	default:
		if err := decodeStrict(raw, &c); err != nil {
			return Config{}, Secrets{}, fmt.Errorf("%s: %w", path, err)
		}
		c.Root = abs // never settable from the file
	}

	var secrets Secrets
	if err := applyEnv(&c, &secrets, getenv); err != nil {
		return Config{}, Secrets{}, err
	}
	if err := c.Validate(); err != nil {
		return Config{}, Secrets{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, secrets, nil
}

// decodeStrict rejects unknown keys. A silently ignored typo in a config file is
// a bug that presents as "the analyzer I configured never ran".
//
// An empty or comments-only file decodes to io.EOF, which is not an error: a
// file someone has created but not filled in yet must behave exactly like no
// file at all, which is what the installer's first write leaves behind.
func decodeStrict(raw []byte, into *Config) error {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	switch err := dec.Decode(into); {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil:
		return err
	}
	return nil
}
