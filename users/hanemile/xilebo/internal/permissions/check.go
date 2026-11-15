package permissions

import (
	"context"
	"fmt"

	"github.com/hanemile/xilebo/internal/storage"
	"github.com/hanemile/xilebo/pkg/protocol"
)

// Checker verifies user permissions for actions.
type Checker struct {
	store storage.Store
}

// NewChecker creates a permissions checker.
func NewChecker(store storage.Store) *Checker {
	return &Checker{store: store}
}

// CanSendMessage checks if a user can send a message in a room.
func (c *Checker) CanSendMessage(ctx context.Context, roomID, userID string) error {
	return c.requireRoomPerm(ctx, roomID, userID, protocol.PermSendMessages, "send messages")
}

// CanManageMessages checks if a user can delete others' messages.
func (c *Checker) CanManageMessages(ctx context.Context, roomID, userID string) error {
	return c.requireRoomPerm(ctx, roomID, userID, protocol.PermManageMessages, "manage messages")
}

// CanInvite checks if a user can invite others to a room.
func (c *Checker) CanInvite(ctx context.Context, roomID, userID string) error {
	return c.requireRoomPerm(ctx, roomID, userID, protocol.PermInviteUsers, "invite users")
}

// CanKick checks if a user can remove others from a room.
func (c *Checker) CanKick(ctx context.Context, roomID, userID string) error {
	return c.requireRoomPerm(ctx, roomID, userID, protocol.PermKickUsers, "kick users")
}

// CanBan checks if a user can ban others from a room.
func (c *Checker) CanBan(ctx context.Context, roomID, userID string) error {
	return c.requireRoomPerm(ctx, roomID, userID, protocol.PermBanUsers, "ban users")
}

// CanManageRoom checks if a user can change room settings.
func (c *Checker) CanManageRoom(ctx context.Context, roomID, userID string) error {
	return c.requireRoomPerm(ctx, roomID, userID, protocol.PermManageRoom, "manage room")
}

// CanManagePerms checks if a user can change others' permissions.
func (c *Checker) CanManagePerms(ctx context.Context, roomID, userID string) error {
	return c.requireRoomPerm(ctx, roomID, userID, protocol.PermManagePerms, "manage permissions")
}

// CanSetPowerLevel checks if a user can set another user's power level in a room.
// The requester must have a higher power level than both the target and the new level.
func (c *Checker) CanSetPowerLevel(ctx context.Context, roomID, requesterID, targetID string, newLevel int) error {
	if err := c.CanManagePerms(ctx, roomID, requesterID); err != nil {
		return err
	}

	requesterLevel, err := c.store.GetPowerLevel(ctx, roomID, requesterID)
	if err != nil {
		return fmt.Errorf("get requester power level: %w", err)
	}

	// Can't set a level higher than or equal to your own
	if newLevel >= requesterLevel {
		return fmt.Errorf("cannot set power level %d (your level: %d)", newLevel, requesterLevel)
	}

	// Can't modify someone at or above your level
	if targetID != requesterID {
		targetLevel, err := c.store.GetPowerLevel(ctx, roomID, targetID)
		if err == nil && targetLevel >= requesterLevel {
			return fmt.Errorf("cannot modify user with power level %d (your level: %d)", targetLevel, requesterLevel)
		}
	}

	return nil
}

// IsInstanceAdmin checks if a user has instance admin permissions.
func (c *Checker) IsInstanceAdmin(ctx context.Context, userID string) error {
	perms, err := c.store.GetInstancePerms(ctx, userID)
	if err != nil {
		return fmt.Errorf("get instance perms: %w", err)
	}

	if !protocol.HasPerm(perms, protocol.PermAdmin) {
		return fmt.Errorf("instance admin required")
	}

	return nil
}

func (c *Checker) requireRoomPerm(ctx context.Context, roomID, userID string, perm uint64, action string) error {
	// First check instance-level perms
	instancePerms, err := c.store.GetInstancePerms(ctx, userID)
	if err != nil {
		return fmt.Errorf("get instance perms: %w", err)
	}

	// Instance admin bypasses room-level checks
	if protocol.HasPerm(instancePerms, protocol.PermAdmin) {
		return nil
	}

	// Check if user has the permission at instance level
	if protocol.HasPerm(instancePerms, perm) {
		return nil
	}

	// Fall back to checking room-level power level
	level, err := c.store.GetPowerLevel(ctx, roomID, userID)
	if err != nil {
		return fmt.Errorf("not a member of room %s", roomID)
	}

	// Moderators (50+) can do most things, admins (100) can do everything
	requiredLevel := permToMinLevel(perm)
	if level < requiredLevel {
		return fmt.Errorf("insufficient power level for %s (have: %d, need: %d)", action, level, requiredLevel)
	}

	return nil
}

// permToMinLevel maps a permission flag to the minimum power level required.
func permToMinLevel(perm uint64) int {
	switch perm {
	case protocol.PermSendMessages, protocol.PermSendMedia, protocol.PermStartCalls:
		return protocol.PowerLevelDefault
	case protocol.PermInviteUsers:
		return protocol.PowerLevelDefault
	case protocol.PermKickUsers, protocol.PermBanUsers, protocol.PermManageMessages:
		return protocol.PowerLevelModerator
	case protocol.PermManageRoom, protocol.PermManagePerms:
		return protocol.PowerLevelAdmin
	case protocol.PermAdmin:
		return protocol.PowerLevelAdmin
	default:
		return protocol.PowerLevelModerator
	}
}
