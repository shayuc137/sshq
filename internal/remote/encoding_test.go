package remote

import (
	"bytes"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestEncodingByName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"gbk", true},
		{"big5", true},
		{"shift-jis", true},
		{"euc-kr", true},
		{"gb18030", true},
		{"", false},
		{"utf-8", false},
		{"unknown", false},
	}
	for _, tt := range tests {
		got := EncodingByName(tt.name)
		if (got != nil) != tt.want {
			t.Errorf("EncodingByName(%q) nil=%v, want nil=%v", tt.name, got == nil, !tt.want)
		}
	}
}

func TestNewDecodingWriter(t *testing.T) {
	// GBK-encoded "你好" = 0xC4 0xE3 0xBA 0xC3
	gbkBytes := []byte{0xC4, 0xE3, 0xBA, 0xC3}
	expected := "你好"

	var buf bytes.Buffer
	w := NewDecodingWriter(&buf, "gbk")
	w.Write(gbkBytes)

	// flush transform writer
	if tw, ok := w.(interface{ Close() error }); ok {
		tw.Close()
	}

	if buf.String() != expected {
		t.Errorf("decoded = %q, want %q", buf.String(), expected)
	}
}

func TestNewDecodingWriterPassthrough(t *testing.T) {
	var buf bytes.Buffer
	w := NewDecodingWriter(&buf, "")
	w.Write([]byte("hello"))
	if buf.String() != "hello" {
		t.Errorf("passthrough failed: got %q", buf.String())
	}
}

func TestNeedsTranscoding(t *testing.T) {
	if NeedsTranscoding(nil) {
		t.Error("nil profile should not need transcoding")
	}
	if NeedsTranscoding(&Profile{OS: Linux}) {
		t.Error("linux profile without encoding should not need transcoding")
	}
	if !NeedsTranscoding(&Profile{OS: Windows, Encoding: "gbk"}) {
		t.Error("windows gbk profile should need transcoding")
	}
}

func TestGBKRoundtrip(t *testing.T) {
	original := "测试中文输出"
	gbkEncoded, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(original))

	var buf bytes.Buffer
	w := NewDecodingWriter(&buf, "gbk")
	w.Write(gbkEncoded)
	if tw, ok := w.(interface{ Close() error }); ok {
		tw.Close()
	}

	if buf.String() != original {
		t.Errorf("roundtrip = %q, want %q", buf.String(), original)
	}
}
