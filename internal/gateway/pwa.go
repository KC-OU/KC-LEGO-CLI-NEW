package gateway

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
)

const manifestJSON = `{"name":"ModernWMS Touch Control Suite","short_name":"ModernWMS","start_url":"/","display":"standalone","background_color":"#000000","theme_color":"#003300","icons":[{"src":"/icon-192.png","sizes":"192x192","type":"image/png"},{"src":"/icon-512.png","sizes":"512x512","type":"image/png"}]}`

const serviceWorkerJS = `const CACHE = "modernwms-shell-v1";
const SHELL = ["/manifest.json", "/icon-192.png", "/icon-512.png"];
self.addEventListener("install", (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)));
});
self.addEventListener("activate", (e) => {
  e.waitUntil(caches.keys().then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))));
});
self.addEventListener("fetch", (e) => {
  if (SHELL.includes(new URL(e.request.url).pathname)) {
    e.respondWith(caches.match(e.request).then((r) => r || fetch(e.request)));
  }
});
`

// phosphorIcon renders a plain green-square-on-black placeholder PNG at the
// requested size — the original embedded a hand-crafted base64 PNG blob;
// generating one with image/png avoids carrying binary data in source at
// the cost of a slightly plainer icon, a deliberate simplification.
func phosphorIcon(size int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	black := color.RGBA{0, 0, 0, 255}
	green := color.RGBA{0x33, 0xff, 0x33, 255}
	margin := size / 8
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if x < margin || x >= size-margin || y < margin || y >= size-margin {
				img.Set(x, y, black)
			} else {
				img.Set(x, y, green)
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// pwaInjectTags is what makes "Add to Home Screen" actually launch ttyd's
// page as a chrome-less standalone window instead of a regular browser tab
// with the address bar/tabs still showing: manifest.json/sw.js/the icons
// were already served correctly (servePWAAssets, below), just never linked
// from the page itself, so a browser had no way to discover them.
const pwaInjectTags = `<link rel="manifest" href="/manifest.json"><meta name="theme-color" content="#003300">` +
	`<script>if('serviceWorker' in navigator){window.addEventListener('load',function(){navigator.serviceWorker.register('/sw.js').catch(function(){});});}</script></head>`

// newInjectedProxy builds a *httputil.ReverseProxy to target the same way
// httputil.NewSingleHostReverseProxy does (join paths, preserve the inbound
// Host header), via Rewrite rather than the deprecated Director (net/http/
// httputil: "Director is deprecated. Use Rewrite instead" -- SA1019; Rewrite
// also closes the IP-spoofing gap Director has by construction, since
// X-Forwarded-* headers from the client are stripped before Rewrite runs --
// SetXForwarded below re-adds them from the actual connection), and wires
// ModifyResponse to insert pwaInjectTags into ttyd's own HTML response in
// place, rather than freezing a static copy of today's ttyd output — a
// future ttyd upgrade on this box changes what gets proxied and rewritten
// automatically, never goes stale against a baked-in snapshot. Deliberately
// conservative: anything that isn't a plain, uncompressed text/html response
// with exactly one "</head>" is served completely untouched rather than
// guessed at — worst case, a page just doesn't get the PWA tags; the
// terminal itself never breaks over a cosmetic addition.
func newInjectedProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := &httputil.ReverseProxy{}
	proxy.Rewrite = func(pr *httputil.ProxyRequest) {
		pr.SetURL(target)
		pr.Out.Host = pr.In.Host // NewSingleHostReverseProxy's own default: it never rewrote the Host header
		pr.SetXForwarded()
		// ttyd compresses (gzip/br) whenever the request carries an
		// Accept-Encoding header -- which every real browser sends, curl
		// without -H does not -- and ModifyResponse below deliberately never
		// rewrites a compressed body. Left alone, that made the injection
		// below silently never fire for an actual visitor, only ever
		// appearing to work under a bare `curl` with no Accept-Encoding set.
		// Stripping it here means ttyd always hands back plain text/html.
		pr.Out.Header.Del("Accept-Encoding")
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		// Deleting Accept-Encoding above means Go's own Transport adds its
		// own (it only auto-decompresses when IT added the header) and
		// strips Content-Encoding before this ever runs -- so this branch
		// shouldn't be reachable through this proxy's real wiring any more.
		// Left in as defense-in-depth: if that ever changes, skipping a
		// compressed body is still safer than corrupting it.
		if resp.Header.Get("Content-Encoding") != "" {
			return nil
		}
		if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
			return nil
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return err
		}
		if bytes.Count(body, []byte("</head>")) != 1 {
			resp.Body = io.NopCloser(bytes.NewReader(body))
			return nil
		}
		body = bytes.Replace(body, []byte("</head>"), []byte(pwaInjectTags), 1)
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
		resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
		return nil
	}
	return proxy
}

func servePWAAssets(mux *http.ServeMux) {
	mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		_, _ = w.Write([]byte(manifestJSON))
	})
	mux.HandleFunc("/sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(serviceWorkerJS))
	})
	mux.HandleFunc("/icon-192.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(phosphorIcon(192))
	})
	mux.HandleFunc("/icon-512.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(phosphorIcon(512))
	})
}
