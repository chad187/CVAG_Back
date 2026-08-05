package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestGetCompaniesAdmin(t *testing.T) {
	router := setupRouter()

	// Setup: Create a normal user and a SysAdmin
	// Assuming setupTest allows setting specific flags or manual DB injection
	sysAdminID, userIDs, _, _, _, _ := setupTest(t, 2, 1, 1, 1, time.Now().UTC())

	normalUser := userIDs[0]

	t.Run("Normal user is REJECTED", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/companies", nil)
		req.Header.Set("Authorization", getAuthHeader(normalUser))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected 403 Forbidden, got %d", w.Code)
		}
	})

	t.Run("SysAdmin is ACCEPTED", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/companies", nil)
		req.Header.Set("Authorization", getAuthHeader(sysAdminID))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", w.Code)
		}
	})
}

func TestGetYardsAdmin(t *testing.T) {
	router := setupRouter()

	// Setup matches company assertions framework exactly
	sysAdminID, userIDs, _, _, _, _ := setupTest(t, 2, 1, 1, 1, time.Now().UTC())
	normalUser := userIDs[0]

	t.Run("Normal user is REJECTED", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/yards", nil)
		req.Header.Set("Authorization", getAuthHeader(normalUser))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected 403 Forbidden, got %d", w.Code)
		}
	})

	t.Run("SysAdmin is ACCEPTED", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/yards", nil)
		req.Header.Set("Authorization", getAuthHeader(sysAdminID))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", w.Code)
		}
	})
}

func TestGetNodesAdmin(t *testing.T) {
	router := setupRouter()

	sysAdminID, userIDs, _, _, _, _ := setupTest(t, 2, 1, 1, 1, time.Now().UTC())
	normalUser := userIDs[0]

	t.Run("Normal user is REJECTED", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/nodes", nil)
		req.Header.Set("Authorization", getAuthHeader(normalUser))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected 403 Forbidden, got %d", w.Code)
		}
	})

	t.Run("SysAdmin is ACCEPTED", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/nodes", nil)
		req.Header.Set("Authorization", getAuthHeader(sysAdminID))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", w.Code)
		}
	})
}

func TestGetUsersAdminExclusion(t *testing.T) {
	router := setupRouter()

	// Setup 2 users
	adminUser, userIDs, _, _, _, err := setupTest(t, 5, 1, 1, 1, time.Now().UTC())
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	normalUser := userIDs[0]

	t.Run("SysAdmin sees everyone EXCEPT other SysAdmins", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/users", nil)
		req.Header.Set("Authorization", getAuthHeader(adminUser))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", w.Code)
		}

		var result []User
		json.Unmarshal(w.Body.Bytes(), &result)

		if len(result) != 5 {
			t.Errorf("Expected 5 users, got %d", len(result))
		}

		// 1. Check that the normal user IS in the list
		foundNormal := false
		foundAdmin := false
		for _, u := range result {
			if u.ID == normalUser {
				foundNormal = true
			}
			if u.ID == adminUser {
				foundAdmin = true
			}
		}

		if !foundNormal {
			t.Error("Normal user was missing from the list")
		}
		if foundAdmin {
			t.Error("SysAdmin (self) was incorrectly included in the filtered list")
		}
	})
}

func TestGetSingleUserAdmin(t *testing.T) {
	router := setupRouter()

	// Setup users
	adminUser, userIDs, _, _, _, err := setupTest(t, 2, 1, 1, 1, time.Now().UTC())
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	targetUser := userIDs[0]

	t.Run("SysAdmin can fetch a specific user", func(t *testing.T) {
		url := "/api/admin/user/" + targetUser.Hex()
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", getAuthHeader(adminUser))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", w.Code)
		}

		var result User
		json.Unmarshal(w.Body.Bytes(), &result)

		if result.ID != targetUser {
			t.Errorf("Expected ID %s, got %s", targetUser.Hex(), result.ID.Hex())
		}
	})

	t.Run("Rejects non-admin request", func(t *testing.T) {
		// Normal user trying to fetch themselves via the admin endpoint
		url := "/api/admin/user/" + targetUser.Hex()
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", getAuthHeader(targetUser))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected 403 Forbidden, got %d", w.Code)
		}
	})

	t.Run("Returns 404 for non-existent ID", func(t *testing.T) {
		fakeID := primitive.NewObjectID().Hex()
		url := "/api/admin/user/" + fakeID
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", getAuthHeader(adminUser))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected 404 Not Found, got %d", w.Code)
		}
	})
}

