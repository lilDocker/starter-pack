// Package builder executes lilDockfile instructions to produce a layered image.
package builder

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/you/lildocker/internal/image"
	"github.com/you/lildocker/internal/parser"
)

// Builder executes a parsed Dockerfile and produces a layered image
type Builder struct {
	store       *image.Store
	contextPath string // build context directory (where lilDockfile lives)
	name        string
	tag         string
	verbose     bool
	buildArgs   map[string]string

	// accumulated image state
	manifest *image.Manifest
	layers   []string // layer directory paths (bottom → top)
}

func New(store *image.Store, contextPath, name, tag string, verbose bool) *Builder {
	return &Builder{
		store:       store,
		contextPath: contextPath,
		name:        name,
		tag:         tag,
		verbose:     verbose,
		buildArgs:   map[string]string{},
	}
}

// Build executes all instructions in the parsed Dockerfile
func (b *Builder) Build(df *parser.Dockerfile) (*image.Manifest, error) {
	b.manifest = &image.Manifest{
		Name:      b.name,
		Tag:       b.tag,
		CreatedAt: time.Now(),
		Labels:    map[string]string{},
	}

	fmt.Printf("🔨 Building %s:%s from %s\n", b.name, b.tag, b.contextPath)

	for i, instr := range df.Instructions {
		fmt.Printf("  Step %d/%d : %s %s\n", i+1, len(df.Instructions), instr.Type, instr.Raw)

		if err := b.execInstruction(instr); err != nil {
			return nil, fmt.Errorf("step %d (%s): %w", i+1, instr.Type, err)
		}
	}

	// Finalize image ID
	b.manifest.ID = image.NewImageID(b.name, b.tag, b.manifest.CreatedAt)
	b.manifest.Layers = b.layerIDs()

	// Calculate total image size
	b.manifest.Size = b.calculateSize()

	// Save the manifest
	if err := b.store.SaveManifest(b.manifest); err != nil {
		return nil, fmt.Errorf("save manifest: %w", err)
	}

	fmt.Printf("✅ Successfully built %s (%s:%s)\n", b.manifest.ID[:12], b.name, b.tag)
	return b.manifest, nil
}

// execInstruction dispatches a single Dockerfile instruction
func (b *Builder) execInstruction(instr parser.Instruction) error {
	switch instr.Type {
	case parser.FROM:
		return b.execFROM(instr)
	case parser.WORKDIR:
		return b.execWORKDIR(instr)
	case parser.COPY:
		return b.execCOPY(instr)
	case parser.ENV:
		return b.execENV(instr)
	case parser.RUN:
		return b.execRUN(instr)
	case parser.EXPOSE:
		return b.execEXPOSE(instr)
	case parser.CMD:
		return b.execCMD(instr)
	case parser.LABEL:
		return b.execLABEL(instr)
	case parser.ARG:
		return b.execARG(instr)
	case parser.VOLUME:
		return b.execVOLUME(instr)
	case parser.USER:
		// Stored in manifest, applied at container start
		b.manifest.Labels["user"] = instr.Raw
		return nil
	default:
		return fmt.Errorf("unsupported instruction: %s", instr.Type)
	}
}

// execFROM sets the base image layer
func (b *Builder) execFROM(instr parser.Instruction) error {
	if len(instr.Args) < 1 {
		return fmt.Errorf("FROM requires an argument")
	}
	base := instr.Args[0]

	if base == "scratch" {
		// scratch = empty base, start fresh
		layerDir, err := b.newLayer("scratch")
		if err != nil {
			return err
		}
		b.layers = append(b.layers, layerDir)
		fmt.Println("    → Using scratch (empty) base")
		return nil
	}

	// Check if base image is already in our store
	baseManifest, err := b.store.FindByName(base)
	if err != nil {
		// Not found locally — try to bootstrap from a tar or fail
		return fmt.Errorf("base image %q not found locally. Run: lildocker pull %s", base, base)
	}

	// Reuse base image layers (copy their paths in)
	imageDir := b.store.ImageDir(baseManifest.ID)
	for _, layerID := range baseManifest.Layers {
		layerPath := filepath.Join(imageDir, "layers", layerID)
		b.layers = append(b.layers, layerPath)
	}

	// Inherit base image settings (overrideable by later instructions)
	b.manifest.Env = append(b.manifest.Env, baseManifest.Env...)
	if len(b.manifest.Cmd) == 0 {
		b.manifest.Cmd = baseManifest.Cmd
	}
	if b.manifest.WorkDir == "" {
		b.manifest.WorkDir = baseManifest.WorkDir
	}

	fmt.Printf("    → Using base image: %s (id: %s)\n", base, baseManifest.ID[:12])
	return nil
}

