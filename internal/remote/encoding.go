package remote

import (
	"io"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

func EncodingByName(name string) encoding.Encoding {
	switch name {
	case "gbk":
		return simplifiedchinese.GBK
	case "gb18030":
		return simplifiedchinese.GB18030
	case "big5":
		return traditionalchinese.Big5
	case "shift-jis":
		return japanese.ShiftJIS
	case "euc-kr":
		return korean.EUCKR
	default:
		return nil
	}
}

func NewDecodingWriter(w io.Writer, enc string) io.Writer {
	e := EncodingByName(enc)
	if e == nil {
		return w
	}
	return transform.NewWriter(w, e.NewDecoder())
}

func NeedsTranscoding(p *Profile) bool {
	return p != nil && p.Encoding != ""
}
