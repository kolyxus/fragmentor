package analysis

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func openDemo(t *testing.T, d *downloader, url string) (*demoBody, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return d.open(ctx, url)
}

func TestDownload(t *testing.T) {
	srv := newDemoServer(t)
	body, err := openDemo(t, newDownloader(), srv.URL+"/demo?X-Amz-Signature="+signature)
	if err != nil {
		t.Fatalf("open() error: %v", err)
	}
	defer body.Close()
	got, err := io.ReadAll(body)
	if err != nil || string(got) != string(demoBytes) {
		t.Fatalf("body = %d bytes, %v; want the demo", len(got), err)
	}
	if body.n != int64(len(demoBytes)) || body.err != nil {
		t.Errorf("body counted %d bytes, err %v", body.n, body.err)
	}
}

func TestDownloadErrors(t *testing.T) {
	tests := []struct {
		name     string
		handler  http.HandlerFunc
		want     error
		wantText string
	}{
		// Backblaze B2 answers 401 to a link that has run out.
		{"expired", status(http.StatusUnauthorized, "UnauthorizedAccess"), errLinkRejected, "status 401 UnauthorizedAccess"},
		{"forbidden", status(http.StatusForbidden, "AccessDenied"), errLinkRejected, "status 403 AccessDenied"},
		{"bad request", status(http.StatusBadRequest, "AuthorizationQueryParametersError"), errLinkRejected, "status 400"},
		{"not found", status(http.StatusNotFound, "NoSuchKey"), errDemoGone, "status 404"},
		{"server error", status(http.StatusServiceUnavailable, "ServiceUnavailable"), errDownload, "status 503"},
		{"redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://example.com/elsewhere", http.StatusFound)
		}, errDownload, "status 302"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newDemoServer(t)
			srv.then(tt.handler)
			body, err := openDemo(t, newDownloader(), srv.URL+"/demo?X-Amz-Signature="+signature)
			if !errors.Is(err, tt.want) {
				t.Fatalf("open() = %v, %v; want %v", body, err, tt.want)
			}
			if !strings.Contains(err.Error(), tt.wantText) {
				t.Errorf("error %q, want it to tell %q", err, tt.wantText)
			}
			if strings.Contains(err.Error(), signature) || strings.Contains(err.Error(), srv.URL) {
				t.Errorf("error %q holds the link", err)
			}
			if n := srv.requests.Load(); n != 1 {
				t.Errorf("%d requests, want 1: a redirect is not followed", n)
			}
		})
	}
}

func TestDownloadNetworkErrorHidesLink(t *testing.T) {
	srv := newDemoServer(t)
	url := srv.URL + "/demo?X-Amz-Signature=" + signature
	srv.Close()
	_, err := openDemo(t, newDownloader(), url)
	if !errors.Is(err, errDownload) {
		t.Fatalf("open() error %v, want errDownload", err)
	}
	if strings.Contains(err.Error(), signature) {
		t.Errorf("error %q holds the link", err)
	}
}

func TestDownloadTooLarge(t *testing.T) {
	d := newDownloader()
	d.maxSize = 100

	// Content-Length tells at once.
	srv := newDemoServer(t)
	if _, err := openDemo(t, d, srv.URL+"/demo"); !errors.Is(err, errTooLarge) {
		t.Errorf("open() of a demo over the limit: error %v, want errTooLarge", err)
	}

	// Without it, the count does.
	srv.then(func(w http.ResponseWriter, _ *http.Request) {
		for range 10 {
			_, _ = w.Write(demoBytes[:50])
			w.(http.Flusher).Flush()
		}
	})
	body, err := openDemo(t, d, srv.URL+"/demo")
	if err != nil {
		t.Fatalf("open() of a chunked demo: %v", err)
	}
	defer body.Close()
	n, err := io.Copy(io.Discard, body)
	if !errors.Is(err, errTooLarge) || !errors.Is(body.err, errTooLarge) {
		t.Errorf("read %d bytes, error %v, body.err %v; want errTooLarge", n, err, body.err)
	}
	if n > d.maxSize {
		t.Errorf("read %d bytes past the limit of %d", n, d.maxSize)
	}
}

func TestDownloadCutShort(t *testing.T) {
	srv := newDemoServer(t)
	srv.then(cutShort)
	body, err := openDemo(t, newDownloader(), srv.URL+"/demo")
	if err != nil {
		t.Fatalf("open() error: %v", err)
	}
	defer body.Close()
	if _, err := io.Copy(io.Discard, body); !errors.Is(err, errDownload) {
		t.Errorf("reading a body cut short: error %v, want errDownload", err)
	}
	if !errors.Is(body.err, errDownload) || !errors.Is(body.err, io.ErrUnexpectedEOF) {
		t.Errorf("body.err = %v, want errDownload for an unexpected EOF", body.err)
	}
}
