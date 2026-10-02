package exif

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

func ifd(o binary.AppendByteOrder, base int, tags [][3]any) ([]byte, []byte) {
	dir := o.AppendUint16(nil, uint16(len(tags)))
	var data []byte
	heap := base + 2 + 12*len(tags) + 4
	for _, t := range tags {
		dir = o.AppendUint16(dir, t[0].(uint16))
		switch v := t[2].(type) {
		case string:
			s := append([]byte(v), 0)
			dir = o.AppendUint16(dir, 2)
			dir = o.AppendUint32(dir, uint32(len(s)))
			dir = o.AppendUint32(dir, uint32(heap+len(data)))
			data = append(data, s...)
		case uint32:
			dir = o.AppendUint16(dir, 4)
			dir = o.AppendUint32(dir, 1)
			dir = o.AppendUint32(dir, v)
		}
	}
	return o.AppendUint32(dir, 0), data
}

func photo(o binary.AppendByteOrder, original, offset, plain string) []byte {
	sig := "II*\x00"
	if o == binary.BigEndian {
		sig = "MM\x00*"
	}
	b := o.AppendUint32([]byte(sig), 8)
	var sub [][3]any
	if original != "" {
		sub = append(sub, [3]any{uint16(0x9003), 2, original})
	}
	if offset != "" {
		sub = append(sub, [3]any{uint16(0x9011), 2, offset})
	}
	tags := [][3]any{{uint16(0x0132), 2, plain}}
	n := 2 + 12*2 + 4 + len(plain) + 1
	tags = append(tags, [3]any{uint16(0x8769), 4, uint32(8 + n)})
	d0, h0 := ifd(o, 8, tags)
	b = append(append(b, d0...), h0...)
	d1, h1 := ifd(o, len(b), sub)
	return append(append(b, d1...), h1...)
}

func jpeg(t []byte) []byte {
	seg := append([]byte("Exif\x00\x00"), t...)
	b := []byte{0xff, 0xd8, 0xff, 0xe0, 0, 4, 'J', 'F'}
	b = append(b, 0xff, 0xe1)
	b = binary.BigEndian.AppendUint16(b, uint16(len(seg)+2))
	return append(append(b, seg...), 0xff, 0xda)
}

func TestTaken(t *testing.T) {
	shanghai := time.FixedZone("CST", 8*3600)
	cases := []struct {
		name string
		b    []byte
		want time.Time
	}{
		{"jpeg original with offset", jpeg(photo(binary.LittleEndian, "2019:04:28 10:23:28", "-05:00", "2020:01:01 00:00:00")), time.Date(2019, 4, 28, 15, 23, 28, 0, time.UTC)},
		{"tiff big endian local time", photo(binary.BigEndian, "2021:12:31 23:59:59", "", "2020:01:01 00:00:00"), time.Date(2021, 12, 31, 23, 59, 59, 0, shanghai)},
		{"falls back to DateTime", jpeg(photo(binary.LittleEndian, "", "", "2018:06:01 08:00:00")), time.Date(2018, 6, 1, 8, 0, 0, 0, shanghai)},
		{"zeroed original", jpeg(photo(binary.LittleEndian, "0000:00:00 00:00:00", "", "2018:06:01 08:00:00")), time.Date(2018, 6, 1, 8, 0, 0, 0, shanghai)},
	}
	for _, c := range cases {
		got, ok := Taken(bytes.NewReader(c.b), shanghai)
		if !ok || !got.Equal(c.want) {
			t.Errorf("%s: %v %v, want %v", c.name, got, ok, c.want)
		}
	}
	for _, b := range [][]byte{nil, []byte("not a photo"), {0xff, 0xd8, 0xff, 0xe1, 0xff, 0xff}, jpeg([]byte("II*\x00\xff\xff\xff\xff"))} {
		if _, ok := Taken(bytes.NewReader(b), time.UTC); ok {
			t.Errorf("%q had a date", b)
		}
	}
}

func FuzzTaken(f *testing.F) {
	f.Add(jpeg(photo(binary.LittleEndian, "2019:04:28 10:23:28", "+08:00", "x")))
	f.Add(photo(binary.BigEndian, "2019:04:28 10:23:28", "", "x"))
	f.Fuzz(func(t *testing.T, b []byte) { Taken(bytes.NewReader(b), time.UTC) })
}
