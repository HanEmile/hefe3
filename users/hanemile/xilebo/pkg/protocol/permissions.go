package protocol

// Permission flags. These are bitwise flags stored as uint64.
// Instance-scoped and room-scoped permissions use the same flag set;
// context determines which scope applies.
const (
	PermSendMessages   uint64 = 1 << iota // Send text messages
	PermSendMedia                          // Send files and media
	PermManageMessages                     // Delete others' messages
	PermInviteUsers                        // Invite users to rooms
	PermKickUsers                          // Remove users from rooms
	PermBanUsers                           // Ban users from rooms
	PermManageRoom                         // Change room name, topic, avatar
	PermManagePerms                        // Change others' power levels
	PermStartCalls                         // Initiate voice/video calls
	PermAdmin                              // Instance-level admin
)

// DefaultUserPerms is the permission set for new users.
const DefaultUserPerms = PermSendMessages | PermSendMedia | PermInviteUsers | PermStartCalls

// DefaultAdminPerms is the permission set for instance admins.
const DefaultAdminPerms = PermSendMessages | PermSendMedia | PermManageMessages |
	PermInviteUsers | PermKickUsers | PermBanUsers |
	PermManageRoom | PermManagePerms | PermStartCalls | PermAdmin

// DefaultPowerLevels defines the standard numeric power levels.
const (
	PowerLevelDefault   = 0
	PowerLevelModerator = 50
	PowerLevelAdmin     = 100
)

// HasPerm checks whether the given flags include a specific permission.
func HasPerm(flags, perm uint64) bool {
	return flags&perm == perm
}

// GrantPerm adds a permission to the flags.
func GrantPerm(flags, perm uint64) uint64 {
	return flags | perm
}

// RevokePerm removes a permission from the flags.
func RevokePerm(flags, perm uint64) uint64 {
	return flags &^ perm
}
