package analysis

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/kolyxus/fragmentor/bot/internal/demo"
)

// Errors of a download. They never hold the link: it is a credential.
var (
	errLinkRejected = errors.New("analysis: the storage refused the link")
	errDemoGone     = errors.New("analysis: the storage has no such demo")
	errDownload     = errors.New("analysis: download failed")
	errTooLarge     = errors.New("analysis: the demo is larger than the limit")
)

// maxDemoSize bounds a download. A demo takes more decompressed than
// compressed, so the limit of the decompressed demo bounds both.
const maxDemoSize = demo.DefaultMaxSize

// responseTimeout bounds the wait for the headers of the answer; the
// whole download is bounded by the job.
const responseTimeout = 30 * time.Second

// downloader downloads demos from the FACEIT storage by signed links.
type downloader struct {
	client  *http.Client
	maxSize int64
}

func newDownloader() *downloader {
	t := http.DefaultTransport.(*http.Transport).Clone() // keeps HTTPS_PROXY
	t.ResponseHeaderTimeout = responseTimeout
	return &downloader{
		client: &http.Client{
			Transport: t,
			// The storage does not redirect, and a redirect would lead
			// off the host the link was checked for.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		maxSize: maxDemoSize,
	}
}

// open starts downloading the demo by link and returns its body as it
// comes in. 400, 401 and 403 are errLinkRejected, 404 errDemoGone, a demo
// larger than the limit errTooLarge and the rest errDownload.
func (d *downloader) open(ctx context.Context, link string) (*demoBody, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		// The error of url.Parse repeats the link.
		return nil, fmt.Errorf("%w: bad link", errDownload)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		// *url.Error repeats the link with its signature.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("%w: %w", errDownload, err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
		code := s3ErrorCode(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("%w: status %d %s", errLinkRejected, resp.StatusCode, code)
	case http.StatusNotFound:
		resp.Body.Close()
		return nil, fmt.Errorf("%w: status 404", errDemoGone)
	default:
		resp.Body.Close()
		return nil, fmt.Errorf("%w: status %d", errDownload, resp.StatusCode)
	}
	if resp.ContentLength > d.maxSize {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: %d bytes", errTooLarge, resp.ContentLength)
	}
	return &demoBody{r: resp.Body, limit: d.maxSize}, nil
}

// s3ErrorCode reads the code of an S3 error answer, like UnauthorizedAccess
// for a link that has run out on Backblaze B2.
func s3ErrorCode(body io.Reader) string {
	var e struct {
		Code string `xml:"Code"`
	}
	if xml.NewDecoder(io.LimitReader(body, 4<<10)).Decode(&e) != nil {
		return ""
	}
	return e.Code
}

// demoBody is the body of a download as the parser reads it. It counts the
// bytes, stops past the limit and keeps the first failure of the network or
// of the limit in err: the parser reports such a failure in its own way, a
// broken-off download as a truncated demo for one, so err tells the truth.
type demoBody struct {
	r     io.ReadCloser
	limit int64
	n     int64 // bytes read
	err   error
}

func (b *demoBody) Read(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	// One byte over the limit tells a demo of the limit from a larger one.
	if room := b.limit - b.n + 1; int64(len(p)) > room {
		p = p[:room]
	}
	n, err := b.r.Read(p)
	b.n += int64(n)
	switch {
	case b.n > b.limit:
		b.err = fmt.Errorf("%w: more than %d bytes", errTooLarge, b.limit)
		return 0, b.err
	case err != nil && err != io.EOF:
		b.err = fmt.Errorf("%w: %w", errDownload, err)
		return n, b.err
	}
	return n, err
}

func (b *demoBody) Close() error { return b.r.Close() }
