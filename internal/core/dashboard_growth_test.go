package core

import "testing"

func TestParseGrowthRange(t *testing.T) {
	cases := []struct {
		in      string
		label   string
		buckets int
		unit    string
		wantErr bool
	}{
		{in: "", label: "30d", buckets: 30, unit: "day"},
		{in: "7d", label: "7d", buckets: 7, unit: "day"},
		{in: "30d", label: "30d", buckets: 30, unit: "day"},
		{in: "90d", label: "90d", buckets: 90, unit: "day"},
		{in: "12m", label: "12m", buckets: 52, unit: "week"},
		{in: "1y", wantErr: true},
		{in: "30", wantErr: true},
		{in: "30D", wantErr: true},
		{in: "-7d", wantErr: true},
	}

	for _, c := range cases {
		r, err := ParseGrowthRange(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseGrowthRange(%q): want error, got %+v", c.in, r)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseGrowthRange(%q): unexpected error: %v", c.in, err)
			continue
		}
		if r.Label != c.label || r.Buckets != c.buckets || r.Unit != c.unit {
			t.Errorf("ParseGrowthRange(%q) = %+v, want {%s %d %s}", c.in, r, c.label, c.buckets, c.unit)
		}
	}
}

func TestParseGrowthTZ(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: "UTC"},
		{in: "  ", want: "UTC"},
		{in: "UTC", want: "UTC"},
		{in: "America/Chicago", want: "America/Chicago"},
		{in: "Asia/Kolkata", want: "Asia/Kolkata"},
		{in: "Local", wantErr: true},
		{in: "Mars/Olympus_Mons", wantErr: true},
		{in: "../../etc/passwd", wantErr: true},
		{in: "America/Chicago'; DROP TABLE subscribers;--", wantErr: true},
	}

	for _, c := range cases {
		got, err := ParseGrowthTZ(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseGrowthTZ(%q): want error, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseGrowthTZ(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseGrowthTZ(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
