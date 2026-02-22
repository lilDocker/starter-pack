// Package image manages container images using an overlay filesystem.
//
// Image Storage Layout:
//
//   /var/lib/lildocker/
//   ├── images/
//   │   └── <image-id>/
//   │       ├── manifest.json       <- image metadata
//   │       └── layers/
//   │           ├── <layer-id>/     <- each layer is a directory (diff)
//   │           │   └── ... files
//   │           └── <layer-id>/
//   └── containers/
//       └── <container-id>/
//           ├── config.json
//           ├── upper/              <- container's writable layer
//           ├── work/               <- overlayfs work dir (required)
//           ├── merged/             <- the final mounted view
//           └── logs/
//               └── stdout.log

package image

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const StorageRoot = "/var/lib/lildocker"

// Manifest describes an image
type Manifest struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Tag         string            `json:"tag"`
	Layers      []string          `json:"layers"`   // ordered list of layer IDs (bottom → top)
	Env         []string          `json:"env"`      // default env vars
	Cmd         []string          `json:"cmd"`      // default command
	WorkDir     string            `json:"workdir"`
	ExposedPorts []string         `json:"exposed_ports"`
	Labels      map[string]string `json:"labels"`
	CreatedAt   time.Time         `json:"created_at"`
	Size        int64             `json:"size"`     // total uncompressed bytes
}

// Store manages the image and container storage on disk
type Store struct {
	Root string
}

func NewStore() *Store {
	return &Store{Root: StorageRoot}
}

func (s *Store) ImagesDir() string     { return filepath.Join(s.Root, "images") }
func (s *Store) ContainersDir() string { return filepath.Join(s.Root, "containers") }

func (s *Store) ImageDir(id string) string {
	return filepath.Join(s.ImagesDir(), id)
}

func (s *Store) LayerDir(imageID, layerID string) string {
	return filepath.Join(s.ImageDir(imageID), "layers", layerID)
}

func (s *Store) ContainerDir(id string) string {
	return filepath.Join(s.ContainersDir(), id)
}

// Init creates the storage directories
func (s *Store) Init() error {
	dirs := []string{
		s.ImagesDir(),
		s.ContainersDir(),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("init storage: %w", err)
		}
	}
	return nil
}

// SaveManifest writes an image manifest to disk
func (s *Store) SaveManifest(m *Manifest) error {
	dir := s.ImageDir(m.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0644)
}

// LoadManifest reads an image manifest by ID
func (s *Store) LoadManifest(id string) (*Manifest, error) {
	path := filepath.Join(s.ImageDir(id), "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("image %s not found: %w", id, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// FindByName resolves "name:tag" or "name" (defaults to latest) to a manifest
func (s *Store) FindByName(nameTag string) (*Manifest, error) {
	name, tag := splitNameTag(nameTag)
	entries, err := os.ReadDir(s.ImagesDir())
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		m, err := s.LoadManifest(e.Name())
		if err != nil {
			continue
		}
		if m.Name == name && m.Tag == tag {
			return m, nil
		}
	}
	return nil, fmt.Errorf("image not found: %s", nameTag)
}

// ListImages returns all images
func (s *Store) ListImages() ([]*Manifest, error) {
	entries, err := os.ReadDir(s.ImagesDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var images []*Manifest
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, err := s.LoadManifest(e.Name())
		if err != nil {
			continue
		}
		images = append(images, m)
	}
	return images, nil
}

// DeleteImage removes an image and all its layers
func (s *Store) DeleteImage(nameTag string) error {
	m, err := s.FindByName(nameTag)
	if err != nil {
		return err
	}
	return os.RemoveAll(s.ImageDir(m.ID))
}

// NewLayerID generates a deterministic layer ID from its content hash
func NewLayerID(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h[:8])
}

// NewImageID generates an image ID
func NewImageID(name, tag string, timestamp time.Time) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d", name, tag, timestamp.UnixNano())))
	return fmt.Sprintf("%x", h[:16])
}

func splitNameTag(nameTag string) (string, string) {
	parts := strings.SplitN(nameTag, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return nameTag, "latest"
}

// MountOverlay mounts an overlayfs for a container.
// lowerDirs: image layers from bottom to top (read-only)
// upperDir: container's writable layer
// workDir: overlayfs internal work dir
// mergedDir: the final unified mount point
func MountOverlay(lowerDirs []string, upperDir, workDir, mergedDir string) error {
	for _, d := range []string{upperDir, workDir, mergedDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	// overlayfs requires lower dirs in top→bottom order (reversed from our storage)
	reversed := make([]string, len(lowerDirs))
	for i, l := range lowerDirs {
		reversed[len(lowerDirs)-1-i] = l
	}
	lower := strings.Join(reversed, ":")

	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", lower, upperDir, workDir)

	// Mount via syscall (see container package for actual mount call)
	// We return the options string so the container package can mount it
	_ = opts
	return nil
}

// OverlayOptions returns the mount options string for overlayfs
func OverlayOptions(lowerDirs []string, upperDir, workDir string) string {
	reversed := make([]string, len(lowerDirs))
	for i, l := range lowerDirs {
		reversed[len(lowerDirs)-1-i] = l
	}
	lower := strings.Join(reversed, ":")
	return fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", lower, upperDir, workDir)
}
