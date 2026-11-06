package filekind

import "testing"

func TestMediaSignatures(t *testing.T) {
	for _, tc := range []struct {
		name       string
		data       []byte
		kind, mime string
	}{
		{"a.mp4", []byte("\x00\x00\x00\x18ftypisom"), Video, "video/mp4"},
		{"a.m4a", []byte("\x00\x00\x00\x18ftypM4A "), Audio, "audio/mp4"},
		{"a.webm", []byte("\x1a\x45\xdf\xa3\x00webm"), Video, "video/webm"},
		{"a.mp3", []byte("ID3\x00"), Audio, "audio/mpeg"},
		{"a.aac", []byte{0xff, 0xf1, 0, 0}, Audio, "audio/aac"},
		{"a.wav", []byte("RIFF\x00\x00\x00\x00WAVE"), Audio, "audio/wav"},
		{"a.ogg", []byte("OggS\x00"), Audio, "audio/ogg"},
		{"a.FLAC", []byte("fLaC\x00"), Audio, "audio/flac"},
		{"fake.mp4", []byte("this is text"), "", ""},
		{"a.mkv", []byte("\x1a\x45\xdf\xa3\x00matroska"), "", ""},
	} {
		if got := Classify(tc.name, tc.data); got != tc.kind {
			t.Fatalf("%s: %s", tc.name, got)
		}
		if got := MediaMIME(tc.name, tc.data); got != tc.mime {
			t.Fatalf("%s mime: %s", tc.name, got)
		}
	}
}
