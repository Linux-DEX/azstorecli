package storage

import "testing"

func TestValidateFilter(t *testing.T) {
	if err := ValidateFilter("PartitionKey eq '2026'"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFilter("Status = 'new'"); err == nil {
		t.Fatal("SQL = should fail")
	}
	if err := ValidateFilter("PartitionKey eq 'unbalanced"); err == nil {
		t.Fatal("unbalanced quote")
	}
}
