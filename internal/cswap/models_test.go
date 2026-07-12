package cswap

import "testing"

func TestPlatformDetectWSLvsLinux(t *testing.T) {
	// On a darwin host runtimeGOOS() returns "darwin", so the linux/WSL
	// branches cannot be exercised without overriding it.
	old := runtimeGOOS
	runtimeGOOS = func() string { return "linux" }
	defer func() { runtimeGOOS = old }()

	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	if got := DetectPlatform(); got != PlatformWSL {
		t.Fatalf("expected WSL with WSL_DISTRO_NAME set, got %v", got)
	}

	t.Setenv("WSL_DISTRO_NAME", "")
	if got := DetectPlatform(); got != PlatformLinux {
		t.Fatalf("expected Linux without WSL_DISTRO_NAME, got %v", got)
	}
}

func TestPlatformDetectMacOS(t *testing.T) {
	old := runtimeGOOS
	runtimeGOOS = func() string { return "darwin" }
	defer func() { runtimeGOOS = old }()
	if got := DetectPlatform(); got != PlatformMacOS {
		t.Fatalf("expected macOS, got %v", got)
	}
}

func TestAccountInfoRoundTrip(t *testing.T) {
	in := map[string]any{
		"email":            "a@example.com",
		"uuid":             "u1",
		"organizationUuid": "org-1",
		"organizationName": "Acme",
		"added":            "2026-01-01T00:00:00Z",
	}
	ai := AccountInfoFromDict(2, in)
	if ai.Email != "a@example.com" || ai.Number != 2 || !ai.IsOrganization() {
		t.Fatalf("unexpected AccountInfo: %+v", ai)
	}
	if ai.DisplayLabel() != "a@example.com [Acme]" {
		t.Fatalf("display label: %q", ai.DisplayLabel())
	}
	out := ai.ToDict()
	if out["email"] != "a@example.com" || out["organizationUuid"] != "org-1" {
		t.Fatalf("ToDict lost data: %+v", out)
	}
}

func TestAccountInfoPersonalLabel(t *testing.T) {
	ai := AccountInfoFromDict(1, map[string]any{"email": "b@example.com"})
	if ai.IsOrganization() {
		t.Fatal("empty org should not be an organization account")
	}
	if ai.DisplayLabel() != "b@example.com [personal]" {
		t.Fatalf("display label: %q", ai.DisplayLabel())
	}
}
