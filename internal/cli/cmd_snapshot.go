package cli

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tgpski/leather/internal/config"
	"github.com/tgpski/leather/internal/fileutil"
)

const snapshotUsage = `Usage:
  leather snapshot save   [--output <path>]
  leather snapshot restore --input <path> [--force]
`

// RunSnapshot dispatches to save or restore sub-subcommands.
func RunSnapshot(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, snapshotUsage)
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "save":
		return RunSnapshotSave(rest, stdout, stderr)
	case "restore":
		return RunSnapshotRestore(rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "leather snapshot: unknown subcommand %q\n\n", sub)
		fmt.Fprint(stderr, snapshotUsage)
		return 2
	}
}

// RunSnapshotSave creates a point-in-time tar.gz archive of runtime state.
//
// Usage: leather snapshot save [--output <path>]
func RunSnapshotSave(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("snapshot save", stderr)
	config.BindFlags(fs)
	defaultOut := "leather-snapshot-" + time.Now().UTC().Format("20060102T150405") + ".tar.gz"
	output := fs.String("output", defaultOut, "destination archive path")
	if !parseFlags(fs, args) {
		return 2
	}

	cfg, err := config.Load(fs)
	if err != nil {
		fmt.Fprintf(stderr, "leather snapshot save: %v\n", err)
		return 1
	}

	// Verify no serve process is holding the lock.
	lockPath := filepath.Join(cfg.StateDir, "leather.lock")
	lf, lockErr := acquireProcessLock(lockPath)
	if lockErr != nil {
		fmt.Fprintf(stderr, "leather snapshot save: serve is running (lock held at %s); stop it before saving\n", lockPath)
		return 1
	}
	releaseProcessLock(lf)

	// Collect source directories.
	type srcDir struct {
		root   string // absolute path to directory
		prefix string // prefix inside the archive
	}
	dirs := []srcDir{
		{filepath.Join(cfg.StateDir, "queues"), "state/queues"},
		{filepath.Join(cfg.StateDir, "runs"), "state/runs"},
		{filepath.Join(cfg.StateDir, "cache"), "state/cache"},
	}

	// Include tannery dirs if configured.
	if cfg.TanneryFile != "" {
		tann, tErr := config.LoadTannery(cfg.TanneryFile)
		if tErr == nil {
			if tann.HideDir != "" {
				dirs = append(dirs, srcDir{tann.HideDir, "tannery/hides"})
			}
			if tann.ArtifactDir != "" {
				dirs = append(dirs, srcDir{tann.ArtifactDir, "tannery/artifacts"})
			}
		}
	}

	var totalFiles int
	err = fileutil.AtomicWriteFileFunc(*output, 0600, func(w io.Writer) error {
		gz := gzip.NewWriter(w)
		tw := tar.NewWriter(gz)

		for _, d := range dirs {
			n, walkErr := tarDir(tw, d.root, d.prefix)
			if walkErr != nil {
				return walkErr
			}
			totalFiles += n
		}

		if err := tw.Close(); err != nil {
			return fmt.Errorf("tar close: %w", err)
		}
		return gz.Close()
	})
	if err != nil {
		fmt.Fprintf(stderr, "leather snapshot save: %v\n", err)
		return 1
	}

	fi, _ := os.Stat(*output)
	size := int64(0)
	if fi != nil {
		size = fi.Size()
	}
	fmt.Fprintf(stdout, "snapshot saved: %s (%d files, %s)\n", *output, totalFiles, formatBytes(size))
	return 0
}

