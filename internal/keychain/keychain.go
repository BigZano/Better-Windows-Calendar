package keychain

import (
	"fmt"
	"time"

	"github.com/zalando/go-keyring"

	"pycalendar/internal/storage"
)

// Set stores secret in the OS keychain and records the entry in credential_index.
func Set(service, account, secret string) error {
	if err := keyring.Set(service, account, secret); err != nil {
		return fmt.Errorf("keychain set %s/%s: %w", service, account, err)
	}
	return recordIndex(service, account)
}

// Get retrieves a secret from the OS keychain.
func Get(service, account string) (string, error) {
	secret, err := keyring.Get(service, account)
	if err != nil {
		return "", fmt.Errorf("keychain get %s/%s: %w", service, account, err)
	}
	return secret, nil
}

// Delete removes a secret from the OS keychain and its credential_index row.
func Delete(service, account string) error {
	if err := keyring.Delete(service, account); err != nil && err != keyring.ErrNotFound {
		return fmt.Errorf("keychain delete %s/%s: %w", service, account, err)
	}
	return removeIndex(service, account)
}

// DeleteAll removes every keychain entry recorded in credential_index.
// Called during uninstall to leave no orphaned credentials.
func DeleteAll() error {
	db, err := storage.Open(5)
	if err != nil {
		return err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT service, account FROM credential_index`)
	if err != nil {
		return fmt.Errorf("credential_index query: %w", err)
	}
	defer rows.Close()

	var entries [][2]string
	for rows.Next() {
		var svc, acct string
		if err := rows.Scan(&svc, &acct); err != nil {
			return err
		}
		entries = append(entries, [2]string{svc, acct})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	for _, e := range entries {
		if err := keyring.Delete(e[0], e[1]); err != nil && err != keyring.ErrNotFound {
			return fmt.Errorf("keychain delete %s/%s: %w", e[0], e[1], err)
		}
	}
	_, err = db.Exec(`DELETE FROM credential_index`)
	return err
}

func recordIndex(service, account string) error {
	db, err := storage.Open(5)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(
		`INSERT OR IGNORE INTO credential_index (service, account, created_ts) VALUES (?, ?, ?)`,
		service, account, time.Now().Unix(),
	)
	return err
}

func removeIndex(service, account string) error {
	db, err := storage.Open(5)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(`DELETE FROM credential_index WHERE service = ? AND account = ?`, service, account)
	return err
}
