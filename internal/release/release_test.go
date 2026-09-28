package release

import "testing"

func TestParse(t *testing.T) {
	for in, ok := range map[string]bool{
		"v0.1.2": true, "0.1.2": true, "v1.10.0": true,
		"v0.0.0": false, "0.0.0": false, "v0.1": false, "v0.1.2-rc1": false,
		"v0.01.2": false, "(devel)": false, "": false, "v0.1.2+dirty": false,
	} {
		if _, got := Parse(in); got != ok {
			t.Errorf("Parse(%q) ok=%v, want %v", in, got, ok)
		}
	}
	if v, _ := Parse("0.1.2"); v.String() != "v0.1.2" {
		t.Fatalf("String %s", v)
	}
}

func TestGap(t *testing.T) {
	v := func(s string) Version { x, _ := Parse(s); return x }
	for _, c := range []struct{ from, to, want string }{
		{"v0.1.2", "v0.1.3", "patch"},
		{"v0.1.2", "v0.2.0", "minor"},
		{"v0.9.9", "v1.0.0", "major"},
		{"v0.1.2", "v0.1.2", "none"},
		{"v0.2.0", "v0.1.9", "none"},
	} {
		if got := Gap(v(c.from), v(c.to)); got != c.want {
			t.Errorf("Gap(%s, %s) = %s, want %s", c.from, c.to, got, c.want)
		}
	}
}