// RunSnapshotRestore extracts a snapshot archive into the configured state directory.
//
// Usage: leather snapshot restore --input <path> [--force]
func RunSnapshotRestore(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("snapshot restore", stderr)
	config.BindFlags(fs)
	input := fs.String("input", "", "snapshot archive to restore (required)")
	force := fs.Bool("force", false, "overwrite existing state without prompting")
	if !parseFlags(fs, args) {
		return 2
	}
	if *input == "" {
		fmt.Fprintf(stderr, "leather snapshot restore: --input is required\n")
		return 2
	}

	cfg, err := config.Load(fs)
	if err != nil {
		fmt.Fprintf(stderr, "leather snapshot restore: %v\n", err)
		return 1
	}

	// Verify no serve process is running. acquireProcessLock creates the lock
	// file as a side effect; remove it so it doesn't appear as a restored file.
	lockPath := filepath.Join(cfg.StateDir, "leather.lock")
	lf, lockErr := acquireProcessLock(lockPath)
	if lockErr != nil {
		fmt.Fprintf(stderr, "leather snapshot restore: serve is running (lock held at %s); stop it before restoring\n", lockPath)
		return 1
	}
	releaseProcessLock(lf)
	_ = os.Remove(lockPath)

	// Guard against clobbering an existing non-empty state dir.
	if !*force {
		if occupied, _ := dirHasFiles(cfg.StateDir); occupied {
			fmt.Fprintf(stderr, "leather snapshot restore: %s is not empty; use --force to overwrite\n", cfg.StateDir)
			return 1
		}
	}

	f, err := os.Open(*input)
	if err != nil {
		fmt.Fprintf(stderr, "leather snapshot restore: %v\n", err)
		return 1
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		fmt.Fprintf(stderr, "leather snapshot restore: not a valid gzip archive: %v\n", err)
		return 1
	}
	defer gz.Close()

	// Build a prefix→destination map from the archive layout.
	prefixMap := map[string]string{
		"state/queues":      filepath.Join(cfg.StateDir, "queues"),
		"state/runs":        filepath.Join(cfg.StateDir, "runs"),
		"state/cache":       filepath.Join(cfg.StateDir, "cache"),
		"tannery/hides":     "",
		"tannery/artifacts": "",
	}

	// Populate tannery destinations if configured.
	if cfg.TanneryFile != "" {
		tann, tErr := config.LoadTannery(cfg.TanneryFile)
		if tErr == nil {
			prefixMap["tannery/hides"] = tann.HideDir
			prefixMap["tannery/artifacts"] = tann.ArtifactDir
		}
	}

	tr := tar.NewReader(gz)
	var restoredFiles int
	for {
		hdr, tErr := tr.Next()
		if tErr == io.EOF {
			break
		}
		if tErr != nil {
			fmt.Fprintf(stderr, "leather snapshot restore: read archive: %v\n", tErr)
			return 1
		}

		destPath, mapErr := resolveArchivePath(hdr.Name, prefixMap)
		if mapErr != nil {
			// Entry belongs to a tannery prefix with no configured destination; skip.
			continue
		}

		// Reject path traversal.
		if strings.Contains(hdr.Name, "..") {
			fmt.Fprintf(stderr, "leather snapshot restore: unsafe path in archive: %s\n", hdr.Name)
			return 1
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0700); err != nil {
				fmt.Fprintf(stderr, "leather snapshot restore: mkdir %s: %v\n", destPath, err)
				return 1
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(destPath), 0700); err != nil {
				fmt.Fprintf(stderr, "leather snapshot restore: mkdir %s: %v\n", filepath.Dir(destPath), err)
				return 1
			}
			if err := writeRestoreFile(destPath, tr); err != nil {
				fmt.Fprintf(stderr, "leather snapshot restore: write %s: %v\n", destPath, err)
				return 1
			}
			restoredFiles++
		}
	}

	fmt.Fprintf(stdout, "snapshot restored: %d files → %s\n", restoredFiles, cfg.StateDir)
	return 0
}

// tarDir walks root and writes every regular file into tw under archivePrefix/rel.
// Returns the number of files added.
func tarDir(tw *tar.Writer, root, archivePrefix string) (int, error) {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return 0, nil // directory doesn't exist yet — skip silently
	}
	var count int
	err := filepath.Walk(root, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		archivePath := archivePrefix + "/" + filepath.ToSlash(rel)

		if fi.IsDir() {
			hdr := &tar.Header{
				Typeflag: tar.TypeDir,
				Name:     archivePath + "/",
				Mode:     0700,
				ModTime:  fi.ModTime(),
			}
			return tw.WriteHeader(hdr)
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		// Skip transient files that serve regenerates on startup.
		base := filepath.Base(path)
		if base == "leather.lock" || base == "devtools.token" {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		hdr := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     archivePath,
			Size:     fi.Size(),
			Mode:     0600,
			ModTime:  fi.ModTime(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

// resolveArchivePath maps an archive entry name to a filesystem destination.
// Returns an error when no mapping is configured (e.g. tannery dir not set).
func resolveArchivePath(name string, prefixMap map[string]string) (string, error) {
	for prefix, dest := range prefixMap {
		p := prefix + "/"
		if name == prefix || strings.HasPrefix(name, p) {
			if dest == "" {
				return "", fmt.Errorf("no destination for prefix %q", prefix)
			}
			rel := strings.TrimPrefix(name, p)
			return filepath.Join(dest, filepath.FromSlash(rel)), nil
		}
	}
	return "", fmt.Errorf("unknown archive prefix for %q", name)
}

// writeRestoreFile writes a tar entry's content to dest atomically.
func writeRestoreFile(dest string, r io.Reader) error {
	return fileutil.AtomicWriteFileFunc(dest, 0600, func(w io.Writer) error {
		_, err := io.Copy(w, r)
		return err
	})
}

// dirHasFiles reports whether dir exists and contains at least one file or
// subdirectory, ignoring leather.lock and devtools.token.
func dirHasFiles(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		n := e.Name()
		if n == "leather.lock" || n == "devtools.token" {
			continue
		}
		return true, nil
	}
	return false, nil
}

// formatBytes returns a human-readable byte count.
func formatBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
