package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

var profileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// AddProfile validates and appends a profile without replacing an existing name.
// YAML nodes retain unrelated fields and comments; encoding may change spacing.
func AddProfile(path, name string, profile Profile) error {
	if !profileName.MatchString(name) {
		return fmt.Errorf("profile name must contain letters, digits, dots, underscores or hyphens and start with a letter or digit")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	lock, err := os.OpenFile(path+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("lock config (another setup may be saving): %w", err)
	}
	defer os.Remove(path + ".lock")
	if err := lock.Close(); err != nil {
		return err
	}
	if st, err := os.Lstat(path); err == nil && !st.Mode().IsRegular() {
		return fmt.Errorf("config must be a regular file, not a symlink or directory")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	before, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read config: %w", err)
	}
	var doc yaml.Node
	if len(bytes.TrimSpace(before)) != 0 {
		dec := yaml.NewDecoder(bytes.NewReader(before))
		if err := dec.Decode(&doc); err != nil {
			return fmt.Errorf("config YAML could not be parsed")
		}
		var extra yaml.Node
		if err := dec.Decode(&extra); err != io.EOF {
			return fmt.Errorf("config must contain one YAML document")
		}
		var f File
		if err := doc.Decode(&f); err != nil {
			return fmt.Errorf("config fields could not be parsed")
		}
	} else {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("config must be a YAML mapping")
	}
	root := doc.Content[0]
	var profiles *yaml.Node
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == "<<" {
			return fmt.Errorf("top-level YAML merges require manual configuration")
		}
		if root.Content[i].Value == "profiles" {
			profiles = root.Content[i+1]
		}
	}
	if profiles == nil {
		profiles = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "profiles"}, profiles)
	}
	if profiles.Tag == "!!null" {
		profiles.Kind, profiles.Tag, profiles.Value = yaml.MappingNode, "!!map", ""
	}
	if profiles.Kind != yaml.MappingNode {
		return fmt.Errorf("profiles must be a YAML mapping without aliases")
	}
	for i := 0; i < len(profiles.Content); i += 2 {
		if profiles.Content[i].Value == name || profiles.Content[i].Value == "<<" {
			return fmt.Errorf("profile already exists or profiles use YAML merges; refusing overwrite")
		}
	}
	var node yaml.Node
	if err := node.Encode(profile); err != nil {
		return err
	}
	profiles.Content = append(profiles.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, &node)
	data, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if _, err := Load(data, Options{Profile: name}); err != nil {
		return fmt.Errorf("profile validation failed: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".micko-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	latest, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if !bytes.Equal(before, latest) {
		return fmt.Errorf("config changed during setup; refusing overwrite, run setup again")
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}
