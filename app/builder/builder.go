package builder

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"lildocker/parser"
)

func BuildImage(srcDir string, imageName string) error {

	os.MkdirAll("images", os.ModePerm)

	// Parse lilDockfile
	instructions, err := parser.ParseLilDockfile(filepath.Join(srcDir, "lilDockfile"))
	if err != nil {
		return err
	}

	var cmdValue string

	for _, inst := range instructions {
		if inst.Command == "CMD" {
			cmdValue = inst.Value
		}
	}

	if cmdValue == "" {
		return fmt.Errorf("CMD not found in lilDockfile")
	}

	// Save CMD metadata
	err = os.WriteFile("images/"+imageName+".cmd", []byte(cmdValue), 0644)
	if err != nil {
		return err
	}

	// Create TAR image
	imagePath := "images/" + imageName + ".tar"

	file, err := os.Create(imagePath)
	if err != nil {
		return err
	}
	defer file.Close()

	tw := tar.NewWriter(file)
	defer tw.Close()

	err = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {

		if info.IsDir() {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		relPath, _ := filepath.Rel(srcDir, path)
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(tw, f)
		return err
	})

	if err != nil {
		return err
	}

	fmt.Println("Image built:", imagePath)
	return nil
}