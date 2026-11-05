package chat

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in, ch, name, text string
		ok                 bool
	}{
		{"(Global) Alice: hello there", "global", "Alice", "hello there", true},
		{"(Direct) Bob Smith: need help at NWAF", "direct", "Bob Smith", "need help at NWAF", true},
		{"RCon admin #0: (Global) Restart in 5 minutes", "admin", "Admin", "Restart in 5 minutes", true},
		{"Player #3 Alice (1.2.3.4:2304) connected", "", "", "", false},
		{"Verified GUID (abc) of player #3 Alice", "", "", "", false},
	}
	for _, c := range cases {
		m, ok := Parse(c.in)
		if ok != c.ok || m.Channel != c.ch || m.Name != c.name || m.Text != c.text {
			t.Errorf("Parse(%q) = %+v %v", c.in, m, ok)
		}
	}
}
