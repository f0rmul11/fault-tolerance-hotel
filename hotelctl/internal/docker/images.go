package docker

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/project"
	archive "github.com/moby/go-archive"
	"github.com/moby/patternmatcher/ignorefile"
)

func (e *Engine) Build(ctx context.Context, root string, s config.Service, out io.Writer) (string, error) {
	dir := filepath.Join(root, s.Context)
	excludes := []string{}
	ignore, err := os.Open(filepath.Join(dir, ".dockerignore"))
	if err == nil {
		excludes, err = ignorefile.ReadAll(ignore)
		ignore.Close()
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	archiveReader, err := archive.TarWithOptions(dir, &archive.TarOptions{ExcludePatterns: excludes})
	if err != nil {
		return "", err
	}
	defer archiveReader.Close()
	tag := s.Image + ":local"
	result, err := e.client.ImageBuild(ctx, archiveReader, build.ImageBuildOptions{Tags: []string{tag}, Dockerfile: filepath.ToSlash(s.Dockerfile), Remove: true, Version: build.BuilderV1})
	if err != nil {
		return "", fmt.Errorf("build %s: %w", s.Name, err)
	}
	err = consumeProgress(result.Body, out)
	result.Body.Close()
	if err != nil {
		return "", err
	}
	info, err := e.client.ImageInspect(ctx, tag)
	if err != nil {
		return "", err
	}
	digest := strings.TrimPrefix(info.ID, "sha256:")
	if len(digest) < 16 {
		return "", fmt.Errorf("unexpected image ID %q", info.ID)
	}
	immutable := s.Image + ":sha-" + digest[:16]
	return immutable, e.client.ImageTag(ctx, tag, immutable)
}

func consumeProgress(reader io.Reader, out io.Writer) error {
	decoder := json.NewDecoder(reader)
	last := ""
	for {
		var event struct {
			Stream string `json:"stream"`
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		err := decoder.Decode(&event)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if event.Error != "" {
			return errors.New(event.Error)
		}
		if event.Stream != "" {
			fmt.Fprint(out, event.Stream)
		}
		if event.Status != "" && event.Status != last {
			fmt.Fprintln(out, event.Status)
			last = event.Status
		}
	}
}

func (e *Engine) Import(ctx context.Context, s *project.State, imageName, dir string, out io.Writer) error {
	stream, err := e.client.ImageSave(ctx, []string{imageName})
	if err != nil {
		return err
	}
	defer stream.Close()
	f, err := os.CreateTemp(dir, "image-*.tar")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	size, err := io.Copy(f, stream)
	if err != nil {
		return err
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		tw := tar.NewWriter(writer)
		err := tw.WriteHeader(&tar.Header{Name: "hotelctl-image.tar", Mode: 0600, Size: size})
		if err == nil {
			_, err = io.Copy(tw, f)
		}
		if closeErr := tw.Close(); err == nil {
			err = closeErr
		}
		_ = writer.CloseWithError(err)
		done <- err
	}()
	err = e.client.CopyToContainer(ctx, ContainerName(s), "/tmp", reader, container.CopyToContainerOptions{})
	_ = reader.CloseWithError(err)
	copyErr := <-done
	if err != nil {
		return err
	}
	if copyErr != nil {
		return copyErr
	}
	importErr := e.Exec(ctx, s, []string{"ctr", "-n", "k8s.io", "images", "import", "/tmp/hotelctl-image.tar"}, nil, out, out)
	cleanupErr := e.Exec(ctx, s, []string{"rm", "-f", "/tmp/hotelctl-image.tar"}, nil, io.Discard, io.Discard)
	return errors.Join(importErr, cleanupErr)
}

func (e *Engine) Exec(ctx context.Context, s *project.State, args []string, in io.Reader, out, errOut io.Writer) error {
	result, err := e.client.ContainerExecCreate(ctx, ContainerName(s), container.ExecOptions{Cmd: args, AttachStdin: in != nil, AttachStdout: true, AttachStderr: true})
	if err != nil {
		return err
	}
	attach, err := e.client.ContainerExecAttach(ctx, result.ID, container.ExecAttachOptions{})
	if err != nil {
		return err
	}
	defer attach.Close()
	if in != nil {
		go func() { _, _ = io.Copy(attach.Conn, in); _ = attach.CloseWrite() }()
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			attach.Close()
		case <-done:
		}
	}()
	if _, err = stdcopy.StdCopy(out, errOut, attach.Reader); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	status, err := e.client.ContainerExecInspect(ctx, result.ID)
	if err != nil {
		return err
	}
	if status.ExitCode != 0 {
		return fmt.Errorf("%s exited with code %d", args[0], status.ExitCode)
	}
	return nil
}

func demultiplex(ctx context.Context, reader io.ReadCloser, out, errOut io.Writer) error {
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			reader.Close()
		case <-done:
		}
	}()
	_, err := stdcopy.StdCopy(out, errOut, reader)
	return err
}
