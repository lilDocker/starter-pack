package runtime

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"os/exec"
)

func RunContainer(image string) error {

	imagePath := "images/" + image + ".tar"
	containerDir := "containers/" + image

	os.MkdirAll(containerDir, os.ModePerm)

	file, err := os.Open(imagePath)
	if err != nil {
		return err
	}
	defer file.Close()

	tr := tar.NewReader(file)

	for {

		header, err := tr.Next()

		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := containerDir + "/" + header.Name

		out, err := os.Create(target)
		if err != nil {
			return err
		}

		io.Copy(out, tr)
		out.Close()
	}

	fmt.Println("Container extracted to:", containerDir)

	// Read CMD metadata
	cmdData, err := os.ReadFile("images/" + image + ".cmd")
	if err != nil {
		return err
	}

	command := string(cmdData)

	fmt.Println("Running CMD:", command)

	cmd := exec.Command("cmd", "/C", command)
	cmd.Dir = containerDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}