package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/buildpacks/imgutil/layer"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/paketo-buildpacks/packit/cargo"
	"io"
	"log"
	"os"
	"strings"
)

var buildpackTomlContent = strings.TrimSpace(`
api = "0.2"

[buildpack]
id = "hybrid"
version = "0.0.1"
name = "Hybrid OS Buildpack"

[[stacks]]
id = "io.buildpacks.samples.stacks.nanoserver-1809"

[[stacks]]
id = "io.buildpacks.samples.stacks.alpine"
`)

func main() {
	imageName := flag.String("ref", "", "image ref")
	publish := flag.Bool("publish", false, "publish to registry")
	flag.Parse()

	if *imageName == "" {
		flag.Usage()
		os.Exit(1)
	}

	if err := run(*imageName, *publish); err != nil {
		log.Fatal(err)
	}

	fmt.Println("image and layer written")
}

var detectBatContent = strings.TrimSpace(`
exit 0
`)

var buildBatContent = strings.TrimSpace(`
@echo off
echo Hello windows
exit 0
`)

var detectShContent = strings.TrimSpace(`
#!/bin/sh
exit 0
`)

var buildShContent = strings.TrimSpace(`
#!/bin/sh
echo Hello linux
exit 0
`)

const windowsReadSecurityDescriptor = "AQAAgBQAAAAoAAAAAAAAAAAAAAABAwAAAAAABV0AAAACAAAAAQAAAAEDAAAAAAAFXQAAAAIAAAABAAAA"
const linuxReadMode = 0777

