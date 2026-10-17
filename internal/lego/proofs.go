package lego

import (
	"fmt"
	"image"
	_ "image/jpeg" // registers the jpeg format with image.DecodeConfig
	_ "image/png"  // registers the png format with image.DecodeConfig
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A scanned or photographed copy of a finished paper checklist, attached as
// evidence against a set's check: if the CLI's own count is ever questioned,
// the proof is the record that the paper count (done carefully, by hand, at
// the table) was right, not the CLI entry.

// Proof is one attached file.
type Proof struct {
	ID                 int64
	CheckID            int64
	SetNum, Kind       string
	FilePath, Note, By string
	UploadedAt         time.Time
}

// maxProofSize is generous for a phone photo or a scanned PDF, small enough
// that a wildly oversized file is almost certainly a mistake, not a proof.
const maxProofSize = 25 << 20

// VerifyProofFile confirms src is a genuine, undamaged jpg, png or PDF before
// it's ever accepted as proof -- checking the actual bytes, not just the file
// extension, so a renamed or corrupt file fails loudly here rather than
// getting silently stored as worthless "evidence". Returns the detected kind
// ("jpeg", "png" or "pdf").
func VerifyProofFile(src string) (kind string, err error) {
	fi, err := os.Stat(src)
	if err != nil {
		return "", fmt.Errorf("can't read %s: %w", src, err)
	}
	if fi.Size() == 0 {
		return "", fmt.Errorf("%s is empty", src)
	}
	if fi.Size() > maxProofSize {
		return "", fmt.Errorf("%s is %.1f MB, larger than the %d MB limit for a proof file", src, float64(fi.Size())/(1<<20), maxProofSize>>20)
	}
	f, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer f.Close()
	head := make([]byte, 5)
	if n, _ := io.ReadFull(f, head); n >= 5 && string(head) == "%PDF-" {
		return "pdf", nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	_, format, err := image.DecodeConfig(f)
	if err != nil {
		return "", fmt.Errorf("%s isn't a readable jpg, png or pdf (it may be corrupt, or not actually one of those): %w", src, err)
	}
	if format != "jpeg" && format != "png" {
		return "", fmt.Errorf("%s decodes as %s, not a jpg or png", src, format)
	}
	return format, nil
}

var proofExt = map[string]string{"jpeg": ".jpg", "png": ".png", "pdf": ".pdf"}

// StoreProof copies an already-verified src into dir (created if needed)
// under a collision-proof name, returning the stored path. Call
// VerifyProofFile first -- this never re-checks the content itself.
func StoreProof(dir, setNum, kind, src string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%s%s", strings.ReplaceAll(setNum, "/", "_"), time.Now().UTC().Format("20060102-150405"), proofExt[kind])
	dst := filepath.Join(dir, name)
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return "", err
	}
	return dst, nil
}

// AttachProof records a stored proof file against checkID.
func (d *DB) AttachProof(checkID int64, setNum, kind, filePath, note, by string) (*Proof, error) {
	now := time.Now().UTC()
	res, err := d.Exec(`INSERT INTO set_check_proofs (check_id, set_num, kind, file_path, note, uploaded_by, uploaded_at) VALUES (?,?,?,?,?,?,?)`,
		checkID, setNum, kind, filePath, note, by, now.Format(timeLayout))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Proof{ID: id, CheckID: checkID, SetNum: setNum, Kind: kind, FilePath: filePath, Note: note, By: by, UploadedAt: now}, nil
}

// ProofsFor lists proofs attached to a check, oldest first.
func (d *DB) ProofsFor(checkID int64) ([]Proof, error) {
	rows, err := d.Query(`SELECT id, check_id, set_num, kind, file_path, note, uploaded_by, uploaded_at FROM set_check_proofs WHERE check_id = ? ORDER BY id`, checkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Proof
	for rows.Next() {
		var p Proof
		var at string
		if err := rows.Scan(&p.ID, &p.CheckID, &p.SetNum, &p.Kind, &p.FilePath, &p.Note, &p.By, &at); err != nil {
			return nil, err
		}
		p.UploadedAt, _ = time.Parse(timeLayout, at)
		out = append(out, p)
	}
	return out, rows.Err()
}

// FlagDispute marks a check disputed -- the CLI's own numbers are in
// question for this check, and a proof on file (see ProofsFor) is the
// evidence of what was actually true. Never applied automatically, only ever
// from a deliberate `wms lego attach --flag`; it changes no totals and
// doesn't touch accuracy scoring on its own -- an admin reviewing accuracy
// (see internal/lego/accuracy.go) decides whether a deduction stands.
func (d *DB) FlagDispute(checkID int64, note string) error {
	_, err := d.Exec(`UPDATE set_checks SET disputed = 1, dispute_note = ? WHERE id = ?`, note, checkID)
	return err
}
