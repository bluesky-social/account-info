package web

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNegotiateAccountRepresentation(t *testing.T) {
	t.Parallel()

	const (
		browser = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/140.0 Safari/537.36"
		curl    = "curl/8.14.1"
	)
	tests := []struct {
		name      string
		accept    []string
		userAgent string
		want      accountRepresentation
	}{
		{name: "browser defaults to HTML", userAgent: browser, want: representationHTML},
		{name: "caller without context keeps JSON default", want: representationJSON},
		{name: "curl wildcard gets JSON", accept: []string{"*/*"}, userAgent: curl, want: representationJSON},
		{name: "Python wildcard gets JSON", accept: []string{"*/*"}, userAgent: "python-requests/2.32.4", want: representationJSON},
		{name: "Go client gets JSON", userAgent: "Go-http-client/1.1", want: representationJSON},
		{name: "Node client gets JSON", userAgent: "node-fetch", want: representationJSON},
		{
			name:      "browser navigation gets HTML",
			accept:    []string{"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8"},
			userAgent: browser,
			want:      representationHTML,
		},
		{
			name:      "explicit JSON overrides browser",
			accept:    []string{"application/json"},
			userAgent: browser,
			want:      representationJSON,
		},
		{
			name:      "explicit HTML overrides curl",
			accept:    []string{"text/html"},
			userAgent: curl,
			want:      representationHTML,
		},
		{
			name:      "JSON quality overrides HTML",
			accept:    []string{"text/html;q=0.5,application/json;q=0.9"},
			userAgent: browser,
			want:      representationJSON,
		},
		{
			name:      "HTML quality overrides JSON",
			accept:    []string{"text/html;q=0.9,application/json;q=0.5"},
			userAgent: curl,
			want:      representationHTML,
		},
		{
			name:      "image-only browser request gets image",
			accept:    []string{"image/avif,image/webp,image/*,*/*;q=0.8"},
			userAgent: browser,
			want:      representationImage,
		},
		{
			name:      "HTML wins browser tie with image",
			accept:    []string{"text/html,image/avif"},
			userAgent: browser,
			want:      representationHTML,
		},
		{
			name:      "unavailable media type is rejected",
			accept:    []string{"application/xml"},
			userAgent: browser,
			want:      representationNotAcceptable,
		},
		{
			name:      "all representations rejected",
			accept:    []string{"text/html;q=0,application/json;q=0,image/*;q=0"},
			userAgent: browser,
			want:      representationNotAcceptable,
		},
		{
			name:      "specific JSON quality overrides wildcard",
			accept:    []string{"application/json;q=0.2,*/*;q=1,image/*;q=0.5"},
			userAgent: curl,
			want:      representationHTML,
		},
		{
			name:      "multiple Accept field lines",
			accept:    []string{"application/json;q=0.4", "image/*;q=0.8"},
			userAgent: curl,
			want:      representationImage,
		},
		{
			name:      "quoted comma remains one media range",
			accept:    []string{`application/json;note="a,b";q=0.9,image/*;q=0.8`},
			userAgent: curl,
			want:      representationJSON,
		},
		{
			name:      "malformed quality is rejected",
			accept:    []string{"image/*;q=nope"},
			userAgent: browser,
			want:      representationNotAcceptable,
		},
		{
			name:      "out of range quality is rejected",
			accept:    []string{"image/*;q=2"},
			userAgent: browser,
			want:      representationNotAcceptable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(
				t,
				test.want,
				negotiateAccountRepresentation(test.accept, test.userAgent),
			)
		})
	}
}
