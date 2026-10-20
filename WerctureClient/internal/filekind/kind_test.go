package filekind

import "testing"

func TestClassify(t *testing.T) {
	if Classify("a.pdf", []byte("%PDF-1.4\n")) != PDF {
		t.Fatal("pdf")
	}
	if Classify("a.md", []byte("%PDF-1.4\n")) != PDF {
		t.Fatal("pdf named md")
	}
	if Classify("note.md", []byte("# Hi\n")) != Markdown {
		t.Fatal("md")
	}
	if Classify("note.txt", []byte("# Hi\n")) != "" {
		t.Fatal("txt")
	}
	if Classify("photo.jpg", []byte{0xff, 0xd8, 0xff, 0x00}) != Image {
		t.Fatal("jpeg")
	}
	if Classify("empty.md", []byte("  \n")) != "" {
		t.Fatal("empty md")
	}
}
