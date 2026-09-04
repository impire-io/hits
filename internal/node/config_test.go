package node

import "testing"

func TestParseSize(t *testing.T) {
	good := map[string]int64{
		"1024": 1024,
		"512K": 512 << 10,
		"2M":   2 << 20,
		"1G":   1 << 30,
		"2g":   2 << 30,
		"1T":   1 << 40,
		" 4G ": 4 << 30,
		"3 G":  3 << 30,
	}
	for in, want := range good {
		got, err := ParseSize(in)
		if err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}

	bad := []string{"", "abc", "-5", "0", "1.5G", "G", "9999999999999G"}
	for _, in := range bad {
		if got, err := ParseSize(in); err == nil {
			t.Errorf("ParseSize(%q) = %d, want an error", in, got)
		}
	}
}

func TestConfigDefaults(t *testing.T) {
	zero := Config{}
	if zero.opsMaxBytes() != DefaultMaxBytes {
		t.Errorf("zero ops budget = %d, want the default %d", zero.opsMaxBytes(), int64(DefaultMaxBytes))
	}
	if zero.stateMaxBytes() != DefaultMaxBytes/4 {
		t.Errorf("zero state budget = %d, want a quarter of the default", zero.stateMaxBytes())
	}
	set := Config{MaxBytes: 64 << 20}
	if set.opsMaxBytes() != 64<<20 || set.stateMaxBytes() != 16<<20 {
		t.Errorf("set budgets = %d/%d, want 64M/16M", set.opsMaxBytes(), set.stateMaxBytes())
	}
}
