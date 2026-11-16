package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ManagedPrefixes are the directories a package manager owns. A binary at or
// under one of them is updated by that manager, never by vh. A bare /usr is
// absent on purpose: it would also refuse a manual install in /usr/local/bin.
// The list is deck's.
var ManagedPrefixes = []string{
	"/usr/bin", "/usr/sbin", "/bin", "/sbin", "/usr/lib", "/usr/libexec",
	"/opt/homebrew", "/usr/local/Cellar", "/home/linuxbrew", "/nix/store", "/snap",
}

// managedPrefix is the managed prefix path lies at or under, or "".
func managedPrefix(path string) string {
	p := filepath.ToSlash(filepath.Clean(path))
	for _, m := range ManagedPrefixes {
		if p == m || strings.HasPrefix(p, m+"/") {
			return m
		}
	}
	return ""
}

func refuseManaged(path, prefix string) error {
	return refuse("%s is under %s, which a package manager owns", "update vh with that package manager (brew, apt, nix or snap)", path, prefix)
}

// target is the file Run replaces: the running binary with symlinks resolved.
// A path under a managed prefix, before or after resolving, is refused, and so
// is a directory vh cannot write to. Nothing is changed and no privilege is
// ever escalated.
func (o Options) target() (string, error) {
	if o.Exe == "" {
		return "", refuse("cannot locate the running vh binary", "install a release with install.sh")
	}
	if m := managedPrefix(o.Exe); m != "" {
		return "", refuseManaged(o.Exe, m)
	}
	target, err := filepath.EvalSymlinks(o.Exe)
	if err != nil {
		return "", refuse("cannot resolve the running vh binary %s: %v", "install a release with install.sh", o.Exe, err)
	}
	if m := managedPrefix(target); m != "" {
		return "", refuseManaged(target, m)
	}
	return target, probeWritable(filepath.Dir(target))
}

// probeWritable creates and removes a file in dir.
func probeWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".vh.probe.*")
	if err != nil {
		return refuse("cannot write to the install directory %s (%v)",
			"re-run with write access, or reinstall to ~/.local/bin with VH_INSTALL_DIR=~/.local/bin using install.sh", dir, err)
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return nil
}

// replace writes the vh binary of the tar.gz archive to a temporary file in
// target's directory and renames it over target, so a running vh is replaced
// safely and the old file is never half-written.
func replace(target string, archive []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(target), ".vh.*")
	if err != nil {
		return refuse("cannot stage the new binary: %v", "nothing was installed", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	err = extract(archive, tmp)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o755) //nolint:gosec // G302: the installed vh must be executable, mode 0755 as install.sh sets
	}
	if err == nil {
		err = os.Rename(tmp.Name(), target)
	}
	if err != nil {
		return refuse("could not replace %s: %v", "nothing was installed", target, err)
	}
	return nil
}

// extract copies the regular file named vh out of the tar.gz archive.
func extract(archive []byte, dst io.Writer) error {
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("the archive is not a gzip file: %w", err)
	}
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("the archive holds no vh binary")
		}
		if err != nil {
			return fmt.Errorf("reading the archive: %w", err)
		}
		if hdr.Typeflag == tar.TypeReg && strings.TrimPrefix(hdr.Name, "./") == "vh" {
			return copyCapped(dst, tr)
		}
	}
}

// copyCapped copies src to dst, failing when src is longer than maxArchive.
func copyCapped(dst io.Writer, src io.Reader) error {
	n, err := io.Copy(dst, io.LimitReader(src, maxArchive+1))
	if err == nil && n > maxArchive {
		err = errors.New("the vh binary in the archive is too large")
	}
	return err
}
