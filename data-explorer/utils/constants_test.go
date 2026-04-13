package utils

import (
	"testing"
)

func TestFetchStorageContractAddresses(t *testing.T) {
	addresses, err := FetchStorageContractAddresses()
	if err != nil {
		t.Fatalf("FetchStorageContractAddresses() error = %v, want nil", err)
	}
	if len(addresses) == 0 {
		t.Errorf("FetchStorageContractAddresses() returned empty list, want non-empty")
	} else {
		t.Logf("FetchStorageContractAddresses() returned %d addresses", len(addresses))
	}
}
