package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"debug/elf"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// attestationKey is the annotation BuildKit puts on a provenance or SBOM
// manifest, which sits in the image index beside the real platform manifests.
const attestationKey = "vnd.docker.reference.type"

// wantPlatforms are the platforms SPEC 12.4 requires, with the ELF machine
// the binary in each must have: the proof that the binary was cross-compiled
// for that platform rather than copied from the build host.
var wantPlatforms = map[string]elf.Machine{
	"linux/amd64": elf.EM_X86_64,
	"linux/arm64": elf.EM_AARCH64,
}

type descriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Annotations map[string]string `json:"annotations"`
	Platform    *struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
	} `json:"platform"`
}

type manifestDoc struct {
	Manifests []descriptor `json:"manifests"`
	Config    descriptor   `json:"config"`
	Layers    []descriptor `json:"layers"`
}

type configDoc struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Config       struct {
		User       string   `json:"User"`
		Entrypoint []string `json:"Entrypoint"`
	} `json:"config"`
}

// Image is what one platform manifest of the OCI layout says about itself.
type Image struct {
	Platform   string
	User       string
	Entrypoint []string
	Machine    elf.Machine
}

// layout is an OCI image layout read into memory: path in the tar -> bytes.
type layout map[string][]byte

func readLayout(r io.Reader) (layout, error) {
	files := layout{}
	reader := tar.NewReader(r)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return files, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read the OCI tar: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", header.Name, err)
		}
		files[path.Clean(header.Name)] = data
	}
}

func (l layout) blob(digest string) ([]byte, error) {
	algorithm, hex, ok := strings.Cut(digest, ":")
	if !ok {
		return nil, fmt.Errorf("malformed digest %q", digest)
	}
	data, found := l[path.Join("blobs", algorithm, hex)]
	if !found {
		return nil, fmt.Errorf("blob %s is not in the layout", digest)
	}
	return data, nil
}

func (l layout) manifest(digest string) (manifestDoc, error) {
	var doc manifestDoc
	data, err := l.blob(digest)
	if err != nil {
		return doc, err
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return doc, fmt.Errorf("parse %s: %w", digest, err)
	}
	return doc, nil
}

// Inspect reads an OCI layout tar and returns one Image per platform
// manifest, following nested indexes and skipping attestation manifests.
func Inspect(r io.Reader) ([]Image, error) {
	files, err := readLayout(r)
	if err != nil {
		return nil, err
	}
	var root manifestDoc
	if err := json.Unmarshal(files["index.json"], &root); err != nil {
		return nil, fmt.Errorf("parse index.json: %w", err)
	}
	return files.images(root.Manifests)
}

func (l layout) images(entries []descriptor) ([]Image, error) {
	var out []Image
	for _, entry := range entries {
		if _, attestation := entry.Annotations[attestationKey]; attestation {
			continue
		}
		doc, err := l.manifest(entry.Digest)
		if err != nil {
			return nil, err
		}
		if len(doc.Manifests) > 0 {
			nested, err := l.images(doc.Manifests)
			if err != nil {
				return nil, err
			}
			out = append(out, nested...)
			continue
		}
		image, err := l.image(doc)
		if err != nil {
			return nil, err
		}
		out = append(out, image)
	}
	return out, nil
}

func (l layout) image(doc manifestDoc) (Image, error) {
	var image Image
	raw, err := l.blob(doc.Config.Digest)
	if err != nil {
		return image, err
	}
	var config configDoc
	if err := json.Unmarshal(raw, &config); err != nil {
		return image, fmt.Errorf("parse the image config: %w", err)
	}
	image.Platform = config.OS + "/" + config.Architecture
	image.User = config.Config.User
	image.Entrypoint = config.Config.Entrypoint
	image.Machine, err = l.binaryMachine(doc.Layers)
	if err != nil {
		return image, fmt.Errorf("%s: %w", image.Platform, err)
	}
	return image, nil
}

// binaryMachine returns the ELF machine of /board in the last layer that
// carries it.
func (l layout) binaryMachine(layers []descriptor) (elf.Machine, error) {
	machine := elf.EM_NONE
	for _, layer := range layers {
		data, err := l.blob(layer.Digest)
		if err != nil {
			return machine, err
		}
		binary, err := fileInLayer(data, "board")
		if err != nil {
			return machine, err
		}
		if binary == nil {
			continue
		}
		parsed, err := elf.NewFile(bytes.NewReader(binary))
		if err != nil {
			return machine, fmt.Errorf("/board is not an ELF binary: %w", err)
		}
		machine = parsed.Machine
	}
	if machine == elf.EM_NONE {
		return machine, errors.New("no /board binary in any layer")
	}
	return machine, nil
}

// fileInLayer returns the bytes of name in a (possibly gzipped) layer tar, or
// nil when the layer does not hold it.
func fileInLayer(layer []byte, name string) ([]byte, error) {
	var source io.Reader = bytes.NewReader(layer)
	if bytes.HasPrefix(layer, []byte{0x1f, 0x8b}) {
		zipped, err := gzip.NewReader(source)
		if err != nil {
			return nil, fmt.Errorf("open a gzipped layer: %w", err)
		}
		source = zipped
	}
	reader := tar.NewReader(source)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read a layer: %w", err)
		}
		if header.Typeflag == tar.TypeReg && strings.TrimPrefix(path.Clean(header.Name), "/") == name {
			return io.ReadAll(reader)
		}
	}
}

// Verify returns one message per way the images fall short of SPEC 12.4:
// a missing platform, a root user, a wrong entrypoint, a binary built for
// another architecture.
func Verify(images []Image) []string {
	var problems []string
	byPlatform := map[string]Image{}
	for _, image := range images {
		byPlatform[image.Platform] = image
	}
	for platform, machine := range wantPlatforms {
		image, ok := byPlatform[platform]
		if !ok {
			problems = append(problems, platform+": no manifest for this platform")
			continue
		}
		problems = append(problems, imageProblems(image, machine)...)
	}
	return problems
}

func imageProblems(image Image, machine elf.Machine) []string {
	var problems []string
	if isRoot(image.User) {
		problems = append(problems, fmt.Sprintf("%s: runs as root (User %q)", image.Platform, image.User))
	}
	if len(image.Entrypoint) == 0 || image.Entrypoint[0] != "/board" {
		problems = append(problems, fmt.Sprintf("%s: entrypoint is %q, want /board", image.Platform, image.Entrypoint))
	}
	if image.Machine != machine {
		problems = append(problems, fmt.Sprintf("%s: /board is %s, want %s: it was not cross-compiled for this platform", image.Platform, image.Machine, machine))
	}
	return problems
}

func isRoot(user string) bool {
	name, _, _ := strings.Cut(user, ":")
	return name == "" || name == "0" || name == "root"
}
