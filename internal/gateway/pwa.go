package gateway

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
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
