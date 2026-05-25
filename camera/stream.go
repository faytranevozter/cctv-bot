package camera

import (
	"regexp"
)

// MaskCredentials replaces user:password in URLs like rtsp://user:pass@host/path.
func MaskCredentials(raw string) string {
	re := regexp.MustCompile(`://[^:]+:[^@]+@`)
	return re.ReplaceAllString(raw, "://***:***@")
}
