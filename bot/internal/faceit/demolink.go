package faceit

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Errors of ParseDemoLink.
var (
	// ErrNotDemoLink means the text is not a link to the demo of a FACEIT
	// CS2 match: another scheme, host or path.
	ErrNotDemoLink = errors.New("faceit: not a link to a FACEIT demo")
	// ErrUnsignedLink means a link to a FACEIT demo the storage does not
	// give out: the demo_url of the Data API, or a link without the
	// signature.
	ErrUnsignedLink = errors.New("faceit: link to a FACEIT demo without a signature")
)

// DemoLink is a signed link to the demo of a FACEIT match, the one a browser
// downloads it by when "Watch demo" is pressed on FACEIT. It is a presigned
// URL of the S3 API of Backblaze B2, where FACEIT keeps the demos; see
// docs/notes/faceit-demo-links.md.
type DemoLink struct {
	// URL is the link to download the demo by. It is a credential for a few
	// minutes: never log it.
	URL string
	// MatchID is the FACEIT match ID, from the file name.
	MatchID string
	// Expires is when the signature runs out: X-Amz-Date plus X-Amz-Expires.
	// Zero if the link does not tell.
	Expires time.Time
	// Lifetime is X-Amz-Expires, how long FACEIT signs a link for: 299 s.
	// Zero if the link does not tell.
	Lifetime time.Duration
}

var (
	// demoHostRe is the host of a signed link: a bucket of FACEIT, like
	// demos-europe-central-faceit-cdn, on an S3 endpoint of Backblaze B2,
	// like s3.eu-central-003.backblazeb2.com.
	demoHostRe = regexp.MustCompile(`^demos-[a-z0-9-]+-faceit-cdn\.s3\.[a-z0-9-]+\.backblazeb2\.com$`)
	// unsignedHostRe is the host of demo_url in the Data API, like
	// demos-europe-central.backblaze.faceit-cdn.net. Without the Downloads
	// API a demo cannot be downloaded from it.
	unsignedHostRe = regexp.MustCompile(`^demos-[a-z0-9-]+\.backblaze\.faceit-cdn\.net$`)
	// demoPathRe is the demo of a CS2 match of one map, which FACEIT names
	// <match ID>-1-1.dem.zst.
	demoPathRe = regexp.MustCompile(`^/cs2/(1-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})-1-1\.dem\.zst$`)
)

// amzDateLayout is the layout of X-Amz-Date, like 20261028T120719Z.
const amzDateLayout = "20060102T150405Z"

// ParseDemoLink checks a signed link to a FACEIT demo and takes the match ID
// from it. The link must be https, without a login or a port, to a FACEIT
// bucket on Backblaze B2 (demos-*-faceit-cdn.s3.*.backblazeb2.com), to
// /cs2/<match ID>-1-1.dem.zst, and carry a signature. A link to a FACEIT demo
// without a signature, such as the demo_url of the Data API, is
// ErrUnsignedLink; anything else is ErrNotDemoLink. A link that has run out
// is not an error here: Expires tells when it does.
func ParseDemoLink(raw string) (DemoLink, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Port() != "" || u.Opaque != "" {
		return DemoLink{}, ErrNotDemoLink
	}
	host := strings.ToLower(u.Hostname())
	if unsignedHostRe.MatchString(host) {
		return DemoLink{}, ErrUnsignedLink
	}
	if u.Scheme != "https" || !demoHostRe.MatchString(host) {
		return DemoLink{}, ErrNotDemoLink
	}
	// The escaped path: an escaped character in it is not FACEIT's.
	m := demoPathRe.FindStringSubmatch(u.EscapedPath())
	if m == nil {
		return DemoLink{}, ErrNotDemoLink
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return DemoLink{}, ErrNotDemoLink
	}
	if query.Get("X-Amz-Signature") == "" {
		return DemoLink{}, ErrUnsignedLink
	}

	link := DemoLink{MatchID: m[1]}
	// The host and the query go as they came: the signature covers them. A
	// fragment, which never reaches the server anyway, is dropped.
	link.URL = "https://" + u.Host + u.EscapedPath() + "?" + u.RawQuery
	signed, dateErr := time.Parse(amzDateLayout, query.Get("X-Amz-Date"))
	seconds, expiresErr := strconv.Atoi(query.Get("X-Amz-Expires"))
	if dateErr == nil && expiresErr == nil && seconds > 0 {
		link.Lifetime = time.Duration(seconds) * time.Second
		link.Expires = signed.Add(link.Lifetime)
	}
	return link, nil
}
