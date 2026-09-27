// Package config stores VLNO connection credentials in a private local file.
// Unix enforces owner-only access. Windows needs an appropriate user directory
// ACL: Go's portable file modes do not establish or validate Windows ACLs.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const maxSize = 64 * 1024

type Config struct {
	Endpoint string `json:"endpoint"`
	APIKey   string `json:"api_key"`
	CAFile   string `json:"ca_file,omitempty"`
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", errors.New("cannot find user configuration directory")
	}
	return filepath.Join(dir, "vlno", "config.json"), nil
}

// inspect never follows a config-file symlink. Parent directory permissions are
// deliberately left unchanged; callers should use a trusted local directory.
func inspect(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("cannot inspect configuration file")
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("configuration file must be a regular file, not a symlink")
	}
	if err := privateFile(info); err != nil {
		return nil, err
	}
	return info, nil
}

func Load(path string) (Config, error) {
	info, err := inspect(path)
	if err != nil || info == nil {
		return Config{}, err
	}
	if info.Size() > maxSize {
		return Config{}, errors.New("configuration exceeds 64 KiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return Config{}, errors.New("cannot open configuration file")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return Config{}, errors.New("configuration file changed while opening")
	}
	if err := privateFile(opened); err != nil {
		return Config{}, err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxSize+1))
	if err != nil {
		return Config{}, errors.New("cannot read configuration file")
	}
	if len(data) > maxSize {
		return Config{}, errors.New("configuration exceeds 64 KiB")
	}
	// Reject null, scalar and array documents, in addition to unknown fields and
	// trailing values. Never include decoder errors: input may contain secrets.
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return Config{}, errors.New("configuration must be a JSON object")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var c Config
	if err := d.Decode(&c); err != nil {
		return Config{}, errors.New("invalid configuration JSON")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return Config{}, errors.New("configuration has trailing content")
	}
	return c, nil
}

func Save(path string, c Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return errors.New("cannot encode configuration")
	}
	data = append(data, '\n')
	if len(data) > maxSize {
		return errors.New("configuration exceeds 64 KiB")
	}
	if _, err := inspect(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return errors.New("cannot create configuration directory")
	}
	f, err := os.CreateTemp(dir, ".vlno-config-*")
	if err != nil {
		return errors.New("cannot create temporary configuration file")
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return errors.New("cannot secure configuration file")
	}
	if _, err := f.Write(data); err != nil {
		return errors.New("cannot write configuration file")
	}
	if err := f.Sync(); err != nil {
		return errors.New("cannot sync configuration file")
	}
	if err := f.Close(); err != nil {
		return errors.New("cannot close configuration file")
	}
	// Recheck before replacement. Rename replaces the directory entry, so even
	// a subsequent destination symlink cannot redirect the credential write.
	if _, err := inspect(path); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return errors.New("cannot replace configuration file")
	}
	return nil
}

func Delete(path string) error {
	info, err := inspect(path)
	if err != nil || info == nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("cannot delete configuration file")
	}
	return nil
}
