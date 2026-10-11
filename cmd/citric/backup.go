package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/blaxstar/citric/internal/brand"
)

var cmdBackup = &cobra.Command{
	Use:   "backup",
	Short: "Write a backup archive of the database, repos and server state",
	Long: `Write a .tar.gz holding a consistent snapshot of the database, every bare
repo, and the rest of data_dir (JWT secret, mailmap, settings files). Build
logs, caches and the /dl client binaries are left out: they are rebuilt.

Safe to run while the server is up: the database is copied with SQLite's
VACUUM INTO. A push landing mid-backup may be missed; run it again or stop the
server for a guaranteed-quiet copy.

Restore: stop the server, extract the archive, and point data_dir and
repos_dir at the extracted data/ and repos/ directories (or move them into
place):

  tar -xzf citric-backup-*.tar.gz -C /var/lib/citric`,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, _ := cmd.Flags().GetString("out")
		cfg, db, err := openServerDB()
		if err != nil {
			return err
		}
		defer db.Close()
		if out == "" {
			out = fmt.Sprintf("%s-backup-%s.tar.gz", brand.Name, time.Now().UTC().Format("20060102-150405"))
		}

		tmp, err := os.MkdirTemp("", brand.Name+"-backup-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		dbName := filepath.Base(brand.DataFile(cfg.Server.DataDir, ".db"))
		snapshot := filepath.Join(tmp, dbName)
		if err := db.Snapshot(snapshot); err != nil {
			return err
		}

		f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		gz := gzip.NewWriter(f)
		tw := tar.NewWriter(gz)

		skipData := func(rel string) bool {
			top, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
			switch top {
			case "cache", "dist", "logs", "run":
				return true
			}
			base := filepath.Base(rel)
			return strings.HasPrefix(base, dbName) // live db + -wal/-shm; the snapshot replaces them
		}
		err = addTree(tw, cfg.Server.DataDir, "data", skipData)
		if err == nil {
			err = addFile(tw, snapshot, "data/"+dbName)
		}
		if err == nil {
			err = addTree(tw, cfg.Server.ReposDir, "repos", nil)
		}
		for _, c := range []io.Closer{tw, gz, f} {
			if cerr := c.Close(); err == nil {
				err = cerr
			}
		}
		if err != nil {
			os.Remove(out)
			return err
		}
		fmt.Printf("✓ backup written to %s\n", out)
		return nil
	},
}

// addTree archives root under prefix, skipping relative paths skip rejects.
// A missing root is not an error (e.g. no repos yet).
func addTree(tw *tar.Writer, root, prefix string, skip func(rel string) bool) error {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		if skip != nil && skip(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		name := prefix + "/" + filepath.ToSlash(rel)
		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			hdr, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			hdr.Name = name + "/"
			return tw.WriteHeader(hdr)
		}
		if !d.Type().IsRegular() {
			return nil // sockets, symlinks: nothing a restore needs
		}
		return addFile(tw, path, name)
	})
}

func addFile(tw *tar.Writer, path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	hdr.Name = name
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

func init() {
	cmdBackup.Flags().StringP("out", "o", "", "archive path (default: citric-backup-<timestamp>.tar.gz)")
}
