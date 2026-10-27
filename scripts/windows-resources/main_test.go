package main

import "testing"

func TestVersionParts(t *testing.T) {
	for _, text := range []string{"1.0.0", "12.34.5678", "65535.65535.65535"} {
		parts, err := versionParts(text)
		if err != nil || parts[3] != 0 {
			t.Fatalf("valid version %q: %v %v", text, parts, err)
		}
	}
	parts, _ := versionParts("12.34.5678")
	if parts != [4]uint16{12, 34, 5678, 0} {
		t.Fatal(parts)
	}
	for _, text := range []string{"", "1.2", "1.2.3.4", "1.2.65536", "-1.2.3", "01.2.3", "1.2.x", "v1.2.3"} {
		if _, err := versionParts(text); err == nil {
			t.Errorf("accepted invalid version %q", text)
		}
	}
}
