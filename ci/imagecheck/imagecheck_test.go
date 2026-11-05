package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// elfBinary is a minimal 64-bit little-endian ELF header with the machine set.
func elfBinary(machine elf.Machine) []byte {
	header := make([]byte, 64)
	copy(header, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(header[16:], uint16(elf.ET_EXEC))
	binary.LittleEndian.PutUint16(header[18:], uint16(machine))
	binary.LittleEndian.PutUint32(header[20:], 1)
	binary.LittleEndian.PutUint16(header[52:], 64) // e_ehsize
	return header
}

func layerTar(t *testing.T, name string, content []byte, gzipped bool) []byte {
	t.Helper()
	var raw bytes.Buffer
	writer := tar.NewWriter(&raw)
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if !gzipped {
		return raw.Bytes()
	}
	var zipped bytes.Buffer
	zip := gzip.NewWriter(&zipped)
	if _, err := zip.Write(raw.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zip.Close(); err != nil {
		t.Fatal(err)
	}
	return zipped.Bytes()
}

// ociBuilder assembles an OCI layout tar in memory.
type ociBuilder struct {
	t     *testing.T
	files map[string][]byte
}

func newOCI(t *testing.T) *ociBuilder { return &ociBuilder{t: t, files: map[string][]byte{}} }

func (b *ociBuilder) blob(data []byte) string {
	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])
	b.files["blobs/sha256/"+hexSum] = data
	return "sha256:" + hexSum
}

func (b *ociBuilder) json(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		b.t.Fatal(err)
	}
	return b.blob(data)
}

type imageSpec struct {
	os, arch, user string
	entrypoint     []string
	machine        elf.Machine
	binaryName     string
	gzipped        bool
}

func goodSpec(arch string, machine elf.Machine) imageSpec {
	return imageSpec{os: "linux", arch: arch, user: "65532:65532", entrypoint: []string{"/board"}, machine: machine, binaryName: "board"}
}

// image adds one platform manifest and returns its index descriptor.
func (b *ociBuilder) image(spec imageSpec) map[string]any {
	layer := b.blob(layerTar(b.t, spec.binaryName, elfBinary(spec.machine), spec.gzipped))
	config := b.json(map[string]any{
		"os": spec.os, "architecture": spec.arch,
		"config": map[string]any{"User": spec.user, "Entrypoint": spec.entrypoint},
	})
	manifest := b.json(map[string]any{
		"config": map[string]any{"digest": config},
		"layers": []map[string]any{{"digest": layer}},
	})
	return map[string]any{"digest": manifest, "platform": map[string]any{"os": spec.os, "architecture": spec.arch}}
}

func (b *ociBuilder) tar(rootEntries ...map[string]any) *bytes.Reader {
	b.files["index.json"], _ = json.Marshal(map[string]any{"manifests": rootEntries})
	var out bytes.Buffer
	writer := tar.NewWriter(&out)
	for name, data := range b.files {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			b.t.Fatal(err)
		}
		if _, err := writer.Write(data); err != nil {
			b.t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		b.t.Fatal(err)
	}
	return bytes.NewReader(out.Bytes())
}

// index nests the given platform manifests in an image index, like buildx does.
func (b *ociBuilder) index(entries ...map[string]any) map[string]any {
	return map[string]any{"digest": b.json(map[string]any{"manifests": entries})}
}

func TestVerifyAcceptsTheTwoPlatformsCrossCompiled(t *testing.T) {
	b := newOCI(t)
	amd := goodSpec("amd64", elf.EM_X86_64)
	amd.gzipped = true
	images, err := Inspect(b.tar(b.index(b.image(amd), b.image(goodSpec("arm64", elf.EM_AARCH64)))))
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(images) != 2 {
		t.Fatalf("images = %+v, want 2", images)
	}
	if problems := Verify(images); len(problems) != 0 {
		t.Errorf("Verify = %q, want none", problems)
	}
}

func TestVerifyFlagsWhatSpecTwelveFourForbids(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(arm *imageSpec)
		want   string
	}{
		{"root user", func(s *imageSpec) { s.user = "0:0" }, "runs as root"},
		{"no user", func(s *imageSpec) { s.user = "" }, "runs as root"},
		{"named root", func(s *imageSpec) { s.user = "root" }, "runs as root"},
		{"wrong entrypoint", func(s *imageSpec) { s.entrypoint = []string{"/bin/sh"} }, "want /board"},
		{"no entrypoint", func(s *imageSpec) { s.entrypoint = nil }, "want /board"},
		{"binary of the build host", func(s *imageSpec) { s.machine = elf.EM_X86_64 }, "not cross-compiled"},
	}
	for _, tc := range cases {
		b := newOCI(t)
		arm := goodSpec("arm64", elf.EM_AARCH64)
		tc.mutate(&arm)
		images, err := Inspect(b.tar(b.image(goodSpec("amd64", elf.EM_X86_64)), b.image(arm)))
		if err != nil {
			t.Fatalf("%s: Inspect: %v", tc.name, err)
		}
		problems := strings.Join(Verify(images), "\n")
		if !strings.Contains(problems, "linux/arm64") || !strings.Contains(problems, tc.want) {
			t.Errorf("%s: problems = %q, want linux/arm64 and %q", tc.name, problems, tc.want)
		}
	}
}

