package migrate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOrderPutsNineBeforeTen(t *testing.T) {
	// Sorting migrations as plain strings puts 10 before 9, which applies an
	// ALTER before the CREATE it depends on.
	if order("09_add_integration_metadata.sql") >= order("10_unique_token_per_connection.sql") {
		t.Fatal("9 must sort before 10")
	}
	if order("00_create_tables.sql") != 0 {
		t.Fatal("leading zero should read as 0")
	}
	if order("no_number.sql") != 0 {
		t.Fatal("a name without a number should not panic")
	}
}

func TestMissingDirectoryIsExplained(t *testing.T) {
	// A broker whose image lost its migrations should say so, rather than
	// starting against whatever schema happens to be there.
	_, err := Run(nil, filepath.Join(t.TempDir(), "absent"))
	if err == nil {
		t.Fatal("expected an error for a missing migrations directory")
	}
	if got := err.Error(); got == "" ||
		!contains(got, "is missing") || !contains(got, "the image") {
		t.Fatalf("error should explain what is wrong, got: %s", got)
	}
}

func TestEmptyDirectoryIsRefused(t *testing.T) {
	_, err := Run(nil, t.TempDir())
	if err == nil {
		t.Fatal("expected an error when there are no migrations to apply")
	}
}

func TestOnlySQLFilesAreConsidered(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"README.md", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Run(nil, dir); err == nil {
		t.Fatal("a directory of non-SQL files holds no migrations")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
