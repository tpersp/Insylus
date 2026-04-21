package server

import (
	"context"
	"database/sql"
	"errors"

	"insylus/internal/pluginhost"
	"insylus/internal/shared"
)

type deviceAdminService struct {
	store   *Store
	targets pluginhost.TargetService
}

func (s deviceAdminService) UpdateNote(ctx context.Context, deviceID, note string) error {
	if err := s.store.UpdateDeviceNote(ctx, deviceID, note); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		target, getErr := s.targets.Get(ctx, deviceID)
		if getErr != nil {
			return err
		}
		_, updateErr := s.targets.Update(ctx, deviceID, pluginhost.TargetInput{
			Name:     target.Name,
			Kind:     target.Kind,
			Hostname: target.Hostname,
			IPs:      target.IPs,
			APIURL:   target.APIURL,
			SSHHost:  target.SSHHost,
			SSHUser:  target.SSHUser,
			Tags:     target.Tags,
			Note:     note,
		})
		return updateErr
	}
	return nil
}

func (s deviceAdminService) SetTypeOverride(ctx context.Context, deviceID string, deviceType *shared.DeviceType) error {
	return s.store.SetTypeOverride(ctx, deviceID, deviceType)
}

func (s deviceAdminService) SetPurposeOverride(ctx context.Context, deviceID string, purpose *shared.DevicePurpose) error {
	return s.store.SetPurposeOverride(ctx, deviceID, purpose)
}

func (s deviceAdminService) SetParentOverride(ctx context.Context, deviceID string, state shared.ParentOverrideState, parentDeviceID *string) error {
	return s.store.SetParentOverride(ctx, deviceID, state, parentDeviceID)
}
