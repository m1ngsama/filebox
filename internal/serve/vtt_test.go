package serve

import "testing"

func TestSRTToVTT(t *testing.T) {
	in := "\xef\xbb\xbf1\r\n00:00:01,500 --> 00:00:03,020\r\nHello, world\r\n\r\n2\r\n01:02:03,4 --> 01:02:05,000 X1:10\r\n<i>bye</i>\r\n"
	want := "WEBVTT\n\n1\n00:00:01.500 --> 00:00:03.020\nHello, world\n\n2\n01:02:03.4 --> 01:02:05.000 X1:10\n<i>bye</i>\n"
	if got := string(SRTToVTT([]byte(in))); got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
	vtt := "WEBVTT\n\n00:01.000 --> 00:02.000\nhi\n"
	if got := string(SRTToVTT([]byte(vtt))); got != vtt {
		t.Fatalf("vtt changed: %q", got)
	}
}
