package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestGetYards_AccessControl(t *testing.T) {
	_, userIDs, companyIDs, _, _, err := setupTest(t, 2, 2, 1, 1, time.Now())
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	user1 := userIDs[0]
	user2 := userIDs[1]

	// U0_COMP_0 belongs to user1, U1_COMP_0 belongs to user2
	user1Company := companyIDs[0]

	router := setupRouter()

	t.Run("User 1 accesses own yard", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/company/"+user1Company+"/yards", nil)
		req.Header.Set("Authorization", getAuthHeader(user1))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", w.Code)
		}
	})

	t.Run("User 2 blocked from User 1's yard", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/company/"+user1Company+"/yards", nil)
		req.Header.Set("Authorization", getAuthHeader(user2)) // Logged in as User 2

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected 403, got %d", w.Code)
		}
	})
}

func TestGetYardsHelper(t *testing.T) {
	// Setup: 2 Companies, 2 Yards per company
	_, _, companyIDs, _, _, err := setupTest(t, 1, 2, 2, 1, time.Now())
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	targetCompany := companyIDs[0]

	t.Run("Fetches only yards for target company", func(t *testing.T) {
		yards, err := getYardsHelper(context.TODO(), targetCompany)
		if err != nil {
			t.Errorf("Helper failed: %v", err)
		}

		if len(yards) != 2 {
			t.Errorf("Expected 2 yards for %s, got %d", targetCompany, len(yards))
		}

		// Ensure all returned yards actually start with the company prefix
		for _, y := range yards {
			if !strings.HasPrefix(y.ID, targetCompany+":") {
				t.Errorf("Yard %s does not belong to company %s", y.ID, targetCompany)
			}
		}
	})
}

func TestGetYardSummaries(t *testing.T) {
	// Setup: 1 Company, 2 Yards, 3 Nodes per yard
	_, _, companyIDs, yardIDs, nodeIDs, err := setupTest(t, 1, 1, 2, 3, time.Now())
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	targetCompany := companyIDs[0]

	t.Run("Isolates health status between yards", func(t *testing.T) {
		os.Setenv("TEMP_THRESHOLD", "80")
		os.Setenv("BATTERY_THRESHOLD", "20")

		// Sabotage one node in YARD_0
		// nodeIDs[0] should be "COMP_0:YARD_0:NODE_0"
		_, err = database.Collection("nodes").UpdateOne(context.TODO(),
			bson.M{"_id": nodeIDs[0]},
			bson.M{"$set": bson.M{"status": KEY_OVERHEATING}},
		)

		summaries, err := getYardSummaries(context.TODO(), targetCompany)
		if err != nil {
			t.Fatalf("Helper failed: %v", err)
		}

		if len(summaries) != 2 {
			t.Errorf("Expected 2 yard summaries, got %d", len(summaries))
		}

		for _, s := range summaries {
			if s.ID == yardIDs[0] {
				// Yard 0 should be unhealthy
				if s.UnhealthyCount != 1 {
					t.Errorf("Yard %s: Expected 1 unhealthy node, got %d", s.ID, s.UnhealthyCount)
				}
				if s.UnhealthyCount == 0 {
					t.Errorf("Yard %s: Should be marked unhealthy", s.ID)
				}
			} else {
				// Yard 1 should still be perfectly healthy
				if s.UnhealthyCount != 0 {
					t.Errorf("Yard %s: Expected 0 unhealthy nodes, got %d", s.ID, s.UnhealthyCount)
				}
				if s.UnhealthyCount > 0 {
					t.Errorf("Yard %s: Should be marked healthy", s.ID)
				}
			}

			if s.NodeCount != 3 {
				t.Errorf("Yard %s: Expected 3 nodes, got %d", s.ID, s.NodeCount)
			}
		}
	})
}
