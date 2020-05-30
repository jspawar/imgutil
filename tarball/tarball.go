package tarball

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"time"

	"github.com/buildpacks/imgutil"
	"github.com/buildpacks/imgutil/remote"
	"github.com/google/go-containerregistry/pkg/authn"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

type Image struct {
	baseImage   *remote.Image
	tarballPath string
}

func NewImage(repoName string, keychain authn.Keychain, tarballPath string, ops ...remote.ImageOption) (imgutil.Image, error) {
	i, err := remote.NewImage(repoName, keychain, ops...)
	if err != nil {
		return nil, err
	}

	baseImage, ok := i.(*remote.Image)
	if !ok {
		// TODO: handle this better
		panic("something wicked this way comes")
	}

	ti := &Image{
		baseImage:   baseImage,
		tarballPath: tarballPath,
	}

	return ti, nil
}

func (i *Image) Name() string {
	panic("not implemented") // TODO: Implement
}

func (i *Image) Rename(name string) {
	panic("not implemented") // TODO: Implement
}

func (i *Image) Label(_ string) (string, error) {
	panic("not implemented") // TODO: Implement
}

func (i *Image) SetLabel(key string, value string) error {
	return i.baseImage.SetLabel(key, value)
}

func (i *Image) Env(key string) (string, error) {
	panic("not implemented") // TODO: Implement
}

func (i *Image) SetEnv(_ string, _ string) error {
	panic("not implemented") // TODO: Implement
}

func (i *Image) SetEntrypoint(_ ...string) error {
	panic("not implemented") // TODO: Implement
}

func (i *Image) SetWorkingDir(_ string) error {
	panic("not implemented") // TODO: Implement
}

func (i *Image) SetCmd(_ ...string) error {
	panic("not implemented") // TODO: Implement
}

func (i *Image) Rebase(_ string, _ imgutil.Image) error {
	panic("not implemented") // TODO: Implement
}

func (i *Image) AddLayer(path string) error {
	return i.baseImage.AddLayer(path)
}

func (i *Image) AddLayerWithDiffID(path string, diffID string) error {
	panic("not implemented") // TODO: Implement
}

func (i *Image) ReuseLayer(diffID string) error {
	panic("not implemented") // TODO: Implement
}

// TopLayer returns the diff id for the top layer
func (i *Image) TopLayer() (string, error) {
	panic("not implemented") // TODO: Implement
}

// Save saves the image as `Name()` and any additional names provided to this method.
func (i *Image) Save(additionalNames ...string) error {
	// TODO: add the `CreatedAt` info to various parts of config file

	// 1. need an `index.json` file whose `manifests` key has one manifest item
	//   - this one manifest item should reference our lone manifest
	//   - `mediaType` needs to be set correctly
	// 2. need a manifest file which will be saved as a blob itself
	//   - should reference a single config file in `config` section
	//   - should reference all the various layers composing the image
	// 3. all of the following should be saved as blobs:
	//   - manifest file
	//   - config file
	//   - each of the layer tarballs
	// 4. put everything together as such: https://github.com/opencontainers/image-spec/blob/master/image-layout.md

	tarFile, err := os.Create(i.tarballPath)
	if err != nil {
		return err
	}
	tarball := tar.NewWriter(tarFile)

	// save blob of image config
	configDescriptor, err := i.writeConfigFileToTarball(tarball)
	if err != nil {
		return err
	}

	// save blob of manifest
	manifestDescriptor, err := i.writeManifestFileToTarball(tarball, configDescriptor)
	if err != nil {
		return err
	}

	// save index file
	indexManifest := &v1.IndexManifest{
		SchemaVersion: 2,
		Manifests:     []v1.Descriptor{*manifestDescriptor},
	}
	if err := i.writeIndexFileToTarball(tarball, indexManifest); err != nil {
		return err
	}

	// flush tarball contents to disk
	if err := tarball.Close(); err != nil {
		return err
	}

	// TODO: create `oci-layout` file

	return nil
}

