package localaccess

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
)

const beginMarker = "# BEGIN hotelctl local databases"
const endMarker = "# END hotelctl local databases"

func HostsBlock() string {
	var names []string
	for _, dependency := range config.Dependencies() {
		if dependency.Database != "" {
			names = append(names, dependency.Name)
		}
	}
	return beginMarker + "\n127.0.0.1 " + strings.Join(names, " ") + "\n" + endMarker + "\n"
}

// MergeHosts preserves unmanaged lines and refuses to redirect an existing name.
func MergeHosts(original []byte, remove bool) ([]byte, error) {
	text := string(original)
	start, end := strings.Index(text, beginMarker), strings.Index(text, endMarker)
	if start >= 0 || end >= 0 {
		if start < 0 || end < start || strings.Count(text, beginMarker) != 1 || strings.Count(text, endMarker) != 1 {
			return nil, fmt.Errorf("повреждён блок hotelctl в hosts; исправьте его вручную")
		}
		end += len(endMarker)
		if end < len(text) && text[end] == '\n' {
			end++
		}
		text = text[:start] + text[end:]
	}
	if remove {
		return []byte(text), nil
	}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(strings.SplitN(line, "#", 2)[0])
		if len(fields) < 2 {
			continue
		}
		for _, dependency := range config.Dependencies() {
			if dependency.Database == "" {
				continue
			}
			for _, name := range fields[1:] {
				if strings.EqualFold(name, dependency.Name) && fields[0] != "127.0.0.1" {
					return nil, fmt.Errorf("hosts уже содержит %s → %s; запись не изменена", name, fields[0])
				}
			}
		}
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return []byte(text + HostsBlock()), nil
}

func UpdateHosts(filename string, remove bool) error {
	original, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	updated, err := MergeHosts(original, remove)
	if err != nil || bytes.Equal(original, updated) {
		return err
	}
	info, err := os.Stat(filename)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(filename), ".hotelctl-hosts-*")
	if err != nil {
		return fmt.Errorf("для изменения %s нужны права администратора: %w", filename, err)
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if err := temp.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if _, err := temp.Write(updated); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), filename)
}
