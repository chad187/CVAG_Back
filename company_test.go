package main

import (
	"context"
	"testing"
	"time"
)

func TestGetCompaniesHelper(t *testing.T) {
	// Setup: 1 User, 3 Companies total
	_, userID, companyIDs, _, _, err := setupTest(t, 2, 3, 1, 1, time.Now().UTC())
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// Define a user who only has access to 2 of the 3 companies
	testUser := User{
		ID:         userID[0],
		CompanyIDs: []string{companyIDs[0], companyIDs[1]},
	}

	t.Run("Returns only assigned companies", func(t *testing.T) {
		results, err := getCompaniesHelper(context.TODO(), testUser)
		if err != nil {
			t.Errorf("Helper failed: %v", err)
		}

		if len(results) != 2 {
			t.Errorf("Expected 2 companies, got %d", len(results))
		}
	})

	t.Run("Returns empty slice for user with no companies", func(t *testing.T) {
		emptyUser := User{ID: userID[0], CompanyIDs: []string{}}
		results, err := getCompaniesHelper(context.TODO(), emptyUser)
		if err != nil {
			t.Errorf("Helper failed: %v", err)
		}

		if len(results) != 0 {
			t.Errorf("Expected 0 companies, got %d", len(results))
		}
	})
}
