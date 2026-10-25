package config

import "testing"

func TestChatGPTPlanRootIsTheParentOfTheSkillsDir(t *testing.T) {
	if got := (&Config{SkillsDir: "/var/lib/aura/skills"}).ChatGPTPlanRoot(); got != "/var/lib/aura" {
		t.Fatalf("ChatGPTPlanRoot() = %q, want /var/lib/aura", got)
	}
}
