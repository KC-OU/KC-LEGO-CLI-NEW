package publicblog

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"image/png"
)

// cleanImage re-encodes a JPEG or PNG so no camera metadata (GPS position, device, timestamps) is ever
// published. JPEGs keep their phone rotation. ok is false for formats it cannot decode.
func cleanImage(data []byte) (out []byte, ctype string, ok bool) {
	var buf bytes.Buffer
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8}):
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, "", false
		}
		if err := jpeg.Encode(&buf, rotate(img, exifOrientation(data)), &jpeg.Options{Quality: 88}); err != nil {
			return nil, "", false
		}
		return buf.Bytes(), "image/jpeg", true
	case bytes.HasPrefix(data, []byte("\x89PNG")):
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, "", false
		}
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", false
		}
		return buf.Bytes(), "image/png", true
	}
	return nil, "", false
}

// exifOrientation reads the EXIF orientation tag (1 when absent or unreadable).
func exifOrientation(b []byte) int {
	i := 2
	for i+4 <= len(b) && b[i] == 0xFF {
		m, n := b[i+1], int(binary.BigEndian.Uint16(b[i+2:]))
		if m == 0xDA || n < 2 || i+2+n > len(b) {
			break
		}
		if seg := b[i+4 : i+2+n]; m == 0xE1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			t := seg[6:]
			if len(t) < 8 {
				return 1
			}
			var bo binary.ByteOrder = binary.BigEndian
			if t[0] == 'I' {
				bo = binary.LittleEndian
			}
			off := int(bo.Uint32(t[4:]))
			if off+2 > len(t) {
				return 1
			}
			for e, cnt := off+2, int(bo.Uint16(t[off:])); cnt > 0 && e+12 <= len(t); e, cnt = e+12, cnt-1 {
				if bo.Uint16(t[e:]) == 0x0112 {
					return int(bo.Uint16(t[e+8:]))
				}
			}
			return 1
		}
		i += 2 + n
	}
	return 1
}

// rotate applies orientations 3, 6 and 8 (the ones phones produce); mirrored ones are left as is.
func rotate(src image.Image, o int) image.Image {
	if o != 3 && o != 6 && o != 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	var dst *image.RGBA
	if o == 3 {
		dst = image.NewRGBA(image.Rect(0, 0, w, h))
	} else {
		dst = image.NewRGBA(image.Rect(0, 0, h, w))
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := src.At(b.Min.X+x, b.Min.Y+y)
			switch o {
			case 3:
				dst.Set(w-1-x, h-1-y, c)
			case 6: // 90 degrees clockwise
				dst.Set(h-1-y, x, c)
			case 8: // 90 degrees anticlockwise
				dst.Set(y, w-1-x, c)
			}
		}
	}
	return dst
}
