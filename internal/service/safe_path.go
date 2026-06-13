package service

import (
	"fmt"
	"os"
	"strings"
)

func writeFileInDir(dir, name string, data []byte, perm os.FileMode) error {
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("%w: %q", errInvalidCopyDestination, name)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open backup directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	return wrapRepo("write pre-restore backup", root.WriteFile(name, data, perm))
}
