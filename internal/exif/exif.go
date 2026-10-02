// Package exif reads the capture time from JPEG and TIFF-based photos without decoding them.
package exif

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"time"
)

const head = 256 << 10

// Taken reports when the photo was shot, from DateTimeOriginal or else DateTime.
// Times without an offset are read in loc, the camera having been set to local time.
func Taken(r io.ReaderAt, loc *time.Location) (time.Time, bool) {
	b := make([]byte, head)
	n, _ := r.ReadAt(b, 0)
	b = b[:n]
	if len(b) > 4 && b[0] == 0xff && b[1] == 0xd8 {
		b = app1(b)
	}
	return tiff(b, loc)
}

func app1(b []byte) []byte {
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xff {
			return nil
		}
		m := b[i+1]
		if m == 0xd8 || m >= 0xd0 && m <= 0xd7 || m == 0x01 {
			i += 2
			continue
		}
		if m == 0xda || m == 0xd9 {
			return nil
		}
		size := int(binary.BigEndian.Uint16(b[i+2:]))
		end := i + 2 + size
		if size < 2 || end > len(b) {
			return nil
		}
		if seg := b[i+4 : end]; m == 0xe1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return seg[6:]
		}
		i = end
	}
	return nil
}

func tiff(b []byte, loc *time.Location) (time.Time, bool) {
	if len(b) < 8 {
		return time.Time{}, false
	}
	var o binary.ByteOrder
	switch string(b[:4]) {
	case "II*\x00":
		o = binary.LittleEndian
	case "MM\x00*":
		o = binary.BigEndian
	default:
		return time.Time{}, false
	}
	ifd0 := entries(b, o, o.Uint32(b[4:]))
	var original, offset string
	if p, ok := ifd0[0x8769]; ok {
		sub := entries(b, o, p.value(o))
		original, offset = sub[0x9003].text(b, o), sub[0x9011].text(b, o)
	}
	for _, s := range []string{original, ifd0[0x0132].text(b, o)} {
		if t, ok := parse(s, offset, loc); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

type entry struct {
	kind  uint16
	count uint32
	raw   []byte
}

func (e entry) value(o binary.ByteOrder) uint32 {
	if len(e.raw) < 4 {
		return 0
	}
	return o.Uint32(e.raw)
}

func (e entry) text(b []byte, o binary.ByteOrder) string {
	if e.kind != 2 || e.count == 0 || e.count > 64 {
		return ""
	}
	s := e.raw[:min(int(e.count), 4)]
	if e.count > 4 {
		at := int(e.value(o))
		if at < 0 || at+int(e.count) > len(b) {
			return ""
		}
		s = b[at : at+int(e.count)]
	}
	return strings.TrimRight(string(s), "\x00 ")
}

func entries(b []byte, o binary.ByteOrder, at uint32) map[uint16]entry {
	out := map[uint16]entry{}
	if int(at)+2 > len(b) || at == 0 {
		return out
	}
	n := int(o.Uint16(b[at:]))
	for i := range n {
		p := int(at) + 2 + 12*i
		if p+12 > len(b) {
			break
		}
		out[o.Uint16(b[p:])] = entry{kind: o.Uint16(b[p+2:]), count: o.Uint32(b[p+4:]), raw: b[p+8 : p+12]}
	}
	return out
}

func parse(s, offset string, loc *time.Location) (time.Time, bool) {
	if len(s) < 19 || strings.HasPrefix(s, "0000") {
		return time.Time{}, false
	}
	if len(offset) == 6 {
		if t, err := time.Parse("2006:01:02 15:04:05-07:00", s[:19]+offset); err == nil {
			return t, true
		}
	}
	t, err := time.ParseInLocation("2006:01:02 15:04:05", s[:19], loc)
	return t, err == nil && t.Year() > 1900
}
