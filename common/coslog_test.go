package common

import "testing"

func TestParseCosLogSamplePercent(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int64
		wantErr bool
	}{
		{name: "disabled", value: "0", want: 0},
		{name: "ten percent", value: "10", want: 1000},
		{name: "fractional percent", value: "10.25", want: 1025},
		{name: "all", value: "100", want: 10000},
		{name: "negative", value: "-1", wantErr: true},
		{name: "over one hundred", value: "100.01", wantErr: true},
		{name: "too precise", value: "1.001", wantErr: true},
		{name: "not a number", value: "abc", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseCosLogSamplePercent(test.value)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", test.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCosLogSamplePercent(%q): %v", test.value, err)
			}
			if got != test.want {
				t.Fatalf("got %d, want %d", got, test.want)
			}
		})
	}
}
