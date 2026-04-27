package homebox

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"insylus/internal/pluginhost"
)

const defaultConfigID = "default"

type store struct {
	db      pluginhost.DBHost
	secrets pluginhost.SecretHost
}

func newStore(host pluginhost.Host) store {
	return store{db: host.DB(), secrets: host.Secrets()}
}

func (s store) setConfig(ctx context.Context, item config) (configSummary, error) {
	item.BaseURL = normalizeBaseURL(item.BaseURL)
	item.Username = strings.TrimSpace(item.Username)
	item.LastError = strings.TrimSpace(item.LastError)
	if item.BaseURL == "" {
		return configSummary{}, errors.New("base_url is required")
	}
	if item.Username == "" {
		return configSummary{}, errors.New("username is required")
	}

	encryptedPassword := ""
	if strings.TrimSpace(item.Password) != "" {
		var err error
		encryptedPassword, err = s.secrets.Encrypt(item.Password)
		if err != nil {
			return configSummary{}, err
		}
	} else {
		existing, err := s.getConfig(ctx)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return configSummary{}, errors.New("password is required")
			}
			return configSummary{}, err
		}
		encryptedPassword, err = s.secrets.Encrypt(existing.Password)
		if err != nil {
			return configSummary{}, err
		}
	}

	encryptedToken, err := s.encryptOptional(item.Token)
	if err != nil {
		return configSummary{}, err
	}
	encryptedAttachmentToken, err := s.encryptOptional(item.AttachmentToken)
	if err != nil {
		return configSummary{}, err
	}

	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
		insert into homebox_config (
			id, base_url, username, password_encrypted, token_encrypted, attachment_token_encrypted,
			expires_at, last_connected_at, last_error, created_at, updated_at
		)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(id) do update set
			base_url = excluded.base_url,
			username = excluded.username,
			password_encrypted = excluded.password_encrypted,
			token_encrypted = excluded.token_encrypted,
			attachment_token_encrypted = excluded.attachment_token_encrypted,
			expires_at = excluded.expires_at,
			last_connected_at = excluded.last_connected_at,
			last_error = excluded.last_error,
			updated_at = excluded.updated_at`,
		defaultConfigID, item.BaseURL, item.Username, encryptedPassword, encryptedToken, encryptedAttachmentToken,
		timeText(item.ExpiresAt), timeText(item.LastConnectedAt), item.LastError, now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return configSummary{}, err
	}
	return s.summary(ctx)
}

func (s store) getConfig(ctx context.Context) (config, error) {
	row := s.db.QueryRowContext(ctx, `
		select base_url, username, password_encrypted, token_encrypted, attachment_token_encrypted,
			expires_at, last_connected_at, last_error, created_at, updated_at
		from homebox_config
		where id = ?`, defaultConfigID)
	return s.scanConfig(row)
}

func (s store) summary(ctx context.Context) (configSummary, error) {
	item, err := s.getConfig(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return configSummary{}, nil
		}
		return configSummary{}, err
	}
	return summarizeConfig(item), nil
}

func (s store) updateAuthState(ctx context.Context, state authState) error {
	encryptedToken, err := s.encryptOptional(state.Token)
	if err != nil {
		return err
	}
	encryptedAttachmentToken, err := s.encryptOptional(state.AttachmentToken)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		update homebox_config
		set token_encrypted = ?, attachment_token_encrypted = ?, expires_at = ?, last_error = '', updated_at = ?
		where id = ?`,
		encryptedToken, encryptedAttachmentToken, timeText(state.ExpiresAt), time.Now().UTC().Format(time.RFC3339), defaultConfigID)
	return err
}

func (s store) markConnected(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		update homebox_config
		set last_connected_at = ?, last_error = '', updated_at = ?
		where id = ?`,
		time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339), defaultConfigID)
	return err
}

func (s store) markError(ctx context.Context, msg string) error {
	_, err := s.db.ExecContext(ctx, `
		update homebox_config
		set last_error = ?, updated_at = ?
		where id = ?`,
		cleanError(msg), time.Now().UTC().Format(time.RFC3339), defaultConfigID)
	return err
}

func (s store) deleteConfig(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `delete from homebox_config where id = ?`, defaultConfigID)
	return err
}

type configScanner interface {
	Scan(dest ...any) error
}

func (s store) scanConfig(scanner configScanner) (config, error) {
	var item config
	var passwordEncrypted, tokenEncrypted, attachmentEncrypted string
	var expiresAtText, lastConnectedText string
	var createdAtText, updatedAtText string
	if err := scanner.Scan(
		&item.BaseURL, &item.Username, &passwordEncrypted, &tokenEncrypted, &attachmentEncrypted,
		&expiresAtText, &lastConnectedText, &item.LastError, &createdAtText, &updatedAtText,
	); err != nil {
		return config{}, err
	}
	var err error
	if item.Password, err = s.secrets.Decrypt(passwordEncrypted); err != nil {
		return config{}, err
	}
	if item.Token, err = s.decryptOptional(tokenEncrypted); err != nil {
		return config{}, err
	}
	if item.AttachmentToken, err = s.decryptOptional(attachmentEncrypted); err != nil {
		return config{}, err
	}
	item.ExpiresAt = parseTimePtr(expiresAtText)
	item.LastConnectedAt = parseTimePtr(lastConnectedText)
	if item.CreatedAt, err = time.Parse(time.RFC3339, createdAtText); err != nil {
		return config{}, err
	}
	if item.UpdatedAt, err = time.Parse(time.RFC3339, updatedAtText); err != nil {
		return config{}, err
	}
	return item, nil
}

func (s store) encryptOptional(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	return s.secrets.Encrypt(value)
}

func (s store) decryptOptional(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	return s.secrets.Decrypt(value)
}

func summarizeConfig(item config) configSummary {
	return configSummary{
		BaseURL:         item.BaseURL,
		Username:        item.Username,
		Configured:      item.BaseURL != "" && item.Username != "",
		Connected:       item.LastError == "" && item.LastConnectedAt != nil,
		TokenExpiresAt:  item.ExpiresAt,
		LastConnectedAt: item.LastConnectedAt,
		LastError:       item.LastError,
		UpdatedAt:       &item.UpdatedAt,
	}
}

func parseTimePtr(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	return &t
}

func timeText(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func cleanError(msg string) string {
	msg = strings.TrimSpace(msg)
	if len(msg) > 500 {
		return msg[:500]
	}
	return msg
}
