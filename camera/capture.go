package camera

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Semaphore limits concurrent FFmpeg processes.
type Semaphore chan struct{}

func NewSemaphore(max int) Semaphore {
	return make(Semaphore, max)
}

func (s Semaphore) Acquire() {
	s <- struct{}{}
}

func (s Semaphore) Release() {
	<-s
}

// Capture grabs a single JPEG frame from an RTMP/RTSP stream using FFmpeg.
// Returns the path to the temp file containing the frame.
func Capture(ctx context.Context, streamURL, ffmpegBin string, timeoutSec int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	f, err := os.CreateTemp("", "cctv-snapshot-*.jpg")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	f.Close()

	cmd := exec.CommandContext(ctx, ffmpegBin,
		"-rtsp_transport", "tcp",
		"-i", streamURL,
		"-frames:v", "1",
		"-q:v", "2",
		f.Name(),
		"-y",
	)

	stderr, err := cmd.CombinedOutput()
	if err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("ffmpeg: %w: %s", err, string(stderr))
	}

	if fi, err := os.Stat(f.Name()); err != nil || fi.Size() == 0 {
		os.Remove(f.Name())
		return "", fmt.Errorf("captured file is empty")
	}

	return f.Name(), nil
}