// execRUN runs a shell command in a temporary container rooted at current layers
func (b *Builder) execRUN(instr parser.Instruction) error {
	if len(instr.Args) == 0 {
		return fmt.Errorf("RUN requires a command")
	}

	// Create a new layer directory for the changes from this RUN
	layerID := image.NewLayerID(fmt.Sprintf("RUN:%s", instr.Raw))
	layerDir, err := b.newLayerWithID(layerID)
	if err != nil {
		return err
	}

	// Mount overlayfs: all previous layers (read-only) + new layer (writable)
	mergedDir := layerDir + "_merged"
	workDir := layerDir + "_work"

	if err := os.MkdirAll(mergedDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return err
	}

	opts := image.OverlayOptions(b.layers, layerDir, workDir)
	mountCmd := exec.Command("mount", "-t", "overlay", "overlay", "-o", opts, mergedDir)
	mountCmd.Stdout = os.Stdout
	mountCmd.Stderr = os.Stderr
	if err := mountCmd.Run(); err != nil {
		return fmt.Errorf("mount overlay for RUN: %w", err)
	}
	defer exec.Command("umount", mergedDir).Run()

	// Run the command inside the merged view with chroot
	shellCmd := strings.Join(instr.Args, " ")
	cmd := exec.Command("chroot", mergedDir, "/bin/sh", "-c", shellCmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = b.buildEnv()

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("RUN command failed: %w", err)
	}

	// The layer now contains the diff (overlayfs writes changes to upperDir = layerDir)
	b.layers = append(b.layers, layerDir)
	return nil
}

// execCOPY copies files from the build context into a new layer
func (b *Builder) execCOPY(instr parser.Instruction) error {
	if len(instr.Args) < 2 {
		return fmt.Errorf("COPY requires <src> <dst>")
	}

	src := filepath.Join(b.contextPath, instr.Args[0])
	dst := instr.Args[1]

	layerID := image.NewLayerID(fmt.Sprintf("COPY:%s:%s", src, dst))
	layerDir, err := b.newLayerWithID(layerID)
	if err != nil {
		return err
	}

	destPath := filepath.Join(layerDir, dst)
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	if err := copyPath(src, destPath); err != nil {
		return fmt.Errorf("COPY %s → %s: %w", src, dst, err)
	}

	b.layers = append(b.layers, layerDir)
	fmt.Printf("    → Copied %s → %s\n", instr.Args[0], dst)
	return nil
}

// execWORKDIR sets the working directory and creates it
func (b *Builder) execWORKDIR(instr parser.Instruction) error {
	if len(instr.Args) == 0 {
		return fmt.Errorf("WORKDIR requires a path")
	}
	b.manifest.WorkDir = instr.Args[0]

	// Create the directory in a new layer
	layerDir, err := b.newLayer(fmt.Sprintf("WORKDIR:%s", instr.Args[0]))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(layerDir, instr.Args[0]), 0755); err != nil {
		return err
	}
	b.layers = append(b.layers, layerDir)
	return nil
}

// execENV sets environment variables in the manifest
func (b *Builder) execENV(instr parser.Instruction) error {
	b.manifest.Env = append(b.manifest.Env, instr.Args...)
	return nil
}

// execEXPOSE records exposed ports in the manifest
func (b *Builder) execEXPOSE(instr parser.Instruction) error {
	b.manifest.ExposedPorts = append(b.manifest.ExposedPorts, instr.Args...)
	return nil
}

// execCMD sets the default command
func (b *Builder) execCMD(instr parser.Instruction) error {
	b.manifest.Cmd = instr.Args
	return nil
}

// execLABEL adds labels to the manifest
func (b *Builder) execLABEL(instr parser.Instruction) error {
	for _, kv := range instr.Args {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			b.manifest.Labels[parts[0]] = parts[1]
		}
	}
	return nil
}

// execARG defines a build-time argument (can be overridden by --build-arg)
func (b *Builder) execARG(instr parser.Instruction) error {
	if len(instr.Args) == 0 {
		return nil
	}
	parts := strings.SplitN(instr.Args[0], "=", 2)
	name := parts[0]
	if _, ok := b.buildArgs[name]; !ok && len(parts) == 2 {
		b.buildArgs[name] = parts[1]
	}
	return nil
}

// execVOLUME records volume mount points
func (b *Builder) execVOLUME(instr parser.Instruction) error {
	// Just store the volume declarations; actual mounting happens at run time
	for _, vol := range instr.Args {
		b.manifest.Labels["volume:"+vol] = "true"
	}
	return nil
}

// --- helpers ---

func (b *Builder) newLayer(reason string) (string, error) {
	id := image.NewLayerID(reason + fmt.Sprintf(":%d", time.Now().UnixNano()))
	return b.newLayerWithID(id)
}

func (b *Builder) newLayerWithID(id string) (string, error) {
	// Layers go into a temp path during build; we'll relocate them on image save
	layerDir := filepath.Join(b.store.ImagesDir(), "_building", id)
	if err := os.MkdirAll(layerDir, 0755); err != nil {
		return "", fmt.Errorf("create layer dir: %w", err)
	}
	return layerDir, nil
}

func (b *Builder) layerIDs() []string {
	var ids []string
	for _, l := range b.layers {
		ids = append(ids, filepath.Base(l))
	}
	return ids
}

func (b *Builder) buildEnv() []string {
	env := os.Environ()
	for _, kv := range b.manifest.Env {
		env = append(env, kv)
	}
	return env
}

func (b *Builder) calculateSize() int64 {
	var total int64
	for _, l := range b.layers {
		filepath.Walk(l, func(_ string, fi os.FileInfo, _ error) error {
			if fi != nil && !fi.IsDir() {
				total += fi.Size()
			}
			return nil
		})
	}
	return total
}

// copyPath copies a file or directory recursively
func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, _ := in.Stat()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
