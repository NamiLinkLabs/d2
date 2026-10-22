package color

import "testing"

func TestThemeColorCodes(t *testing.T) {
	if len(ThemeColorCodes) != 18 {
		t.Fatalf("got %d theme color codes, want 18", len(ThemeColorCodes))
	}
	seen := map[string]bool{}
	for _, code := range ThemeColorCodes {
		if !IsThemeColor(code) {
			t.Errorf("IsThemeColor(%q) = false", code)
		}
		if seen[code] {
			t.Errorf("duplicate theme color code %q", code)
		}
		seen[code] = true
	}
}

func TestValidStyleColor(t *testing.T) {
	valid := []string{
		"N1", "N7", "B1", "B6", "AA2", "AA4", "AA5", "AB4", "AB5",
		"red", "Red", "#fff", "#f0ff3a", "transparent",
		"linear-gradient(red, blue)",
	}
	for _, s := range valid {
		if !ValidStyleColor(s) {
			t.Errorf("ValidStyleColor(%q) = false, want true", s)
		}
	}

	invalid := []string{
		"", "n1", "b2", "N0", "N8", "B7", "AA3", "AB2", "AC4", "N 1", " N1", "N1 ", "N12",
		"notacolor", "#ff", "linear-gradient(B1, N7)",
	}
	for _, s := range invalid {
		if ValidStyleColor(s) {
			t.Errorf("ValidStyleColor(%q) = true, want false", s)
		}
	}

	// Theme color codes must not collide with CSS named colors.
	for _, named := range NamedColors {
		if IsThemeColor(named) || LooksLikeThemeColor(named) {
			t.Errorf("named color %q looks like a theme color code", named)
		}
	}
}

func TestLooksLikeThemeColor(t *testing.T) {
	for _, s := range []string{"n1", "b2", "aa4", "Ab5", "N0", "N8", "B7", "AA3", "AB9"} {
		if !LooksLikeThemeColor(s) {
			t.Errorf("LooksLikeThemeColor(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"N1", "B2", "AA4", "AB5", "red", "#fff", "N12", "C1", ""} {
		if LooksLikeThemeColor(s) {
			t.Errorf("LooksLikeThemeColor(%q) = true, want false", s)
		}
	}
}

func TestGradientHasThemeColor(t *testing.T) {
	for _, s := range []string{"linear-gradient(B1, N7)", "radial-gradient(circle, red, AA4)"} {
		if !GradientHasThemeColor(s) {
			t.Errorf("GradientHasThemeColor(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"linear-gradient(red, blue)", "B1", "red", "linear-gradient(notacolor)"} {
		if GradientHasThemeColor(s) {
			t.Errorf("GradientHasThemeColor(%q) = true, want false", s)
		}
	}
}
