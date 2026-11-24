package appservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/tricknotes/merry-desktop/internal/core"
)

// VolumeBackup is a backup file in the backup folder (I-03).
type VolumeBackup struct {
	File      string    `json:"file"`
	SizeBytes int64     `json:"sizeBytes"`
	Modified  time.Time `json:"modified"`
	// Volume is the volume it was made from, as a default for restoring.
	Volume string `json:"volume"`
}

func (s *Service) backupDir() (string, error) {
	if s.BackupDir == "" {
		return "", errors.New("バックアップの保存先がありません")
	}
	return s.BackupDir, nil
}

// BackupFolder is the host folder where volume backups go.
func (s *Service) BackupFolder() string { return s.BackupDir }

// Volumes lists the volumes in the engine.
func (s *Service) Volumes() ([]core.VolumeInfo, error) {
	v, err := s.Controller.Volumes(context.Background())
	return v, notRunningError(err)
}

// BackupVolume writes a backup of the volume into the backup folder.
func (s *Service) BackupVolume(volume string) (core.BackupResult, error) {
	dir, err := s.backupDir()
	if err != nil {
		return core.BackupResult{}, err
	}
	r, err := s.Controller.BackupVolume(context.Background(), volume, dir)
	return r, notRunningError(err)
}

// VolumeBackups lists the backups in the backup folder, newest first.
func (s *Service) VolumeBackups() ([]VolumeBackup, error) {
	dir, err := s.backupDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []VolumeBackup{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []VolumeBackup{}
	for _, e := range entries {
		if e.IsDir() || !core.IsBackupFile(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, VolumeBackup{
			File: e.Name(), SizeBytes: info.Size(), Modified: info.ModTime(),
			Volume: core.VolumeFromBackup(e.Name()),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out, nil
}

// RestoreVolume restores a backup from the backup folder into a volume that
// does not exist yet, or is empty and unused.
func (s *Service) RestoreVolume(file, volume string) error {
	dir, err := s.backupDir()
	if err != nil {
		return err
	}
	if !core.IsBackupFile(file) {
		return errors.New("バックアップのファイルを選んでください")
	}
	return notRunningError(s.Controller.RestoreVolume(context.Background(), filepath.Join(dir, file), volume))
}
