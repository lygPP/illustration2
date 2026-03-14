package utils

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func ConcatVideos(ctx context.Context, inputVideos []string, outputPath string) error {
	if len(inputVideos) < 2 {
		return fmt.Errorf("at least 2 input videos required")
	}

	if outputPath == "" {
		return fmt.Errorf("output path required")
	}

	for _, video := range inputVideos {
		if _, err := os.Stat(video); os.IsNotExist(err) {
			return fmt.Errorf("video file not found: %s", video)
		}
	}

	listFile, err := createConcatListFile(inputVideos)
	if err != nil {
		return err
	}
	defer os.RemoveAll(filepath.Dir(listFile))

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-f", "concat",
		"-safe", "0",
		"-i", listFile,
		"-c", "copy",
		outputPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg failed: %w, output: %s", err, string(output))
	}

	return nil
}

func createConcatListFile(videos []string) (string, error) {
	timestamp := time.Now().Format("20060102_150405")
	randomNum := rand.Intn(10000)
	dirName := fmt.Sprintf("video_concat_%s_%04d", timestamp, randomNum)
	tmpDir := filepath.Join(os.TempDir(), dirName)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", err
	}

	listFile := filepath.Join(tmpDir, "concat_list.txt")
	f, err := os.Create(listFile)
	if err != nil {
		return "", err
	}
	defer f.Close()

	for _, video := range videos {
		absPath, err := filepath.Abs(video)
		if err != nil {
			return "", err
		}
		_, err = fmt.Fprintf(f, "file '%s'\n", absPath)
		if err != nil {
			return "", err
		}
	}

	return listFile, nil
}

func ConcatVideosFromURLs(ctx context.Context, videoURLs []string, outputPath string) error {
	if len(videoURLs) < 2 {
		return fmt.Errorf("at least 2 video URLs required")
	}

	if outputPath == "" {
		return fmt.Errorf("output path required")
	}

	timestamp := time.Now().Format("20060102_150405")
	randomNum := rand.Intn(10000)
	dirName := fmt.Sprintf("video_urls_%s_%04d", timestamp, randomNum)
	tmpDir := filepath.Join(os.TempDir(), dirName)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	var localVideos []string
	for i, url := range videoURLs {
		localPath := filepath.Join(tmpDir, fmt.Sprintf("video_%d.mp4", i))
		if err := downloadVideo(ctx, url, localPath); err != nil {
			return err
		}
		localVideos = append(localVideos, localPath)
	}

	return ConcatVideos(ctx, localVideos, outputPath)
}

func downloadVideo(ctx context.Context, url, localPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed, status code: %d", resp.StatusCode)
	}

	f, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		return err
	}

	return nil
}
