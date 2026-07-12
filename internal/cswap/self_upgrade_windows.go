//go:build windows

package cswap

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// replaceExecutable mirrors the standard Windows self-update trick: a
// running .exe cannot be overwritten or deleted while mapped, but it CAN
// be renamed out of the way (the OS keeps serving the already-running
// process's mapped pages regardless of what the file is now called), so
// this renames the current exe aside, moves the new binary into the
// original path, then makes a best-effort attempt to delete the old one
// (failure there is expected and ignored: Windows may still hold it open
// until the process exits, and a later `cswap upgrade` run will find no
// stale .old file since a previous successful upgrade already replaced
// it, or will simply overwrite it again).
func replaceExecutable(newBinPath, currentExePath string) error {
	old := currentExePath + ".old"
	os.Remove(old) // best-effort: a leftover from a previous upgrade

	if err := os.Rename(currentExePath, old); err != nil {
		return fmt.Errorf("renaming the running executable aside (do you have write access?): %w", err)
	}

	dir := filepath.Dir(currentExePath)
	staged := filepath.Join(dir, ".cswap-upgrade-staged.exe")
	if err := copyExecutableFile(newBinPath, staged); err != nil {
		if revertErr := os.Rename(old, currentExePath); revertErr != nil {
			return fmt.Errorf("staging new binary in %s: %w (additionally, could not restore the original executable: %v — your previous cswap.exe is preserved at %s, rename it back manually)", dir, err, revertErr, old)
		}
		return fmt.Errorf("staging new binary in %s: %w", dir, err)
	}
	if err := os.Rename(staged, currentExePath); err != nil {
		os.Remove(staged)
		if revertErr := os.Rename(old, currentExePath); revertErr != nil {
			return fmt.Errorf("moving the new binary into place: %w (additionally, could not restore the original executable: %v — your previous cswap.exe is preserved at %s, rename it back manually)", err, revertErr, old)
		}
		return fmt.Errorf("moving the new binary into place: %w", err)
	}

	os.Remove(newBinPath)
	os.Remove(old) // best-effort, often still locked; not an error if it fails
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
	return nil
}
