package lego

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"
)

// SetPhoto is one photo attached to a set directly -- the box, the built
// model, a damaged part -- independent of any particular check (compare
// Proof/set_check_proofs, which is tied to one check's own evidence).
// Verified and stored the same way (see VerifyProofFile/StoreProof in
// proofs.go, both already generic over what they're attached to).
type SetPhoto struct {
	ID                 int64
	SetNum, Kind       string
	FilePath, Note, By string
	UploadedAt         time.Time
}

// AttachSetPhoto records an already-verified, already-stored photo against
// setNum. Call VerifyProofFile then StoreProof first -- this only writes
// the database record, never touches the file itself.
func (d *DB) AttachSetPhoto(setNum, kind, filePath, note, by string) (*SetPhoto, error) {
	now := time.Now().UTC()
	res, err := d.Exec(`INSERT INTO set_photos (set_num, kind, file_path, note, uploaded_by, uploaded_at) VALUES (?,?,?,?,?,?)`,
		setNum, kind, filePath, note, by, now.Format(timeLayout))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &SetPhoto{ID: id, SetNum: setNum, Kind: kind, FilePath: filePath, Note: note, By: by, UploadedAt: now}, nil
}

// SetPhotos lists every photo attached to setNum, oldest first.
func (d *DB) SetPhotos(setNum string) ([]SetPhoto, error) {
	rows, err := d.Query(`SELECT id, set_num, kind, file_path, note, uploaded_by, uploaded_at FROM set_photos WHERE set_num = ? ORDER BY id`, setNum)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SetPhoto
	for rows.Next() {
		var p SetPhoto
		var at string
		if err := rows.Scan(&p.ID, &p.SetNum, &p.Kind, &p.FilePath, &p.Note, &p.By, &at); err != nil {
			return nil, err
		}
		p.UploadedAt, _ = time.Parse(timeLayout, at)
		out = append(out, p)
	}
	return out, rows.Err()
}

// RemoveSetPhoto deletes one photo's record and its stored file -- the
// wrong photo attached by mistake, say. The file removal is best-effort: a
// file already missing never blocks clearing the record.
func (d *DB) RemoveSetPhoto(id int64) error {
	var path string
	err := d.QueryRow(`SELECT file_path FROM set_photos WHERE id = ?`, id).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("photo #%d not found", id)
	}
	if err != nil {
		return err
	}
	if _, err := d.Exec(`DELETE FROM set_photos WHERE id = ?`, id); err != nil {
		return err
	}
	_ = os.Remove(path)
	return nil
}