func (i *Image) writeConfigFileToTarball(tw *tar.Writer) (*v1.Descriptor, error) {
	configFile, err := i.baseImage.ConfigFile()
	if err != nil {
		return nil, err
	}
	rawConfig, err := json.Marshal(&configFile)
	if err != nil {
		return nil, err
	}
	configDigest := sha256.Sum256(rawConfig)
	// TODO: check the bytes of the SHA sum aren't empty?
	hdr := &tar.Header{
		Name: fmt.Sprintf("blobs/sha256/%x", configDigest),
		Mode: 0644,
		Size: int64(len(rawConfig)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return nil, err
	}
	if _, err := tw.Write(rawConfig); err != nil {
		return nil, err
	}

	configDescriptor := &v1.Descriptor{
		MediaType: types.OCIConfigJSON,
		Digest: v1.Hash{
			Algorithm: "sha256",
			Hex:       fmt.Sprintf("%x", configDigest),
		},
		Size: int64(len(rawConfig)),
	}
	return configDescriptor, nil
}

func (i *Image) writeManifestFileToTarball(tw *tar.Writer, configDescriptor *v1.Descriptor) (*v1.Descriptor, error) {
	// write layers to tarball and construct layer descriptors
	layers, err := i.baseImage.Layers()
	if err != nil {
		return nil, err
	}
	layerDescriptors := []v1.Descriptor{}
	for _, layer := range layers {
		// construct layer descriptor
		mediaType, err := layer.MediaType()
		if err != nil {
			return nil, err
		}
		size, err := layer.Size()
		if err != nil {
			return nil, err
		}
		digest, err := layer.Digest()
		if err != nil {
			return nil, err
		}
		layerDescriptors = append(layerDescriptors, v1.Descriptor{
			MediaType: mediaType,
			Size:      size,
			Digest:    digest,
		})

		// add layer to tarball
		hdr := &tar.Header{
			Name: fmt.Sprintf("blobs/sha256/%s", digest.Hex),
			Mode: 0644,
			Size: size,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		// TODO: loading entire contents of layer into memory might not be a good idea...
		layerContents, err := layer.Compressed()
		if err != nil {
			return nil, err
		}
		rawLayer, err := ioutil.ReadAll(layerContents)
		if err != nil {
			return nil, err
		}
		if _, err := tw.Write(rawLayer); err != nil {
			return nil, err
		}
	}

	manifest := v1.Manifest{
		SchemaVersion: 2,
		Config:        *configDescriptor,
		Layers:        layerDescriptors,
	}
	rawManifest, err := json.Marshal(&manifest)
	if err != nil {
		return nil, err
	}

	manifestDigest := sha256.Sum256(rawManifest)
	hdr := &tar.Header{
		Name: fmt.Sprintf("blobs/sha256/%x", manifestDigest),
		Mode: 0644,
		Size: int64(len(rawManifest)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return nil, err
	}
	if _, err := tw.Write(rawManifest); err != nil {
		return nil, err
	}

	manifestDescriptor := &v1.Descriptor{
		MediaType: types.OCIManifestSchema1,
		Digest: v1.Hash{
			Algorithm: "sha256",
			Hex:       fmt.Sprintf("%x", manifestDigest),
		},
		Size: int64(len(rawManifest)),
	}

	return manifestDescriptor, nil
}

func (i *Image) writeIndexFileToTarball(tw *tar.Writer, indexManifest *v1.IndexManifest) error {
	rawIndexManifest, err := json.Marshal(indexManifest)
	if err != nil {
		return err
	}
	hdr := &tar.Header{
		Name: "index.json",
		Mode: 0644,
		Size: int64(len(rawIndexManifest)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(rawIndexManifest); err != nil {
		return err
	}
	return nil
}

// Found tells whether the image exists in the repository by `Name()`.
func (i *Image) Found() bool {
	panic("not implemented") // TODO: Implement
}

// GetLayer retrieves layer by diff id. Returns a reader of the uncompressed contents of the layer.
func (i *Image) GetLayer(diffID string) (io.ReadCloser, error) {
	return i.baseImage.GetLayer(diffID)
}

func (i *Image) Delete() error {
	panic("not implemented") // TODO: Implement
}

func (i *Image) CreatedAt() (time.Time, error) {
	panic("not implemented") // TODO: Implement
}

func (i *Image) Identifier() (imgutil.Identifier, error) {
	panic("not implemented") // TODO: Implement
}

func (i *Image) OS() (string, error) {
	return i.baseImage.OS()
}

func (i *Image) OSVersion() (string, error) {
	return i.baseImage.OSVersion()
}

func (i *Image) Architecture() (string, error) {
	return i.baseImage.Architecture()
}
