package supervisor

import (
	"context"
	"errors"
	"fmt"
)

var addonUnits = map[string]bool{
	"homepanel-database-install.service": true,
	"homepanel-phpmyadmin.service":       true,
	"homepanel-update.service":           true,
}

var ErrUnitNotAllowed = errors.New("unit is not a panel add-on")

var ErrNoSystemd = errors.New("add-ons need systemd; run the installer from a terminal instead")

type AddonController interface {
	StartAddon(ctx context.Context, unit string) error
	RestartAddon(ctx context.Context, unit string) error
	AddonState(ctx context.Context, unit string) (active, sub string, err error)
}

func (s *Systemd) StartAddon(ctx context.Context, unit string) error {
	if !addonUnits[unit] {
		return ErrUnitNotAllowed
	}
	_ = s.conn.ResetFailedUnitContext(ctx, unit)
	if _, err := s.conn.StartUnitContext(ctx, unit, "replace", nil); err != nil {
		return fmt.Errorf("start %s: %w", unit, err)
	}
	return nil
}

func (s *Systemd) RestartAddon(ctx context.Context, unit string) error {
	if !addonUnits[unit] {
		return ErrUnitNotAllowed
	}
	if _, err := s.conn.RestartUnitContext(ctx, unit, "replace", nil); err != nil {
		return fmt.Errorf("restart %s: %w", unit, err)
	}
	return nil
}

func (s *Systemd) AddonState(ctx context.Context, unit string) (string, string, error) {
	if !addonUnits[unit] {
		return "", "", ErrUnitNotAllowed
	}
	props, err := s.conn.GetUnitPropertiesContext(ctx, unit)
	if err != nil {
		return "", "", err
	}
	a, _ := props["ActiveState"].(string)
	sub, _ := props["SubState"].(string)
	return a, sub, nil
}

func (l *Local) StartAddon(context.Context, string) error   { return ErrNoSystemd }
func (l *Local) RestartAddon(context.Context, string) error { return ErrNoSystemd }
func (l *Local) AddonState(context.Context, string) (string, string, error) {
	return "", "", ErrNoSystemd
}
