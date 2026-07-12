//go:build !windows

package cswap

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// replaceExecutable mirrors the standard Unix self-update trick: renaming
// a new file over the path of the currently-running executable is safe,
// the OS keeps serving the old inode's bytes to the process that's
// already running it, only a future exec of this path sees the new
// content. os.Rename requires staying on one filesystem, so newBinPath
// (typically under the system temp dir, which may be a different
// filesystem/tmpfs) is first copied into currentExePath's own directory,
// then renamed from there.
func replaceExecutable(newBinPath, currentExePath string) error {
	dir := filepath.Dir(currentExePath)
	staged := filepath.Join(dir, ".cswap-upgrade-staged")

	if err := copyExecutableFile(newBinPath, staged); err != nil {
		return fmt.Errorf("staging new binary in %s (do you have write access, e.g. via sudo?): %w", dir, err)
	}
	if err := os.Rename(staged, currentExePath); err != nil {
		os.Remove(staged)
		return fmt.Errorf("replacing %s: %w", currentExePath, err)
	}
	os.Remove(newBinPath)
	return nil
}

func copyExecutableFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Chmod(0o755)
}
