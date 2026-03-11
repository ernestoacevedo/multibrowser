package chrome

import "testing"

func TestDefaultBinaryPath(t *testing.T) {
	t.Parallel()

	if got := DefaultBinaryPath(); got == "" {
		t.Fatal("DefaultBinaryPath() returned empty string")
	}
}