func run(imageName string, publish bool) error {
	hybridLayerBlob := &bytes.Buffer{}

	hybridWriter := tar.NewWriter(hybridLayerBlob)

	// write standard windows root directories (require linux permissions)
	// - needs only Linux permissions as Windows doesn't check grandparent permissions
	if err := hybridWriter.WriteHeader(&tar.Header{
		Name:     "Files",
		Typeflag: tar.TypeDir,
		Mode:     linuxReadMode,
	}); err != nil {
		return err
	}
	if err := hybridWriter.WriteHeader(&tar.Header{
		Name:     "Hives",
		Typeflag: tar.TypeDir,
	}); err != nil {
		return err
	}

	// write buildpack parent-directory hierarchy
	// - needs only Linux permissions as Windows doesn't check grandparent permissions
	if err := hybridWriter.WriteHeader(&tar.Header{
		Name:     "Files/cnb",
		Typeflag: tar.TypeDir,
		Mode:     linuxReadMode,
	}); err != nil {
		return err
	}
	if err := hybridWriter.WriteHeader(&tar.Header{
		Name:     "Files/cnb/buildpacks",
		Typeflag: tar.TypeDir,
		Mode:     linuxReadMode,
	}); err != nil {
		return err
	}
	if err := hybridWriter.WriteHeader(&tar.Header{
		Name:     "Files/cnb/buildpacks/hybrid",
		Typeflag: tar.TypeDir,
		Mode:     linuxReadMode,
	}); err != nil {
		return err
	}
	if err := hybridWriter.WriteHeader(&tar.Header{
		Name:     "Files/cnb/buildpacks/hybrid/0.0.1",
		Typeflag: tar.TypeDir,
		Mode:     linuxReadMode,
	}); err != nil {
		return err
	}

	// write buildpack.toml
	// - needs Windows & Linux permissions
	if err := hybridWriter.WriteHeader(&tar.Header{
		Name:     "Files/cnb/buildpacks/hybrid/0.0.1/buildpack.toml",
		Typeflag: tar.TypeReg,
		Size:     int64(len(buildpackTomlContent)),
		Mode:     linuxReadMode,
		PAXRecords: map[string]string{
			"MSWINDOWS.rawsd": windowsReadSecurityDescriptor,
		},
		Format: tar.FormatPAX,
	}); err != nil {
		return err
	}
	if _, err := hybridWriter.Write([]byte(buildpackTomlContent)); err != nil {
		return err
	}

	// write Windows buildpack binary dir
	// - needs Windows & Linux permissions
	if err := hybridWriter.WriteHeader(&tar.Header{
		Name:     "Files/cnb/buildpacks/hybrid/0.0.1/bin",
		Typeflag: tar.TypeDir,
		Mode:     777,
		PAXRecords: map[string]string{
			"MSWINDOWS.rawsd": windowsReadSecurityDescriptor,
		},
		Format: tar.FormatPAX,
	}); err != nil {
		return err
	}

	// write Windows buildpack binaries
	// - needs only Windows permissions
	if err := hybridWriter.WriteHeader(&tar.Header{
		Name:     "Files/cnb/buildpacks/hybrid/0.0.1/bin/detect.bat",
		Typeflag: tar.TypeReg,
		Size:     int64(len(detectBatContent)),
		PAXRecords: map[string]string{
			"MSWINDOWS.rawsd": windowsReadSecurityDescriptor,
		},
		Format: tar.FormatPAX,
	}); err != nil {
		return err
	}
	if _, err := hybridWriter.Write([]byte(detectBatContent)); err != nil {
		return err
	}
	if err := hybridWriter.WriteHeader(&tar.Header{
		Name:     "Files/cnb/buildpacks/hybrid/0.0.1/bin/build.bat",
		Typeflag: tar.TypeReg,
		Size:     int64(len(buildBatContent)),
		PAXRecords: map[string]string{
			"MSWINDOWS.rawsd": windowsReadSecurityDescriptor,
		},
		Format: tar.FormatPAX,
	}); err != nil {
		return err
	}
	if _, err := hybridWriter.Write([]byte(buildBatContent)); err != nil {
		return err
	}

	// write Linux buildpack binaries
	// - needs only Linux permissions
	if err := hybridWriter.WriteHeader(&tar.Header{
		Name:     "Files/cnb/buildpacks/hybrid/0.0.1/bin/detect",
		Typeflag: tar.TypeReg,
		Size:     int64(len(detectShContent)),
		Mode:     linuxReadMode,
	}); err != nil {
		return err
	}
	if _, err := hybridWriter.Write([]byte(detectShContent)); err != nil {
		return err
	}

	if err := hybridWriter.WriteHeader(&tar.Header{
		Name:     "Files/cnb/buildpacks/hybrid/0.0.1/bin/build",
		Typeflag: tar.TypeReg,
		Size:     int64(len(buildShContent)),
		Mode:     linuxReadMode,
	}); err != nil {
		return err
	}
	if _, err := hybridWriter.Write([]byte(buildShContent)); err != nil {
		return err
	}

	// write Linux parent-directory hierarchy
	// - note relative paths (no leading /)
	if err := hybridWriter.WriteHeader(&tar.Header{
		Name:     "cnb",
		Typeflag: tar.TypeDir,
		Mode:     linuxReadMode,
	}); err != nil {
		return err
	}
	if err := hybridWriter.WriteHeader(&tar.Header{
		Name:     "cnb/buildpacks",
		Typeflag: tar.TypeDir,
		Mode:     linuxReadMode,
	}); err != nil {
		return err
	}
	if err := hybridWriter.WriteHeader(&tar.Header{
		Name:     "cnb/buildpacks/hybrid",
		Typeflag: tar.TypeDir,
		Mode:     linuxReadMode,
	}); err != nil {
		return err
	}

	// symlink Linux buildpack directory to absolute Windows buildpack directory
	// - note leading / in Linkname
	if err := hybridWriter.WriteHeader(&tar.Header{
		Name:     "cnb/buildpacks/hybrid/0.0.1",
		Linkname: "/Files/cnb/buildpacks/hybrid/0.0.1",
		Typeflag: tar.TypeSymlink,
		Mode:     linuxReadMode,
	}); err != nil {
		return err
	}

	// generate image LABEL layer metadata
	buildpackConfig := &cargo.Config{}
	if err := cargo.DecodeConfig(bytes.NewBufferString(buildpackTomlContent), buildpackConfig); err != nil {
		return err
	}

	sum := sha256.Sum256(hybridLayerBlob.Bytes())
	shasum := fmt.Sprintf("sha256:%x", sum)

	layerConfig := map[string]map[string]struct {
		Api         string      `json:"api"`
		Stacks      interface{} `json:"stacks"`
		LayerDiffID string      `json:"layerDiffID"`
	}{
		buildpackConfig.Buildpack.ID: {
			buildpackConfig.Buildpack.Version: {
				buildpackConfig.API,
				buildpackConfig.Stacks,
				shasum,
			},
		},
	}

	layerMetadata := struct {
		ID      string      `json:"id"`
		Version string      `json:"version"`
		Stacks  interface{} `json:"stacks"`
	}{
		buildpackConfig.Buildpack.ID,
		buildpackConfig.Buildpack.Version,
		buildpackConfig.Stacks,
	}

	layerConfigJSON, err := json.Marshal(layerConfig)
	if err != nil {
		return err
	}
	layerMetadataJSON, err := json.Marshal(layerMetadata)
	if err != nil {
		return err
	}

	// initialize image with config
	image, err := mutate.ConfigFile(empty.Image, &v1.ConfigFile{
		Config: v1.Config{
			Labels: map[string]string{
				"io.buildpacks.buildpack.layers":      string(layerConfigJSON),
				"io.buildpacks.buildpackage.metadata": string(layerMetadataJSON),
			},
		},
	})
	if err != nil {
		return err
	}

	// generate Windows-scratch base layer
	windowsBaseLayerReader, err := layer.WindowsBaseLayer()
	if err != nil {
		return err
	}

	windowsScratchBaseLayer, err := tarball.LayerFromReader(windowsBaseLayerReader)
	if err != nil {
		return err
	}

	// generate hybrid layer
	hybridLayer, err := tarball.LayerFromReader(hybridLayerBlob)
	if err != nil {
		return err
	}

	image, err = mutate.AppendLayers(image, windowsScratchBaseLayer, hybridLayer)
	if err != nil {
		return err
	}

	tag, err := name.NewTag(imageName)
	if err != nil {
		return err
	}

	if publish {
		if err := remote.Write(tag, image, remote.WithAuthFromKeychain(authn.DefaultKeychain)); err != nil {
			return err
		}
	} else {
		fmt.Println("writing new image")
		output, err := daemon.Write(tag, image)
		if err != nil {
			return err
		}
		if _, err := io.Copy(os.Stderr, bytes.NewBuffer([]byte(output))); err != nil {
			return err
		}
	}

	return nil
}