func TestVerifyFlagsAMissingPlatform(t *testing.T) {
	b := newOCI(t)
	images, err := Inspect(b.tar(b.image(goodSpec("amd64", elf.EM_X86_64))))
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if problems := strings.Join(Verify(images), "\n"); !strings.Contains(problems, "linux/arm64: no manifest") {
		t.Errorf("problems = %q, want a missing linux/arm64 manifest", problems)
	}
}

func TestInspectSkipsAttestationManifests(t *testing.T) {
	b := newOCI(t)
	attestation := map[string]any{
		"digest":      "sha256:" + strings.Repeat("0", 64), // not in the layout: must not be read
		"annotations": map[string]any{attestationKey: "attestation-manifest"},
	}
	images, err := Inspect(b.tar(b.index(b.image(goodSpec("amd64", elf.EM_X86_64)), attestation)))
	if err != nil || len(images) != 1 {
		t.Errorf("Inspect = %+v, %v, want the one real image", images, err)
	}
}

func TestInspectRejectsBrokenLayouts(t *testing.T) {
	missingBinary := newOCI(t)
	noBinary := goodSpec("amd64", elf.EM_X86_64)
	noBinary.binaryName = "other"
	notELF := newOCI(t)
	notELFImage := notELF.image(goodSpec("amd64", elf.EM_X86_64))
	notELF.files = map[string][]byte{} // drop the blobs: manifest not found
	cases := []struct {
		name string
		in   func() *bytes.Reader
		want string
	}{
		{"no /board in any layer", func() *bytes.Reader { return missingBinary.tar(missingBinary.image(noBinary)) }, "no /board binary"},
		{"a blob that is not in the layout", func() *bytes.Reader { return notELF.tar(notELFImage) }, "is not in the layout"},
		{"index.json is not JSON", func() *bytes.Reader {
			b := newOCI(t)
			b.files["index.json"] = []byte("{")
			return b.rawTar()
		}, "parse index.json"},
		{"not a tar", func() *bytes.Reader { return bytes.NewReader(bytes.Repeat([]byte("x"), 600)) }, "read the OCI tar"},
	}
	for _, tc := range cases {
		if _, err := Inspect(tc.in()); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
}

// rawTar writes the files as they are, without regenerating index.json.
func (b *ociBuilder) rawTar() *bytes.Reader {
	var out bytes.Buffer
	writer := tar.NewWriter(&out)
	for name, data := range b.files {
		_ = writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg})
		_, _ = writer.Write(data)
	}
	_ = writer.Close()
	return bytes.NewReader(out.Bytes())
}

func TestBlobRejectsAMalformedDigest(t *testing.T) {
	if _, err := (layout{}).blob("nocolon"); err == nil || !strings.Contains(err.Error(), "malformed digest") {
		t.Errorf("err = %v, want malformed digest", err)
	}
}

func TestFileInLayerRejectsACorruptGzip(t *testing.T) {
	if _, err := fileInLayer([]byte{0x1f, 0x8b, 0, 0}, "board"); err == nil {
		t.Error("a truncated gzip layer was read without error")
	}
	if _, err := fileInLayer(bytes.Repeat([]byte("y"), 600), "board"); err == nil {
		t.Error("a layer that is not a tar was read without error")
	}
}

func writeTar(t *testing.T, build func(b *ociBuilder) *bytes.Reader) string {
	t.Helper()
	b := newOCI(t)
	file := filepath.Join(t.TempDir(), "oci.tar")
	data, err := io.ReadAll(build(b))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestRunReportsEachOutcomeWithItsExitCode(t *testing.T) {
	good := writeTar(t, func(b *ociBuilder) *bytes.Reader {
		return b.tar(b.index(b.image(goodSpec("amd64", elf.EM_X86_64)), b.image(goodSpec("arm64", elf.EM_AARCH64))))
	})
	partial := writeTar(t, func(b *ociBuilder) *bytes.Reader { return b.tar(b.image(goodSpec("amd64", elf.EM_X86_64))) })
	garbage := filepath.Join(t.TempDir(), "garbage.tar")
	if err := os.WriteFile(garbage, bytes.Repeat([]byte("z"), 600), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		args   []string
		code   int
		stdout string
		stderr string
	}{
		{"both platforms", []string{good}, 0, "linux/arm64 user=65532:65532 entrypoint=[/board] binary=EM_AARCH64", ""},
		{"one platform", []string{partial}, 1, "linux/amd64", "linux/arm64: no manifest"},
		{"unreadable tar", []string{garbage}, 1, "", "read the OCI tar"},
		{"missing file", []string{filepath.Join(t.TempDir(), "absent.tar")}, 1, "", "imagecheck:"},
		{"no argument", nil, 2, "", "usage"},
	}
	for _, tc := range cases {
		var stdout, stderr bytes.Buffer
		if code := run(tc.args, &stdout, &stderr); code != tc.code {
			t.Errorf("%s: exit %d, want %d (stderr %q)", tc.name, code, tc.code, stderr.String())
		}
		if !strings.Contains(stdout.String(), tc.stdout) || !strings.Contains(stderr.String(), tc.stderr) {
			t.Errorf("%s: stdout %q stderr %q, want %q and %q", tc.name, stdout.String(), stderr.String(), tc.stdout, tc.stderr)
		}
	}
}
