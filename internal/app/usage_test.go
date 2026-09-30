package app

import "testing"

func TestVolumeUsageAndFolderSize(t *testing.T) {
	f := newTestApp(t)
	vs := decode[struct {
		Vols []struct {
			Name              string
			Used, Free, Total uint64
		}
	}](t, f.do("GET", "/api/vols", nil)).Vols
	if len(vs) != 2 || vs[0].Name != "v" || vs[0].Total == 0 || vs[0].Used+vs[0].Free != vs[0].Total || vs[0].Free == 0 {
		t.Fatalf("vols %+v", vs)
	}
	f.write(t, "d/a.txt", "abc")
	f.write(t, "d/e/b.txt", "hello")
	f.write(t, "dx/c.txt", "zzzzzzzz")
	f.write(t, "top.txt", "1")
	type size struct {
		Size, Files int64
		Scanning    bool
	}
	if s := decode[size](t, f.do("GET", "/api/size?vol=v&path=d", nil)); !s.Scanning {
		t.Fatalf("before scan %+v", s)
	}
	f.App.Index.Scan(f.App.Vols)
	for p, want := range map[string]size{"d": {8, 2, false}, "/": {17, 4, false}, "d/e": {5, 1, false}, "nope": {}} {
		if s := decode[size](t, f.do("GET", "/api/size?vol=v&path="+p, nil)); s != want {
			t.Errorf("size of %q = %+v, want %+v", p, s, want)
		}
	}
	if w := f.do("GET", "/api/size?vol=v&path=.trash", nil); w.Code != 400 {
		t.Fatalf("reserved %d", w.Code)
	}
	if w := f.do("GET", "/api/vols", nil, "X-No-Auth", "1"); w.Code != 401 {
		t.Fatalf("anonymous %d", w.Code)
	}
}
