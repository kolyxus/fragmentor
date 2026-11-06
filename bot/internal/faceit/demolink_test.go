package faceit

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// testDemoQuery is the query of a signed link as FACEIT makes it, with a
// made-up key ID and signature.
const testDemoQuery = "X-Amz-Algorithm=AWS4-HMAC-SHA256" +
	"&X-Amz-Credential=0000000000000000000000000%2F20261105%2F%2Fs3%2Faws4_request" +
	"&X-Amz-Date=20261105T120719Z&X-Amz-Expires=299&X-Amz-SignedHeaders=host&x-id=GetObject" +
	"&X-Amz-Signature=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

const testDemoMatchID = "1-00000000-0000-4000-8000-000000000004"

const testDemoLink = "https://demos-europe-central-faceit-cdn.s3.eu-central-003.backblazeb2.com/cs2/" +
	testDemoMatchID + "-1-1.dem.zst?" + testDemoQuery

func TestParseDemoLink(t *testing.T) {
	signed := time.Date(2026, 11, 5, 12, 7, 19, 0, time.UTC)
	tests := []struct {
		name string
		in   string
		url  string // "" for the input itself
	}{
		{"europe", testDemoLink, ""},
		{"us", "https://demos-us-east-faceit-cdn.s3.us-east-005.backblazeb2.com/cs2/" + testDemoMatchID + "-1-1.dem.zst?" + testDemoQuery, ""},
		{"spaces around", "  " + testDemoLink + "\n", testDemoLink},
		{"fragment dropped", testDemoLink + "#x", testDemoLink},
		{"upper case scheme", "HTTPS" + strings.TrimPrefix(testDemoLink, "https"), testDemoLink},
		{"upper case host", strings.Replace(testDemoLink, "demos-europe-central", "Demos-Europe-Central", 1),
			strings.Replace(testDemoLink, "demos-europe-central", "Demos-Europe-Central", 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link, err := ParseDemoLink(tt.in)
			if err != nil {
				t.Fatalf("ParseDemoLink() error: %v", err)
			}
			want := tt.url
			if want == "" {
				want = tt.in
			}
			if link.URL != want {
				t.Errorf("URL = %q, want %q", link.URL, want)
			}
			if link.MatchID != testDemoMatchID {
				t.Errorf("MatchID = %q, want %q", link.MatchID, testDemoMatchID)
			}
			if want := signed.Add(299 * time.Second); !link.Expires.Equal(want) {
				t.Errorf("Expires = %v, want %v", link.Expires, want)
			}
			if link.Lifetime != 299*time.Second {
				t.Errorf("Lifetime = %v, want 299s", link.Lifetime)
			}
		})
	}
}

func TestParseDemoLinkWithoutExpiry(t *testing.T) {
	for _, query := range []string{
		"X-Amz-Signature=abc",
		"X-Amz-Signature=abc&X-Amz-Date=20261105T120719Z",
		"X-Amz-Signature=abc&X-Amz-Expires=299",
		"X-Amz-Signature=abc&X-Amz-Date=yesterday&X-Amz-Expires=299",
		"X-Amz-Signature=abc&X-Amz-Date=20261105T120719Z&X-Amz-Expires=-5",
	} {
		link, err := ParseDemoLink("https://demos-europe-central-faceit-cdn.s3.eu-central-003.backblazeb2.com/cs2/" + testDemoMatchID + "-1-1.dem.zst?" + query)
		if err != nil {
			t.Errorf("%s: error %v", query, err)
			continue
		}
		if !link.Expires.IsZero() || link.Lifetime != 0 {
			t.Errorf("%s: Expires = %v, Lifetime = %v, want zero", query, link.Expires, link.Lifetime)
		}
	}
}

func TestParseDemoLinkRejects(t *testing.T) {
	const host = "demos-europe-central-faceit-cdn.s3.eu-central-003.backblazeb2.com"
	const file = "/cs2/" + testDemoMatchID + "-1-1.dem.zst"
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"http", "http://" + host + file + "?" + testDemoQuery, ErrNotDemoLink},
		{"no scheme", host + file + "?" + testDemoQuery, ErrNotDemoLink},
		{"other host", "https://example.com" + file + "?" + testDemoQuery, ErrNotDemoLink},
		{"host as a prefix", "https://" + host + ".example.com" + file + "?" + testDemoQuery, ErrNotDemoLink},
		{"host without demos-", "https://faceit-cdn.s3.eu-central-003.backblazeb2.com" + file + "?" + testDemoQuery, ErrNotDemoLink},
		{"another bucket", "https://demos-europe-central-other.s3.eu-central-003.backblazeb2.com" + file + "?" + testDemoQuery, ErrNotDemoLink},
		{"backblaze native host", "https://f003.backblazeb2.com/file/demos-europe-central-faceit-cdn" + file + "?" + testDemoQuery, ErrNotDemoLink},
		{"login", "https://user:pass@" + host + file + "?" + testDemoQuery, ErrNotDemoLink},
		{"port", "https://" + host + ":443" + file + "?" + testDemoQuery, ErrNotDemoLink},
		{"csgo", "https://" + host + "/csgo/" + testDemoMatchID + "-1-1.dem.zst?" + testDemoQuery, ErrNotDemoLink},
		{"second map", "https://" + host + "/cs2/" + testDemoMatchID + "-2-1.dem.zst?" + testDemoQuery, ErrNotDemoLink},
		{"gzip", "https://" + host + "/cs2/" + testDemoMatchID + "-1-1.dem.gz?" + testDemoQuery, ErrNotDemoLink},
		{"bad match ID", "https://" + host + "/cs2/1-xyz-1-1.dem.zst?" + testDemoQuery, ErrNotDemoLink},
		{"upper case match ID", "https://" + host + "/cs2/1-ABCDEF00-0000-4000-8000-00000000000A-1-1.dem.zst?" + testDemoQuery, ErrNotDemoLink},
		{"nested path", "https://" + host + "/cs2/x" + file + "?" + testDemoQuery, ErrNotDemoLink},
		{"escaped path", "https://" + host + "/cs2/" + testDemoMatchID + "%2D1-1.dem.zst?" + testDemoQuery, ErrNotDemoLink},
		{"dot segments", "https://" + host + "/cs2/../cs2/" + testDemoMatchID + "-1-1.dem.zst?" + testDemoQuery, ErrNotDemoLink},
		{"bad query", "https://" + host + file + "?X-Amz-Signature=%zz", ErrNotDemoLink},
		{"text", "привет", ErrNotDemoLink},
		{"empty", "", ErrNotDemoLink},
		{"no query", "https://" + host + file, ErrUnsignedLink},
		{"no signature", "https://" + host + file + "?X-Amz-Date=20261105T120719Z&X-Amz-Expires=299", ErrUnsignedLink},
		{"empty signature", "https://" + host + file + "?X-Amz-Signature=", ErrUnsignedLink},
		{"demo_url of the Data API", "https://demos-europe-central.backblaze.faceit-cdn.net" + file, ErrUnsignedLink},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link, err := ParseDemoLink(tt.in)
			if !errors.Is(err, tt.want) {
				t.Fatalf("ParseDemoLink() = %+v, %v; want error %v", link, err, tt.want)
			}
		})
	}
}
