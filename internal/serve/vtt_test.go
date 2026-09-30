package serve

import (
	"strings"
	"testing"

	"golang.org/x/text/encoding/traditionalchinese"
)

func TestSRTToVTT(t *testing.T) {
	in := "\xef\xbb\xbf1\r\n00:00:01,500 --> 00:00:03,020\r\nHello, world\r\n\r\n2\r\n01:02:03,4 --> 01:02:05,000 X1:10\r\n<i>bye</i>\r\n"
	want := "WEBVTT\n\n1\n00:00:01.500 --> 00:00:03.020\nHello, world\n\n2\n01:02:03.400 --> 01:02:05.000 X1:10\n<i>bye</i>\n"
	if got := string(SRTToVTT([]byte(in))); got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
	vtt := "WEBVTT\n\n00:01.000 --> 00:02.000\nhi\n"
	if got := string(SRTToVTT([]byte(vtt))); got != vtt {
		t.Fatalf("vtt changed: %q", got)
	}
	gbk := "1\r\n00:00:01,000 --> 00:00:02,000\r\n\xc4\xe3\xba\xc3\r\n"
	if got := string(SRTToVTT([]byte(gbk))); !strings.Contains(got, "你好") {
		t.Fatalf("gbk: %q", got)
	}
}

func TestUTF8(t *testing.T) {
	big5, _ := traditionalchinese.Big5.NewEncoder().String("中文書籍的內容")
	for in, want := range map[string]string{
		"中文书籍内容\xff结束":                             "中文书籍内容结束",
		"\xc4\xe3\xba\xc3\xa3\xac\xca\xc0\xbd\xe7": "你好，世界",
		big5:                 "中文書籍的內容",
		"caf\xe9 na\xefve":   "café naïve",
		"\xff\xfeh\x00i\x00": "hi",
		"\xef\xbb\xbfplain":  "plain",
	} {
		if got := string(UTF8([]byte(in))); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
