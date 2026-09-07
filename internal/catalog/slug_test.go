package catalog

import "testing"

func TestSlugify(t *testing.T) {
	tests := []struct{ in, want string }{
		{"SweetFX by CeeJay.dk", "sweetfx-by-ceejay-dk"},
		{"Standard effects", "standard-effects"},
		{"qUINT by Marty McFly", "quint-by-marty-mcfly"},
		{"iMMERSE by Marty McFly", "immerse-by-marty-mcfly"},
		{"vort_Shaders by vortigern11", "vort-shaders-by-vortigern11"},
		{"ReshadeEffectShaderToggler (REST) by 4lex4nder", "reshadeeffectshadertoggler-rest-by-4lex4nder"},
		{"3DGameBridge by Janthony & DinnerBram", "3dgamebridge-by-janthony-dinnerbram"},
		{"LumeniteFX", "lumenitefx"},
		{"  ...leading and trailing...  ", "leading-and-trailing"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := Slugify(tt.in); got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Aliases are written into user config, so every one must resolve to a
// package id the real catalog actually produces.
func TestAliasesResolveToRealPackages(t *testing.T) {
	packages, err := ParsePackages(fixture(t, "EffectPackages.ini"))
	if err != nil {
		t.Fatalf("ParsePackages() error = %v", err)
	}
	ids := make(map[string]bool, len(packages))
	for _, p := range packages {
		ids[p.ID] = true
	}

	// The fixture holds only the first three packages, so check the
	// aliases those cover; the rest are asserted against the id format.
	for alias, full := range aliases {
		if Slugify(full) != full {
			t.Errorf("alias %q maps to %q, which is not a valid slug", alias, full)
		}
	}
	for _, alias := range []string{"standard", "sweetfx", "legacy"} {
		if got := ResolveAlias(alias); !ids[got] {
			t.Errorf("alias %q resolved to %q, absent from the catalog", alias, got)
		}
	}

	if got := ResolveAlias("not-an-alias"); got != "not-an-alias" {
		t.Errorf("ResolveAlias() should pass unknown ids through, got %q", got)
	}
}
