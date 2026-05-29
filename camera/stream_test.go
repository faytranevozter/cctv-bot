package camera

import "testing"

func TestMaskCredentials(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "rtsp credentials", raw: "rtsp://user:pass@example.com/stream", want: "rtsp://***:***@example.com/stream"},
		{name: "http credentials", raw: "http://user:secret@example.com/path", want: "http://***:***@example.com/path"},
		{name: "no credentials", raw: "rtsp://example.com/stream", want: "rtsp://example.com/stream"},
		{name: "username only", raw: "rtsp://user@example.com/stream", want: "rtsp://user@example.com/stream"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaskCredentials(tt.raw); got != tt.want {
				t.Fatalf("MaskCredentials(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
