package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type archiveEntry struct {
	absolutePath string
	archivePath  string
	info         fs.FileInfo
}

func main() {
	format := flag.String("format", "", "archive format: tar.gz or zip")
	root := flag.String("root", "", "directory whose files become the archive root")
	output := flag.String("output", "", "output archive path")
	epoch := flag.Int64("epoch", 0, "Unix timestamp recorded in every archive entry")
	flag.Parse()

	if *format == "" || *root == "" || *output == "" || flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := createArchive(*format, *root, *output, *epoch); err != nil {
		fmt.Fprintf(os.Stderr, "build release archive: %v\n", err)
		os.Exit(1)
	}
}

func createArchive(format, root, output string, epoch int64) (err error) {
	entries, err := collectEntries(root)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return errors.New("archive root contains no files")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}

	file, err := os.Create(output)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := file.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(output)
		}
	}()

	timestamp := time.Unix(epoch, 0).UTC()
	switch format {
	case "tar.gz":
		err = writeTarGz(file, entries, timestamp)
	case "zip":
		err = writeZip(file, entries, timestamp)
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
	if err != nil {
		return err
	}
	return nil
}

func collectEntries(root string) ([]archiveEntry, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !rootInfo.IsDir() {
		return nil, fmt.Errorf("archive root %q is not a directory", root)
	}

	var entries []archiveEntry
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("release archives do not allow symlinks: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("release archives allow regular files only: %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, archiveEntry{
			absolutePath: path,
			archivePath:  filepath.ToSlash(relative),
			info:         info,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].archivePath < entries[j].archivePath
	})
	return entries, nil
}

func writeTarGz(destination io.Writer, entries []archiveEntry, timestamp time.Time) error {
	gzipWriter, err := gzip.NewWriterLevel(destination, gzip.BestCompression)
	if err != nil {
		return err
	}
	gzipWriter.Header.ModTime = timestamp
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)

	for _, entry := range entries {
		mode := normalizedMode(entry.info.Mode())
		header := &tar.Header{
			Name:     entry.archivePath,
			Mode:     int64(mode),
			Size:     entry.info.Size(),
			ModTime:  timestamp,
			Typeflag: tar.TypeReg,
			Format:   tar.FormatUSTAR,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if err := copyFile(tarWriter, entry.absolutePath); err != nil {
			return err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	return gzipWriter.Close()
}

func writeZip(destination io.Writer, entries []archiveEntry, timestamp time.Time) error {
	if timestamp.Before(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)) {
		timestamp = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	zipWriter := zip.NewWriter(destination)
	for _, entry := range entries {
		header := &zip.FileHeader{
			Name:   entry.archivePath,
			Method: zip.Deflate,
		}
		header.SetMode(fs.FileMode(normalizedMode(entry.info.Mode())))
		header.SetModTime(timestamp)
		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}
		if err := copyFile(writer, entry.absolutePath); err != nil {
			return err
		}
	}
	return zipWriter.Close()
}

func normalizedMode(mode fs.FileMode) uint32 {
	if mode&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

func copyFile(destination io.Writer, path string) error {
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()
	_, err = io.Copy(destination, source)
	return err
}
