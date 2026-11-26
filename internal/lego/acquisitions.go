package lego

import (
	"errors"
	"fmt"
	"time"
)

// An acquisition follows one purchase (a set, a lot, a bulk bag) from being
// bought to being on sale, recording what it cost so profit per purchase can
// be worked out later.
var AcquisitionStages = []string{"purchased", "received", "checked", "shelved", "listed"}

type Acquisition struct {
	ID                         int64
	Label, Supplier, SetNum    string
	Cost                       float64
	Currency, Stage, StageAt   string
	Note, CreatedBy, CreatedAt string
}

func (d *DB) AddAcquisition(a Acquisition) (int64, error) {
	if a.Label == "" {
		return 0, errors.New("an acquisition needs a label")
	}
	if a.Cost < 0 {
		return 0, errors.New("cost cannot be negative")
	}
	now := time.Now().UTC().Format(timeLayout)
	res, err := d.Exec(`INSERT INTO acquisitions (label, supplier, set_num, cost, currency, stage, stage_at, note, created_by, created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		a.Label, a.Supplier, a.SetNum, a.Cost, a.Currency, AcquisitionStages[0], now, a.Note, a.CreatedBy, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// AdvanceAcquisition moves it to the next stage and returns the new one.
func (d *DB) AdvanceAcquisition(id int64) (string, error) {
	var stage string
	if err := d.QueryRow(`SELECT stage FROM acquisitions WHERE id = ?`, id).Scan(&stage); err != nil {
		return "", fmt.Errorf("no acquisition %d", id)
	}
	for i, s := range AcquisitionStages {
		if s == stage {
			if i == len(AcquisitionStages)-1 {
				return stage, errors.New("already listed: nothing further to do")
			}
			next := AcquisitionStages[i+1]
			_, err := d.Exec(`UPDATE acquisitions SET stage = ?, stage_at = ? WHERE id = ?`, next, time.Now().UTC().Format(timeLayout), id)
			return next, err
		}
	}
	return "", fmt.Errorf("acquisition %d is in unknown stage %q", id, stage)
}

// ListAcquisitions returns acquisitions newest first; stage "" means all.
func (d *DB) ListAcquisitions(stage string) ([]Acquisition, error) {
	q := `SELECT id, label, supplier, set_num, cost, currency, stage, stage_at, note, created_by, created_at FROM acquisitions`
	var args []any
	if stage != "" {
		q += ` WHERE stage = ?`
		args = append(args, stage)
	}
	rows, err := d.Query(q+` ORDER BY id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Acquisition
	for rows.Next() {
		var a Acquisition
		if err := rows.Scan(&a.ID, &a.Label, &a.Supplier, &a.SetNum, &a.Cost, &a.Currency, &a.Stage, &a.StageAt, &a.Note, &a.CreatedBy, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
