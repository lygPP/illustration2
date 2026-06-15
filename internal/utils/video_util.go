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
	"strconv"
	"strings"
	"time"
)

const defaultVideoTransitionSeconds = 1.0

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

	if err := ConcatVideosWithFade(ctx, inputVideos, outputPath, defaultVideoTransitionSeconds); err == nil {
		return nil
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

func ConcatVideosWithFade(ctx context.Context, inputVideos []string, outputPath string, transitionSeconds float64) error {
	if len(inputVideos) < 2 {
		return fmt.Errorf("at least 2 input videos required")
	}
	if strings.TrimSpace(outputPath) == "" {
		return fmt.Errorf("output path required")
	}
	if transitionSeconds <= 0 {
		transitionSeconds = defaultVideoTransitionSeconds
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}

	durations := make([]float64, len(inputVideos))
	args := []string{"-y"}
	for i, video := range inputVideos {
		if _, err := os.Stat(video); os.IsNotExist(err) {
			return fmt.Errorf("video file not found: %s", video)
		}
		duration, err := probeVideoDuration(ctx, video)
		if err != nil {
			return err
		}
		if duration <= transitionSeconds {
			return fmt.Errorf("video duration %.3fs must be greater than transition %.3fs: %s", duration, transitionSeconds, video)
		}
		durations[i] = duration
		args = append(args, "-i", video)
	}
	args = append(args, "-filter_complex", buildFadeFilter(len(inputVideos), durations, transitionSeconds))
	args = append(args,
		"-map", fmt.Sprintf("[v%d]", len(inputVideos)-1),
		"-map", fmt.Sprintf("[a%d]", len(inputVideos)-1),
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", "18",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-movflags", "+faststart",
		outputPath,
	)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg fade concat failed: %w, output: %s", err, string(output))
	}
	return nil
}

func buildFadeFilter(count int, durations []float64, transitionSeconds float64) string {
	var b strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintf(&b, "[%d:v]settb=AVTB,setpts=PTS-STARTPTS,format=yuv420p[v%d0];", i, i)
		fmt.Fprintf(&b, "[%d:a]asetpts=PTS-STARTPTS[a%d0];", i, i)
	}

	videoIn := "[v00]"
	audioIn := "[a00]"
	offset := durations[0] - transitionSeconds
	for i := 1; i < count; i++ {
		vOut := fmt.Sprintf("[v%d]", i)
		aOut := fmt.Sprintf("[a%d]", i)
		fmt.Fprintf(&b, "%s[v%d0]xfade=transition=fade:duration=%.3f:offset=%.3f%s;", videoIn, i, transitionSeconds, offset, vOut)
		fmt.Fprintf(&b, "%s[a%d0]acrossfade=d=%.3f:c1=tri:c2=tri%s;", audioIn, i, transitionSeconds, aOut)
		videoIn = vOut
		audioIn = aOut
		if i < count-1 {
			offset += durations[i] - transitionSeconds
		}
	}
	return b.String()
}

func probeVideoDuration(ctx context.Context, video string) (float64, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		video,
	)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe duration failed: %w", err)
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return 0, fmt.Errorf("parse video duration failed: %w", err)
	}
	return duration, nil
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

func SaveAudioFile(audio []byte, outputPath string) error {
	if len(audio) == 0 {
		return fmt.Errorf("audio data required")
	}
	if strings.TrimSpace(outputPath) == "" {
		return fmt.Errorf("output path required")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(outputPath, audio, 0644)
}

func MuxVideoWithAudioFromURL(ctx context.Context, videoURL, audioPath, outputPath string) error {
	if strings.TrimSpace(videoURL) == "" {
		return fmt.Errorf("video url required")
	}
	if strings.TrimSpace(audioPath) == "" {
		return fmt.Errorf("audio path required")
	}
	if strings.TrimSpace(outputPath) == "" {
		return fmt.Errorf("output path required")
	}
	if _, err := os.Stat(audioPath); os.IsNotExist(err) {
		return fmt.Errorf("audio file not found: %s", audioPath)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}

	timestamp := time.Now().Format("20060102_150405")
	randomNum := rand.Intn(10000)
	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("video_mux_%s_%04d", timestamp, randomNum))
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	localVideo := filepath.Join(tmpDir, "input.mp4")
	if err := downloadVideo(ctx, videoURL, localVideo); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",
		"-i", localVideo,
		"-i", audioPath,
		"-map", "0:v:0",
		"-map", "1:a:0",
		"-c:v", "copy",
		"-c:a", "aac",
		"-shortest",
		outputPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg mux failed: %w, output: %s", err, string(output))
	}
	return nil
}

func CreateSilentVideo(ctx context.Context, outputPath string, duration int) error {
	if strings.TrimSpace(outputPath) == "" {
		return fmt.Errorf("output path required")
	}
	if duration <= 0 {
		duration = 10
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",
		"-f", "lavfi",
		"-i", fmt.Sprintf("color=c=black:s=1280x720:d=%d", duration),
		"-pix_fmt", "yuv420p",
		outputPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg create silent video failed: %w, output: %s", err, string(output))
	}
	return nil
}

func ResourceURL(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	if strings.HasPrefix(clean, "resource/") {
		return "/" + clean
	}
	if clean == "resource" {
		return "/resource"
	}
	return "/" + clean
}

func downloadVideo(ctx context.Context, url, localPath string) error {
	if strings.HasPrefix(url, "/") {
		src := strings.TrimPrefix(url, "/")
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		f, err := os.Create(localPath)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(f, in)
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: time.Duration(envInt("VIDEO_DOWNLOAD_TIMEOUT_SECONDS", 900)) * time.Second}
	resp, err := client.Do(req)
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

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
