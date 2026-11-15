package protocol

import "testing"

func TestHasPerm(t *testing.T) {
	flags := PermSendMessages | PermSendMedia | PermStartCalls

	if !HasPerm(flags, PermSendMessages) {
		t.Error("expected PermSendMessages")
	}
	if !HasPerm(flags, PermSendMedia) {
		t.Error("expected PermSendMedia")
	}
	if HasPerm(flags, PermAdmin) {
		t.Error("should not have PermAdmin")
	}
	if HasPerm(flags, PermKickUsers) {
		t.Error("should not have PermKickUsers")
	}
}

func TestGrantRevokePerm(t *testing.T) {
	flags := DefaultUserPerms

	// Grant admin
	flags = GrantPerm(flags, PermAdmin)
	if !HasPerm(flags, PermAdmin) {
		t.Error("expected PermAdmin after grant")
	}

	// Revoke admin
	flags = RevokePerm(flags, PermAdmin)
	if HasPerm(flags, PermAdmin) {
		t.Error("should not have PermAdmin after revoke")
	}

	// Other perms should still be there
	if !HasPerm(flags, PermSendMessages) {
		t.Error("PermSendMessages should survive admin revoke")
	}
}

func TestDefaultPerms(t *testing.T) {
	// Default users can send messages, media, invite, and start calls
	if !HasPerm(DefaultUserPerms, PermSendMessages) {
		t.Error("default: missing PermSendMessages")
	}
	if !HasPerm(DefaultUserPerms, PermSendMedia) {
		t.Error("default: missing PermSendMedia")
	}
	if !HasPerm(DefaultUserPerms, PermInviteUsers) {
		t.Error("default: missing PermInviteUsers")
	}
	if !HasPerm(DefaultUserPerms, PermStartCalls) {
		t.Error("default: missing PermStartCalls")
	}

	// Default users cannot kick, ban, manage
	if HasPerm(DefaultUserPerms, PermKickUsers) {
		t.Error("default: should not have PermKickUsers")
	}
	if HasPerm(DefaultUserPerms, PermAdmin) {
		t.Error("default: should not have PermAdmin")
	}
}

func TestAdminPerms(t *testing.T) {
	// Admins should have everything
	if !HasPerm(DefaultAdminPerms, PermAdmin) {
		t.Error("admin: missing PermAdmin")
	}
	if !HasPerm(DefaultAdminPerms, PermManagePerms) {
		t.Error("admin: missing PermManagePerms")
	}
	if !HasPerm(DefaultAdminPerms, PermBanUsers) {
		t.Error("admin: missing PermBanUsers")
	}
}
