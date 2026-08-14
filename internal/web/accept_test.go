package web

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrefersImage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []string
		want   bool
	}{
		{name: "missing"},
		{name: "curl wildcard", values: []string{"*/*"}},
		{name: "image wildcard", values: []string{"image/*"}, want: true},
		{name: "exact image", values: []string{"image/png"}, want: true},
		{
			name:   "browser image request",
			values: []string{"image/avif,image/webp,image/*,*/*;q=0.8"},
			want:   true,
		},
		{
			name:   "JSON quality wins",
			values: []string{"image/*;q=0.5,application/json;q=0.9"},
		},
		{
			name:   "explicit JSON quality overrides wildcard",
			values: []string{"application/json;q=0.2,*/*;q=1,image/*;q=0.5"},
			want:   true,
		},
		{name: "image rejected", values: []string{"image/*;q=0,*/*;q=1"}},
		{name: "malformed quality", values: []string{"image/*;q=nope"}},
		{name: "out of range quality", values: []string{"image/*;q=2"}},
		{
			name:   "multiple field lines",
			values: []string{"application/json;q=0.4", "image/*;q=0.8"},
			want:   true,
		},
		{
			name:   "quoted comma stays in one media range",
			values: []string{`application/json;note="a,b";q=0.9,image/*;q=0.8`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, prefersImage(test.values))
		})
	}
}
