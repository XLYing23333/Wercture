package filekind

import (
	"bytes"
	"path"
	"strings"
)

const Video = "video"
const Audio = "audio"

// MediaMIME performs a bounded signature check. The server validates full streams.
func MediaMIME(name string, data []byte) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".mp4", ".m4a":
		if len(data) >= 12 && bytes.Equal(data[4:8], []byte("ftyp")) {
			if strings.EqualFold(path.Ext(name), ".m4a") {
				return "audio/mp4"
			}
			return "video/mp4"
		}
	case ".webm":
		if bytes.HasPrefix(data, []byte{0x1a, 0x45, 0xdf, 0xa3}) && bytes.Contains(data, []byte("webm")) {
			return "video/webm"
		}
	case ".mp3":
		if bytes.HasPrefix(data, []byte("ID3")) || (len(data) > 1 && data[0] == 0xff && data[1]&0xe0 == 0xe0 && data[1]&0x18 != 0x08 && data[1]&0x06 != 0) {
			return "audio/mpeg"
		}
	case ".aac":
		if len(data) > 1 && data[0] == 0xff && data[1]&0xf6 == 0xf0 {
			return "audio/aac"
		}
	case ".wav":
		if len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WAVE")) {
			return "audio/wav"
		}
	case ".ogg":
		if bytes.HasPrefix(data, []byte("OggS")) {
			return "audio/ogg"
		}
	case ".flac":
		if bytes.HasPrefix(data, []byte("fLaC")) {
			return "audio/flac"
		}
	}
	return ""
}