func TestEditUserAdmin(t *testing.T) {
	router := setupRouter()
	ctx := context.TODO()

	// Setup: 2 users
	adminUser, userIDs, _, _, _, err := setupTest(t, 2, 1, 1, 1, time.Now().UTC())
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	targetUser := userIDs[0]

	t.Run("SysAdmin can update a user's company list", func(t *testing.T) {
		payload := gin.H{"company_id": "NEW_COMP_A", "remove": false}
		body, _ := json.Marshal(payload)

		url := "/api/admin/user/" + targetUser.Hex()
		req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
		req.Header.Set("Authorization", getAuthHeader(adminUser))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d. Body: %s", w.Code, w.Body.String())
		}

		// Verify change in DB
		var updatedUser User
		database.Collection("users").FindOne(ctx, bson.M{"_id": targetUser}).Decode(&updatedUser)

		if len(updatedUser.CompanyIDs) != 2 || updatedUser.CompanyIDs[1] != "NEW_COMP_A" {
			t.Errorf("DB Update failed. Expected [NEW_COMP_A], got %v", updatedUser.CompanyIDs)
		}
	})

	t.Run("SysAdmin can update a user's company list", func(t *testing.T) {
		payload := gin.H{"company_id": "NEW_COMP_A", "remove": true}
		body, _ := json.Marshal(payload)

		url := "/api/admin/user/" + targetUser.Hex()
		req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
		req.Header.Set("Authorization", getAuthHeader(adminUser))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d. Body: %s", w.Code, w.Body.String())
		}

		// Verify change in DB
		var updatedUser User
		database.Collection("users").FindOne(ctx, bson.M{"_id": targetUser}).Decode(&updatedUser)

		if len(updatedUser.CompanyIDs) != 1 {
			t.Errorf("DB Update failed. Expected 1, got %v", updatedUser.CompanyIDs)
		}
	})

	t.Run("Rejects non-admin update attempts", func(t *testing.T) {
		payload := gin.H{"company_id": []string{"HACKED"}}
		body, _ := json.Marshal(payload)

		url := "/api/admin/user/" + targetUser.Hex()
		req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
		req.Header.Set("Authorization", getAuthHeader(targetUser)) // Normal user trying to edit

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected 403 Forbidden, got %d", w.Code)
		}
	})
}

func TestDeleteUserAdmin(t *testing.T) {
	router := setupRouter()
	ctx := context.TODO()

	// Setup: 3 users
	adminUser, userIDs, _, _, _, err := setupTest(t, 3, 1, 1, 1, time.Now().UTC())
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	targetUser := userIDs[0]

	t.Run("SysAdmin can delete a user", func(t *testing.T) {
		url := "/api/admin/user/" + targetUser.Hex()
		req, _ := http.NewRequest("DELETE", url, nil)
		req.Header.Set("Authorization", getAuthHeader(adminUser))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", w.Code)
		}

		// Verify user is deleted from DB
		var deletedUser User
		err := database.Collection("users").FindOne(ctx, bson.M{"_id": targetUser}).Decode(&deletedUser)

		if err == nil {
			t.Error("User should have been deleted but still exists in DB")
		}
	})

	t.Run("Rejects non-admin delete attempts", func(t *testing.T) {
		otherUser := userIDs[1]

		url := "/api/admin/user/" + otherUser.Hex()
		req, _ := http.NewRequest("DELETE", url, nil)
		req.Header.Set("Authorization", getAuthHeader(otherUser)) // Normal user trying to delete

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected 403 Forbidden, got %d", w.Code)
		}

		// Verify user still exists
		var user User
		err := database.Collection("users").FindOne(ctx, bson.M{"_id": otherUser}).Decode(&user)

		if err != nil {
			t.Error("User should still exist in DB after failed delete attempt")
		}
	})

	t.Run("Returns 404 for non-existent user", func(t *testing.T) {
		fakeID := primitive.NewObjectID().Hex()
		url := "/api/admin/user/" + fakeID
		req, _ := http.NewRequest("DELETE", url, nil)
		req.Header.Set("Authorization", getAuthHeader(adminUser))

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected 404 Not Found, got %d", w.Code)
		}
	})
}
