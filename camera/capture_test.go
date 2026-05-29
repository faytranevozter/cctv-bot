package camera

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureSucceedsWithFakeFFmpeg(t *testing.T) {
	record := filepath.Join(t.TempDir(), "args.txt")
	ffmpeg := fakeFFmpeg(t, `#!/bin/sh
printf '%s\n' "$@" > "`+record+`"
prev=""
for arg in "$@"; do out="$prev"; prev="$arg"; done
printf 'jpeg-data' > "$out"
`)

	path, err := Capture(context.Background(), "rtsp://example.com/stream", ffmpeg, 5)
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	t.Cleanup(func() { os.Remove(path) })
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read capture file: %v", err)
	}
	if string(data) != "jpeg-data" {
		t.Fatalf("capture data = %q, want jpeg-data", data)
	}
	args, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("read args record: %v", err)
	}
	if !strings.Contains(string(args), "-rtsp_transport\ntcp\n") {
		t.Fatalf("rtsp args = %q, want -rtsp_transport tcp", args)
	}
}

func TestCaptureOmitsRTSPTransportForNonRTSPStreams(t *testing.T) {
	record := filepath.Join(t.TempDir(), "args.txt")
	ffmpeg := fakeFFmpeg(t, `#!/bin/sh
printf '%s\n' "$@" > "`+record+`"
prev=""
for arg in "$@"; do out="$prev"; prev="$arg"; done
printf 'jpeg-data' > "$out"
`)

	path, err := Capture(context.Background(), "https://example.com/stream.m3u8", ffmpeg, 5)
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	t.Cleanup(func() { os.Remove(path) })
	args, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("read args record: %v", err)
	}
	if strings.Contains(string(args), "-rtsp_transport") {
		t.Fatalf("non-rtsp args = %q, should not include -rtsp_transport", args)
	}
}

func TestCaptureRemovesTempFileOnFFmpegFailure(t *testing.T) {
	ffmpeg := fakeFFmpeg(t, `#!/bin/sh
exit 7
`)

	path, err := Capture(context.Background(), "rtsp://example.com/stream", ffmpeg, 5)
	if err == nil || !strings.Contains(err.Error(), "ffmpeg:") {
		t.Fatalf("Capture() = %q, %v; want ffmpeg error", path, err)
	}
	if path != "" {
		t.Fatalf("path = %q, want empty on failure", path)
	}
}

func TestCaptureRejectsEmptyOutput(t *testing.T) {
	ffmpeg := fakeFFmpeg(t, `#!/bin/sh
prev=""
for arg in "$@"; do out="$prev"; prev="$arg"; done
: > "$out"
`)

	path, err := Capture(context.Background(), "rtsp://example.com/stream", ffmpeg, 5)
	if err == nil || !strings.Contains(err.Error(), "captured file is empty") {
		t.Fatalf("Capture() = %q, %v; want empty file error", path, err)
	}
	if path != "" {
		t.Fatalf("path = %q, want empty on failure", path)
	}
}

func TestCaptureTimesOut(t *testing.T) {
	ffmpeg := fakeFFmpeg(t, `#!/bin/sh
sleep 2
`)

	path, err := Capture(context.Background(), "rtsp://example.com/stream", ffmpeg, 1)
	if err == nil || !strings.Contains(err.Error(), "ffmpeg:") {
		t.Fatalf("Capture() = %q, %v; want timeout ffmpeg error", path, err)
	}
}

func fakeFFmpeg(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ffmpeg: %v", err)
	}
	return path
}
