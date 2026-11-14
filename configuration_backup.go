package skillsdex

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"
)

type configurationBackup struct {
	Version      int                   `json:"version"`
	Instructions []instructionRevision `json:"instructions"`
	Machines     []Device              `json:"machines"`
}

func writeConfigurationBackup(ctx context.Context, db *sql.DB, userID string, archive *zip.Writer) error {
	history, err := instructionHistory(ctx, db, userID)
	if err != nil {
		return err
	}
	machines, err := listDevices(ctx, db, userID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(configurationBackup{Version: 1, Instructions: history, Machines: machines})
	if err != nil {
		return err
	}
	if len(data) > 16<<20 {
		return errors.New("configuration history exceeds the 16 MiB portable backup limit")
	}
	file, err := archive.Create("configuration.json")
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	return err
}

func readConfigurationBackup(data []byte) (*configurationBackup, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var result *configurationBackup
	for _, file := range archive.File {
		if file.Name != "configuration.json" {
			continue
		}
		if result != nil {
			return nil, errors.New("duplicate configuration backup")
		}
		content, err := readZipFileLimited(file, 16<<20)
		if err != nil {
			return nil, err
		}
		var backup configurationBackup
		if err := json.Unmarshal(content, &backup); err != nil {
			return nil, err
		}
		if backup.Version != 1 {
			return nil, errors.New("unsupported configuration backup version")
		}
		last := int(^uint(0) >> 1)
		for _, revision := range backup.Instructions {
			if revision.Revision < 1 || revision.Revision >= last || len(revision.Body) > 65536 || !utf8.ValidString(revision.Body) {
				return nil, errors.New("invalid instruction history in backup")
			}
			if _, err := time.Parse(time.RFC3339, revision.CreatedAt); err != nil {
				return nil, errors.New("invalid instruction timestamp in backup")
			}
			last = revision.Revision
		}
		result = &backup
	}
	return result, nil
}

// Append restored history so existing revisions and connected-device version
// counters remain valid. Machine metadata is inventory only, never access.
func restoreConfigurationBackup(ctx context.Context, db *sql.DB, userID string, backup *configurationBackup) error {
	if backup == nil || len(backup.Instructions) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0) FROM instruction_revisions WHERE user_id=?`, userID).Scan(&current); err != nil {
		return err
	}
	for i := len(backup.Instructions) - 1; i >= 0; i-- {
		current++
		r := backup.Instructions[i]
		if _, err := tx.ExecContext(ctx, `INSERT INTO instruction_revisions(user_id,revision,body,created_at) VALUES(?,?,?,?)`, userID, current, r.Body, r.CreatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}
