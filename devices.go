package skillsdex

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type Device struct {
	Reports    []deviceAgentStatus `json:"-"`
	ID         string              `json:"id"`
	UserID     string              `json:"-"`
	Name       string              `json:"name"`
	Platform   string              `json:"platform"`
	CreatedAt  string              `json:"created_at"`
	LastSeenAt string              `json:"last_seen_at"`
	RevokedAt  string              `json:"revoked_at"`
}

func createDeviceEnrolment(ctx context.Context, db *sql.DB, userID string) (string, error) {
	token := newToken()
	_, err := db.ExecContext(ctx, `INSERT INTO device_enrolments(token_hash,user_id,expires_at) VALUES(?,?,?)`, apiTokenHash(token), userID, time.Now().UTC().Add(10*time.Minute).Format(time.RFC3339))
	if err != nil {
		return "", err
	}
	return token, nil
}

func registerDevice(ctx context.Context, db *sql.DB, enrolment, name, platform string) (Device, string, error) {
	var device Device
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return device, "", errors.New("machine name must be between 1 and 100 bytes")
	}
	if platform != "windows" && platform != "darwin" && platform != "linux" {
		return device, "", errors.New("unsupported platform")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return device, "", err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	// DELETE RETURNING atomically consumes the short-lived enrolment token.
	err = tx.QueryRowContext(ctx, `DELETE FROM device_enrolments WHERE token_hash=? AND expires_at>? RETURNING user_id`, apiTokenHash(enrolment), now).Scan(&device.UserID)
	if err != nil {
		return device, "", errors.New("invalid or expired enrolment token")
	}
	device.ID = newID("dev")
	device.Name = name
	device.Platform = platform
	device.CreatedAt = now
	credential := newToken()
	_, err = tx.ExecContext(ctx, `INSERT INTO devices(id,user_id,name,platform,credential_hash,created_at) VALUES(?,?,?,?,?,?)`, device.ID, device.UserID, name, platform, apiTokenHash(credential), now)
	if err != nil {
		return Device{}, "", err
	}
	if err = tx.Commit(); err != nil {
		return Device{}, "", err
	}
	return device, credential, nil
}

func deviceForCredential(ctx context.Context, db *sql.DB, credential string) (Device, error) {
	var d Device
	err := db.QueryRowContext(ctx, `SELECT d.id,d.user_id,d.name,d.platform,d.created_at,COALESCE(d.last_seen_at,'') FROM devices d JOIN users u ON u.id=d.user_id WHERE d.credential_hash=? AND d.revoked_at IS NULL AND u.deactivated_at IS NULL`, apiTokenHash(credential)).Scan(&d.ID, &d.UserID, &d.Name, &d.Platform, &d.CreatedAt, &d.LastSeenAt)
	return d, err
}

func listDevices(ctx context.Context, db *sql.DB, userID string) ([]Device, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,name,platform,created_at,COALESCE(last_seen_at,''),COALESCE(revoked_at,'') FROM devices WHERE user_id=? ORDER BY created_at DESC,id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := []Device{}
	for rows.Next() {
		d := Device{UserID: userID}
		if err := rows.Scan(&d.ID, &d.Name, &d.Platform, &d.CreatedAt, &d.LastSeenAt, &d.RevokedAt); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

func revokeDevice(ctx context.Context, db *sql.DB, userID, deviceID string) error {
	result, err := db.ExecContext(ctx, `UPDATE devices SET revoked_at=? WHERE id=? AND user_id=? AND revoked_at IS NULL`, time.Now().UTC().Format(time.RFC3339), deviceID, userID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	return nil
}
