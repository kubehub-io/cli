package kubehubcli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBackupFilePath(t *testing.T) {
	now := time.Date(2026, 11, 27, 10, 30, 0, 0, time.UTC)
	require.Equal(t, "/etc/containerd/config.toml.2026-11-27.bak", backupFilePath("/etc/containerd/config.toml", now))
}

func TestBackupFileAsRootAt(t *testing.T) {
	withEUID(t, 0)

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0o644))

	now := time.Date(2026, 11, 27, 10, 30, 0, 0, time.UTC)
	require.NoError(t, backupFileAsRootAt(path, now))

	backup := path + ".2026-11-27.bak"
	data, err := os.ReadFile(backup)
	require.NoError(t, err)
	require.Equal(t, "original", string(data))

	// A second backup on the same day must not overwrite the original.
	require.NoError(t, os.WriteFile(path, []byte("updated"), 0o644))
	require.NoError(t, backupFileAsRootAt(path, now))
	data, err = os.ReadFile(backup)
	require.NoError(t, err)
	require.Equal(t, "original", string(data))
}

func TestWriteFileAsRootIfChangedWithBackup(t *testing.T) {
	withEUID(t, 0)

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0o644))

	changed, err := writeFileAsRootIfChangedWithBackup(path, []byte("updated"), 0o644, true)
	require.NoError(t, err)
	require.True(t, changed)
	backupPath, err := filepath.Glob(path + ".*.bak")
	require.NoError(t, err)
	require.Len(t, backupPath, 1)
	backup, err := os.ReadFile(backupPath[0])
	require.NoError(t, err)
	require.Equal(t, "original", string(backup))

	changed, err = writeFileAsRootIfChangedWithBackup(path, []byte("updated"), 0o644, true)
	require.NoError(t, err)
	require.False(t, changed)
	backups, err := filepath.Glob(path + ".*.bak")
	require.NoError(t, err)
	require.Len(t, backups, 1)
}

func TestDumpDefaultContainerdConfig(t *testing.T) {
	withEUID(t, 0)

	binDir := t.TempDir()
	fakeContainerd := filepath.Join(binDir, "containerd")
	require.NoError(t, os.WriteFile(fakeContainerd, []byte("#!/bin/sh\necho 'containerd default config'\n"), 0o755))
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	configPath := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, dumpDefaultContainerdConfig(configPath))

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Equal(t, "containerd default config\n", string(data))
}
