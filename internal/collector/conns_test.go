package collector

import "testing"

func TestConnAddr(t *testing.T) {
	cases := []struct {
		ip   string
		port uint32
		want string
	}{
		{"127.0.0.1", 8080, "127.0.0.1:8080"},
		{"::1", 22, "[::1]:22"},
		{"fe80::1", 0, "[fe80::1]:*"},
		{"", 0, "*:*"},
		{"0.0.0.0", 443, "0.0.0.0:443"},
	}
	for _, c := range cases {
		if got := connAddr(c.ip, c.port); got != c.want {
			t.Errorf("connAddr(%q, %d) = %q, want %q", c.ip, c.port, got, c.want)
		}
	}
}

func TestSortConnsLeadsWithTraffic(t *testing.T) {
	sorted := []Conn{
		{Local: "a", State: "CLOSE_WAIT"},
		{Local: "b", State: "LISTEN"},
		{Local: "c", State: "ESTABLISHED"},
		{Local: "d", State: "LISTEN"},
		{Local: "e", State: "TIME_WAIT"},
	}
	sortConns(sorted)
	want := []string{"ESTABLISHED", "LISTEN", "LISTEN", "CLOSE_WAIT", "TIME_WAIT"}
	for i, w := range want {
		if sorted[i].State != w {
			t.Fatalf("row %d: state = %s, want %s", i, sorted[i].State, w)
		}
	}
}
