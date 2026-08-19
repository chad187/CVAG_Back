package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestPostAlertDetails_WithTestMessageTranslation(t *testing.T) {
	// 1. Setup test environment
	_, userIDs, _, yardIDs, _, err := setupTest(t, 1, 1, 1, 0, time.Now().UTC())
	if err != nil {
		t.Fatalf("Failed to setup test data: %v", err)
	}

	yardID := yardIDs[0]

	// 2. Seed an existing alert with initial messages and a user target language (Spanish)
	seededAlert := AlertDetails{
		YardID:   yardID,
		CoolDown: 5,
		Messages: []AlertMessages{
			{Language: "English", Message: "live message old"},
		},
		TestMessages: []AlertMessages{
			{Language: "English", Message: "test message old"},
		},
		Users: []AlertUserDetails{
			{Language: "Spanish", Name: "Usuario de Prueba", Email: "test@example.com", Phone: "555-0100"},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	_, err = database.Collection("alerts").InsertOne(t.Context(), seededAlert)
	if err != nil {
		t.Fatalf("Failed to seed alert: %v", err)
	}

	// 3. Initialize router and generate test token
	router := setupRouter()
	token := generateTestToken(userIDs[0].Hex())

	// 4. Create the post payload with both Message and TestMessage updates
	payload := AlertPostPayload{
		Message:     "live message new",
		TestMessage: "test message new",
		CoolDown:    10,
		TestEmail:   "alerts@example.com",
		TestPhone:   "555-0199",
		LastRun:     time.Now().UTC().Unix(),
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	// 5. Execute the POST Request to update the alert and trigger translation for both
	req, _ := http.NewRequest("POST", "/api/yard/"+yardID+"/alert", bytes.NewBuffer(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200 on POST, got %d. Body: %s", w.Code, w.Body.String())
	}

	// 6. Verify via an HTTP GET request
	getReq, _ := http.NewRequest("GET", "/api/yard/"+yardID+"/alert", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)

	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("Expected status 200 on GET, got %d. Body: %s", getW.Code, getW.Body.String())
	}

	var fetchedAlert AlertDetails
	if err := json.Unmarshal(getW.Body.Bytes(), &fetchedAlert); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// 7. Assertions for Test Messages and Live Messages
	if len(fetchedAlert.Messages) < 1 || fetchedAlert.Messages[0].Message != "live message new" {
		t.Errorf("Expected primary message to be 'live message new', got %v", fetchedAlert.Messages)
	}

	// Ensure TestMessage updated to the new string and that translation triggered for the Spanish user
	if len(fetchedAlert.TestMessages) < 1 || fetchedAlert.TestMessages[0].Message != "test message new" {
		t.Errorf("Expected test message to be 'test message new', got %v", fetchedAlert.TestMessages)
	}

	if len(fetchedAlert.TestMessages) < 2 {
		t.Errorf("Expected test messages to include translation for Spanish user, got %d messages", len(fetchedAlert.TestMessages))
	} else if fetchedAlert.TestMessages[1].Language != "Spanish" {
		t.Errorf("Expected second test message language to be Spanish, got %s", fetchedAlert.TestMessages[1].Language)
	}
}

func TestGetAlertDetails_Existing(t *testing.T) {
	// 1. Setup test environment (1 user, 1 company, 1 yard)
	_, userIDs, _, yardIDs, _, err := setupTest(t, 1, 1, 1, 0, time.Now().UTC())
	if err != nil {
		t.Fatalf("Failed to setup test data: %v", err)
	}

	yardID := yardIDs[0]

	// 2. Manually seed an existing alert document in the collection
	seededAlert := AlertDetails{
		YardID:    yardID,
		CoolDown:  10,
		TestEmail: "test@example.com",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	_, err = database.Collection("alerts").InsertOne(t.Context(), seededAlert)
	if err != nil {
		t.Fatalf("Failed to seed alert: %v", err)
	}

	// 3. Initialize router and generate test token
	router := setupRouter()
	token := generateTestToken(userIDs[0].Hex())

	// 4. Execute the Request
	req, _ := http.NewRequest("GET", "/api/yard/"+yardID+"/alert", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 5. Assertions
	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var alert AlertDetails
	if err := json.Unmarshal(w.Body.Bytes(), &alert); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if alert.YardID != yardID {
		t.Errorf("Expected YardID %s, got %s", yardID, alert.YardID)
	}
	if alert.CoolDown != 10 {
		t.Errorf("Expected CoolDown to be 10, got %d", alert.CoolDown)
	}
	if alert.TestEmail != "test@example.com" {
		t.Errorf("Expected TestEmail 'test@example.com', got '%s'", alert.TestEmail)
	}
}

func TestGetAlertDetails_NotFoundAndCreated(t *testing.T) {
	// 1. Setup test environment without pre-seeding any alerts
	_, userIDs, _, yardIDs, _, err := setupTest(t, 1, 1, 1, 0, time.Now().UTC())
	if err != nil {
		t.Fatalf("Failed to setup test data: %v", err)
	}

	yardID := yardIDs[0]

	// 2. Initialize router and generate test token
	router := setupRouter()
	token := generateTestToken(userIDs[0].Hex())

	// 3. Execute the Request for a yard that has no alert document yet
	req, _ := http.NewRequest("GET", "/api/yard/"+yardID+"/alert", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 4. Assertions
	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var alert AlertDetails
	if err := json.Unmarshal(w.Body.Bytes(), &alert); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// Verify fallback defaults were applied and inserted
	if alert.YardID != yardID {
		t.Errorf("Expected YardID %s, got %s", yardID, alert.YardID)
	}
	if alert.CoolDown != 300000000000 {
		t.Errorf("Expected default CoolDown to be 300000000000, got %d", alert.CoolDown)
	}

	// 5. Verify the document was successfully inserted into MongoDB
	var dbAlert AlertDetails
	err = database.Collection("alerts").FindOne(t.Context(), bson.M{"yard_id": yardID}).Decode(&dbAlert)
	if err != nil {
		t.Fatalf("Expected alert to be inserted into database, but got error: %v", err)
	}

	if dbAlert.YardID != yardID {
		t.Errorf("Expected database document YardID %s, got %s", yardID, dbAlert.YardID)
	}
}

func TestPostAlertDetails_WithTranslation(t *testing.T) {
	// 1. Setup test environment
	_, userIDs, _, yardIDs, _, err := setupTest(t, 1, 1, 1, 0, time.Now().UTC())
	if err != nil {
		t.Fatalf("Failed to setup test data: %v", err)
	}

	yardID := yardIDs[0]

	// 2. Seed an existing alert with an "old" message and a user target language (Spanish)
	seededAlert := AlertDetails{
		YardID:   yardID,
		CoolDown: 5,
		Messages: []AlertMessages{
			{Language: "English", Message: "old"},
		},
		TestMessages: []AlertMessages{
			{Language: "English", Message: "test old"},
		},
		Users: []AlertUserDetails{
			{Language: "Spanish"},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	_, err = database.Collection("alerts").InsertOne(t.Context(), seededAlert)
	if err != nil {
		t.Fatalf("Failed to seed alert: %v", err)
	}

	// 3. Initialize router and generate test token
	router := setupRouter()
	token := generateTestToken(userIDs[0].Hex())

	// 4. Create the post payload with a "new" message
	payload := AlertPostPayload{
		Message:     "new",
		TestMessage: "test new",
		CoolDown:    10,
		TestEmail:   "alerts@example.com",
		TestPhone:   "555-0199",
		LastRun:     time.Now().UTC().Unix(),
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	// 5. Execute the POST Request to update the alert and trigger translation
	req, _ := http.NewRequest("POST", "/api/yard/"+yardID+"/alert", bytes.NewBuffer(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200 on POST, got %d. Body: %s", w.Code, w.Body.String())
	}

	// 6. Verify using getAlertDetails via an HTTP GET request (testing via the API route instead of direct DB query)
	getReq, _ := http.NewRequest("GET", "/api/yard/"+yardID+"/alert", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)

	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("Expected status 200 on GET, got %d. Body: %s", getW.Code, getW.Body.String())
	}

	var fetchedAlert AlertDetails
	if err := json.Unmarshal(getW.Body.Bytes(), &fetchedAlert); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// 7. Assertions
	if fetchedAlert.CoolDown != 10 {
		t.Errorf("Expected CoolDown to be 10, got %d", fetchedAlert.CoolDown)
	}

	// Ensure English message changed from "old" to "new"
	if len(fetchedAlert.Messages) < 1 || fetchedAlert.Messages[0].Message != "new" {
		t.Errorf("Expected primary message to be 'new', got %v", fetchedAlert.Messages)
	}

	// Ensure translation ran because "old" != "new" and appended the translated message
	if len(fetchedAlert.Messages) < 2 {
		t.Errorf("Expected translated message to be appended, but got only %d message(s)", len(fetchedAlert.Messages))
	} else {
		if fetchedAlert.Messages[1].Language != "Spanish" {
			t.Errorf("Expected second message language to be Spanish, got %s", fetchedAlert.Messages[1].Language)
		}
		if fetchedAlert.Messages[1].Message == "" {
			t.Errorf("Expected translated message content to not be empty")
		}
	}
}

func TestAddUserAlert(t *testing.T) {
	// 1. Setup test environment
	_, userIDs, _, yardIDs, _, err := setupTest(t, 1, 1, 1, 0, time.Now().UTC())
	if err != nil {
		t.Fatalf("Failed to setup test data: %v", err)
	}

	yardID := yardIDs[0]

	// 2. Seed initial alert details so getAlertDetails finds an existing document with a user and message
	seededAlert := AlertDetails{
		YardID:   yardID,
		CoolDown: 5,
		Messages: []AlertMessages{
			{Language: "English", Message: "Plant shutdown warning"},
		},
		Users: []AlertUserDetails{
			{
				Name:     "Initial User",
				Email:    "initial@example.com",
				Phone:    "555-0000",
				Language: "English",
			},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	_, err = database.Collection("alerts").InsertOne(t.Context(), seededAlert)
	if err != nil {
		t.Fatalf("Failed to seed alert: %v", err)
	}

	// 3. Initialize router and generate test token
	router := setupRouter()
	token := generateTestToken(userIDs[0].Hex())

	// ==========================================
	// Test Case 1: Successfully add a new user with a new language (triggers translation)
	// ==========================================
	newPayload := AlertUserDetails{
		Name:     "Maria Garcia",
		Email:    "maria@example.com",
		Phone:    "555-1111",
		Language: "Spanish",
	}

	jsonBody, err := json.Marshal(newPayload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	req, _ := http.NewRequest("PUT", "/api/yard/"+yardID+"/alert/user", bytes.NewBuffer(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200 on valid add, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify database record has the new user and the new Spanish translation appended
	var updatedAlert AlertDetails
	err = database.Collection("alerts").FindOne(t.Context(), bson.M{"yard_id": yardID}).Decode(&updatedAlert)
	if err != nil {
		t.Fatalf("Failed to query updated alert from database: %v", err)
	}

	if len(updatedAlert.Users) != 2 {
		t.Errorf("Expected 2 users in database, got %d", len(updatedAlert.Users))
	}

	// Check if Spanish translation was added
	foundSpanish := false
	for _, msg := range updatedAlert.Messages {
		if msg.Language == "Spanish" {
			foundSpanish = true
			if msg.Message == "" {
				t.Errorf("Expected translated message content for Spanish, got empty string")
			}
		}
	}
	if !foundSpanish {
		t.Errorf("Expected Spanish translation to be added to Messages")
	}

	// ==========================================
	// Test Case 2: Attempt to add a user with a duplicate email
	// ==========================================
	updateEmailPayload := AlertUserDetails{
		Name:     "Updated Initial User Name",
		Email:    "initial@example.com", // Matches seeded user
		Phone:    "555-0000",
		Language: "English",
	}

	updateEmailBody, _ := json.Marshal(updateEmailPayload)
	reqUpdateEmail, _ := http.NewRequest("PUT", "/api/yard/"+yardID+"/alert/user", bytes.NewBuffer(updateEmailBody))
	reqUpdateEmail.Header.Set("Authorization", "Bearer "+token)
	reqUpdateEmail.Header.Set("Content-Type", "application/json")

	wUpdateEmail := httptest.NewRecorder()
	router.ServeHTTP(wUpdateEmail, reqUpdateEmail)

	if wUpdateEmail.Code != http.StatusOK {
		t.Errorf("Expected status 200 on email upsert, got %d. Body: %s", wUpdateEmail.Code, wUpdateEmail.Body.String())
	}

	// Verify database record updated the name and kept total user count at 1
	var updatedAlertEmail AlertDetails
	err = database.Collection("alerts").FindOne(t.Context(), bson.M{"yard_id": yardID}).Decode(&updatedAlertEmail)
	if err != nil {
		t.Fatalf("Failed to query updated alert from database: %v", err)
	}

	if len(updatedAlertEmail.Users) != 2 {
		t.Errorf("Expected 2 users in database after upsert, got %d", len(updatedAlertEmail.Users))
	}
	if updatedAlertEmail.Users[0].Name != "Updated Initial User Name" {
		t.Errorf("Expected user name to be updated, got %s", updatedAlertEmail.Users[0].Name)
	}

	// ==========================================
	// Test Case 3: Update existing user by matching phone (Upsert)
	// ==========================================
	updatePhonePayload := AlertUserDetails{
		Name:     "Updated By Phone Name",
		Email:    "unique-new@example.com",
		Phone:    "555-0000", // Matches seeded user's phone
		Language: "English",
	}

	updatePhoneBody, _ := json.Marshal(updatePhonePayload)
	reqUpdatePhone, _ := http.NewRequest("PUT", "/api/yard/"+yardID+"/alert/user", bytes.NewBuffer(updatePhoneBody))
	reqUpdatePhone.Header.Set("Authorization", "Bearer "+token)
	reqUpdatePhone.Header.Set("Content-Type", "application/json")

	wUpdatePhone := httptest.NewRecorder()
	router.ServeHTTP(wUpdatePhone, reqUpdatePhone)

	if wUpdatePhone.Code != http.StatusOK {
		t.Errorf("Expected status 200 on phone upsert, got %d. Body: %s", wUpdatePhone.Code, wUpdatePhone.Body.String())
	}

	// Verify database record replaced the user properly via phone match
	var updatedAlertPhone AlertDetails
	err = database.Collection("alerts").FindOne(t.Context(), bson.M{"yard_id": yardID}).Decode(&updatedAlertPhone)
	if err != nil {
		t.Fatalf("Failed to query updated alert from database: %v", err)
	}

	if len(updatedAlertPhone.Users) != 2 {
		t.Errorf("Expected 2 users in database after phone upsert, got %d", len(updatedAlertPhone.Users))
	}
	if updatedAlertPhone.Users[0].Name != "Updated By Phone Name" {
		t.Errorf("Expected user name to be updated, got %s", updatedAlertPhone.Users[0].Name)
	}
}

func TestAddUserAlert_Comprehensive(t *testing.T) {
	// 1. Setup test environment
	_, userIDs, _, yardIDs, _, err := setupTest(t, 1, 1, 1, 0, time.Now().UTC())
	if err != nil {
		t.Fatalf("Failed to setup test data: %v", err)
	}

	yardID := yardIDs[0]

	// 2. Seed initial alert details with MULTIPLE users and messages
	seededAlert := AlertDetails{
		YardID:   yardID,
		CoolDown: 5,
		Messages: []AlertMessages{
			{Language: "English", Message: "Plant shutdown warning"},
			{Language: "Spanish", Message: "Advertencia de parada de planta"}, // Pre-translated
		},
		Users: []AlertUserDetails{
			{
				Name:     "User One",
				Email:    "one@example.com",
				Phone:    "555-0001",
				Language: "English",
			},
			{
				Name:     "User Two",
				Email:    "two@example.com",
				Phone:    "555-0002",
				Language: "Spanish",
			},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	_, err = database.Collection("alerts").InsertOne(t.Context(), seededAlert)
	if err != nil {
		t.Fatalf("Failed to seed alert: %v", err)
	}

	router := setupRouter()
	token := generateTestToken(userIDs[0].Hex())

	// ==========================================
	// Test Case 1: Add a user with an ALREADY translated language (Should NOT trigger new translation)
	// ==========================================
	existingLangPayload := AlertUserDetails{
		Name:     "User Three Spanish",
		Email:    "three@example.com",
		Phone:    "555-0003",
		Language: "Spanish", // Already exists in alert.Messages
	}

	jsonBody, _ := json.Marshal(existingLangPayload)
	req, _ := http.NewRequest("PUT", "/api/yard/"+yardID+"/alert/user", bytes.NewBuffer(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var alertAfter1 AlertDetails
	database.Collection("alerts").FindOne(t.Context(), bson.M{"yard_id": yardID}).Decode(&alertAfter1)

	if len(alertAfter1.Users) != 3 {
		t.Errorf("Expected 3 users, got %d", len(alertAfter1.Users))
	}
	// Messages count should still be 2 (English, Spanish) because Spanish was already there
	if len(alertAfter1.Messages) != 2 {
		t.Errorf("Expected messages length to remain 2, got %d", len(alertAfter1.Messages))
	}

	// ==========================================
	// Test Case 2: Update an existing user mid-list by Email (Preserve total count)
	// ==========================================
	updateUserPayload := AlertUserDetails{
		Name:     "User One Updated",
		Email:    "one@example.com", // Matches User One
		Phone:    "555-0001",
		Language: "English",
	}

	updateBody, _ := json.Marshal(updateUserPayload)
	reqUpdate, _ := http.NewRequest("PUT", "/api/yard/"+yardID+"/alert/user", bytes.NewBuffer(updateBody))
	reqUpdate.Header.Set("Authorization", "Bearer "+token)
	reqUpdate.Header.Set("Content-Type", "application/json")

	wUpdate := httptest.NewRecorder()
	router.ServeHTTP(wUpdate, reqUpdate)

	if wUpdate.Code != http.StatusOK {
		t.Fatalf("Expected status 200 on update, got %d", wUpdate.Code)
	}

	var alertAfter2 AlertDetails
	database.Collection("alerts").FindOne(t.Context(), bson.M{"yard_id": yardID}).Decode(&alertAfter2)

	// Count should still be 3 (updated in place, not appended)
	if len(alertAfter2.Users) != 3 {
		t.Errorf("Expected user count to stay 3 after email upsert, got %d", len(alertAfter2.Users))
	}

	// Verify it was User One that got updated
	foundUpdated := false
	for _, u := range alertAfter2.Users {
		if u.Email == "one@example.com" && u.Name == "User One Updated" {
			foundUpdated = true
		}
	}
	if !foundUpdated {
		t.Errorf("Expected User One's name to be updated successfully")
	}

	// ==========================================
	// Test Case 3: Add a user with a brand NEW language (Triggers translation)
	// ==========================================
	newLangPayload := AlertUserDetails{
		Name:     "User Portuguese",
		Email:    "portuguese@example.com",
		Phone:    "555-0004",
		Language: "Portuguese", // New language
	}

	portugueseBody, _ := json.Marshal(newLangPayload)
	reqPortuguese, _ := http.NewRequest("PUT", "/api/yard/"+yardID+"/alert/user", bytes.NewBuffer(portugueseBody))
	reqPortuguese.Header.Set("Authorization", "Bearer "+token)
	reqPortuguese.Header.Set("Content-Type", "application/json")

	wPortuguese := httptest.NewRecorder()
	router.ServeHTTP(wPortuguese, reqPortuguese)

	if wPortuguese.Code != http.StatusOK {
		t.Fatalf("Expected status 200 on new language add, got %d. Body: %s", wPortuguese.Code, wPortuguese.Body.String())
	}

	var alertAfter3 AlertDetails
	database.Collection("alerts").FindOne(t.Context(), bson.M{"yard_id": yardID}).Decode(&alertAfter3)

	if len(alertAfter3.Users) != 4 {
		t.Errorf("Expected 4 users, got %d", len(alertAfter3.Users))
	}

	// Verify Portuguese message was appended
	foundPortuguese := false
	for _, msg := range alertAfter3.Messages {
		if strings.EqualFold(msg.Language, "Portuguese") {
			foundPortuguese = true
			if msg.Message == "" {
				t.Errorf("Expected translated Portuguese message content, got empty string")
			}
		}
	}
	if !foundPortuguese {
		t.Errorf("Expected Portuguese translation to be added to Messages")
	}
}

func TestDeleteUserAlert(t *testing.T) {
	// 1. Setup test environment
	_, userIDs, _, yardIDs, _, err := setupTest(t, 1, 1, 1, 0, time.Now().UTC())
	if err != nil {
		t.Fatalf("Failed to setup test data: %v", err)
	}

	yardID := yardIDs[0]

	// 2. Seed initial alert details with a user to delete
	seededAlert := AlertDetails{
		YardID:   yardID,
		CoolDown: 5,
		Messages: []AlertMessages{
			{Language: "English", Message: "Plant shutdown warning"},
		},
		Users: []AlertUserDetails{
			{
				Name:     "Target User",
				Email:    "target@example.com",
				Phone:    "555-9999",
				Language: "English",
			},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	_, err = database.Collection("alerts").InsertOne(t.Context(), seededAlert)
	if err != nil {
		t.Fatalf("Failed to seed alert: %v", err)
	}

	// 3. Initialize router and generate test token
	router := setupRouter()
	token := generateTestToken(userIDs[0].Hex())

	// ==========================================
	// Test Case 1: Successfully delete an existing user by email
	// ==========================================
	payload := map[string]string{
		"email": "target@example.com",
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	req, _ := http.NewRequest("DELETE", "/api/yard/"+yardID+"/alert/user", bytes.NewBuffer(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200 on delete, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify database record has removed the user
	var updatedAlert AlertDetails
	err = database.Collection("alerts").FindOne(t.Context(), bson.M{"yard_id": yardID}).Decode(&updatedAlert)
	if err != nil {
		t.Fatalf("Failed to query updated alert from database: %v", err)
	}

	if len(updatedAlert.Users) != 0 {
		t.Errorf("Expected 0 users in database after deletion, got %d", len(updatedAlert.Users))
	}

	// ==========================================
	// Test Case 2: Attempt to delete a non-existent user email
	// ==========================================
	badPayload := map[string]string{
		"email": "nonexistent@example.com",
	}
	badBody, _ := json.Marshal(badPayload)

	reqBad, _ := http.NewRequest("DELETE", "/api/yard/"+yardID+"/alert/user", bytes.NewBuffer(badBody))
	reqBad.Header.Set("Authorization", "Bearer "+token)
	reqBad.Header.Set("Content-Type", "application/json")

	wBad := httptest.NewRecorder()
	router.ServeHTTP(wBad, reqBad)

	if wBad.Code == http.StatusOK {
		t.Errorf("Expected non-200 status code when deleting a non-existent user, got 200")
	}
}

func TestDeleteAlertHistory(t *testing.T) {
	// 1. Setup test environment
	_, userIDs, _, yardIDs, _, err := setupTest(t, 1, 1, 1, 0, time.Now().UTC())
	if err != nil {
		t.Fatalf("Failed to setup test data: %v", err)
	}

	yardID := yardIDs[0]
	targetDate := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)

	// Convert targetDate to millisecond timestamp matching frontend behavior
	targetMillis := targetDate.UnixNano() / int64(time.Millisecond)

	// 2. Seed initial alert details with a run history entry to delete
	seededAlert := AlertDetails{
		YardID:   yardID,
		CoolDown: 5,
		Messages: []AlertMessages{
			{Language: "English", Message: "Plant shutdown warning"},
		},
		RunHistory: []AlertRunHistory{
			{
				Date: targetDate,
			},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	_, err = database.Collection("alerts").InsertOne(t.Context(), seededAlert)
	if err != nil {
		t.Fatalf("Failed to seed alert: %v", err)
	}

	// 3. Initialize router and generate test token
	router := setupRouter()
	token := generateTestToken(userIDs[0].Hex())

	// ==========================================
	// Test Case 1: Successfully delete a run history entry by date path parameter
	// ==========================================
	req, _ := http.NewRequest("DELETE", "/api/yard/"+yardID+"/alert/history/"+fmt.Sprintf("%d", targetMillis), nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200 on delete history, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify database record has removed the run history entry
	var updatedAlert AlertDetails
	err = database.Collection("alerts").FindOne(t.Context(), bson.M{"yard_id": yardID}).Decode(&updatedAlert)
	if err != nil {
		t.Fatalf("Failed to query updated alert from database: %v", err)
	}

	if len(updatedAlert.RunHistory) != 0 {
		t.Errorf("Expected 0 run history entries in database after deletion, got %d", len(updatedAlert.RunHistory))
	}
}

func TestPostAlert_ExistingTestPhoneUpdate(t *testing.T) {
	// 1. Setup test environment
	_, userIDs, _, yardIDs, _, err := setupTest(t, 1, 1, 1, 0, time.Now().UTC())
	if err != nil {
		t.Fatalf("Failed to setup test data: %v", err)
	}

	yardID := yardIDs[0]

	// 2. Seed initial alert details with a user whose phone matches our upcoming payload test phone
	seededAlert := AlertDetails{
		YardID:   yardID,
		CoolDown: 5,
		Messages: []AlertMessages{
			{Language: "English", Message: "Initial plant message"},
		},
		TestMessages: []AlertMessages{
			{Language: "English", Message: "Test plant message"},
		},
		Users: []AlertUserDetails{
			{
				Name:     "Original Name",
				Email:    "oldemail@example.com",
				Phone:    "555-8888", // This phone number will be matched
				Language: "Spanish",  // Should be preserved
			},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	_, err = database.Collection("alerts").InsertOne(t.Context(), seededAlert)
	if err != nil {
		t.Fatalf("Failed to seed alert: %v", err)
	}

	router := setupRouter()
	token := generateTestToken(userIDs[0].Hex())

	// 3. Construct the payload using the matching phone number
	payload := AlertPostPayload{
		Message:     "Initial plant message", // Same message so it doesn't trigger translation logic
		TestMessage: "Test plant message",    // Same test message
		CoolDown:    10,
		TestEmail:   "updated-test@example.com",
		TestPhone:   "555-8888", // Matches existing user's phone
		LastRun:     time.Now().Unix(),
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	req, _ := http.NewRequest("POST", "/api/yard/"+yardID+"/alert", bytes.NewBuffer(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200 on post alert update, got %d. Body: %s", w.Code, w.Body.String())
	}

	// 4. Verify database record updated the user in-place instead of appending a new one
	var updatedAlert AlertDetails
	err = database.Collection("alerts").FindOne(t.Context(), bson.M{"yard_id": yardID}).Decode(&updatedAlert)
	if err != nil {
		t.Fatalf("Failed to query updated alert from database: %v", err)
	}

	// User count should still be 1 (updated, not appended)
	if len(updatedAlert.Users) != 1 {
		t.Errorf("Expected 1 user in database after phone match, got %d", len(updatedAlert.Users))
	}

	// Check that the email was updated, but the language ("Spanish") was successfully preserved
	updatedUser := updatedAlert.Users[0]
	if updatedUser.Email != "updated-test@example.com" {
		t.Errorf("Expected test email to be updated, got %s", updatedUser.Email)
	}
	if updatedUser.Language != "Spanish" {
		t.Errorf("Expected existing language preference 'Spanish' to be preserved, got %s", updatedUser.Language)
	}
}

func TestEditUserAlert_Matrix(t *testing.T) {
	// 1. Setup test environment
	_, userIDs, _, yardIDs, _, err := setupTest(t, 1, 1, 1, 0, time.Now().UTC())
	if err != nil {
		t.Fatalf("Failed to setup test data: %v", err)
	}

	yardID := yardIDs[0]

	// Seed with an initial user (English) and baseline messages
	seededAlert := AlertDetails{
		YardID:   yardID,
		CoolDown: 5,
		Messages: []AlertMessages{
			{Language: "English", Message: "Warning: High temperature detected"},
		},
		Users: []AlertUserDetails{
			{
				Name:     "Alice Smith",
				Email:    "alice@example.com",
				Phone:    "555-0100",
				Language: "English",
			},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	_, err = database.Collection("alerts").InsertOne(t.Context(), seededAlert)
	if err != nil {
		t.Fatalf("Failed to seed alert: %v", err)
	}

	router := setupRouter()
	token := generateTestToken(userIDs[0].Hex())

	// Define a sequence of operations to test create, update (email/phone), and language expansion
	testSteps := []struct {
		name          string
		payload       AlertUserDetails
		expectedCount int
		expectedLang  string
		expectedName  string
		expectNewLang bool
	}{
		{
			name: "Step 1: Add a new user with Spanish (Triggers translation)",
			payload: AlertUserDetails{
				Name:     "Carlos Gomez",
				Email:    "carlos@example.com",
				Phone:    "555-0200",
				Language: "Spanish",
			},
			expectedCount: 2,
			expectedLang:  "Spanish",
			expectedName:  "Carlos Gomez",
			expectNewLang: true,
		},
		{
			name: "Step 2: Add a new user with Portuguese (Triggers translation)",
			payload: AlertUserDetails{
				Name:     "Beatriz Silva",
				Email:    "beatriz@example.com",
				Phone:    "555-0300",
				Language: "Portuguese",
			},
			expectedCount: 3,
			expectedLang:  "Portuguese",
			expectedName:  "Beatriz Silva",
			expectNewLang: true,
		},
		{
			name: "Step 3: Update existing user (Alice) by matching Email (Should update in-place, count stays 3)",
			payload: AlertUserDetails{
				Name:     "Alice Updated",
				Email:    "alice@example.com", // Matches Alice's email
				Phone:    "555-0100",
				Language: "English",
			},
			expectedCount: 3,
			expectedLang:  "English",
			expectedName:  "Alice Updated",
			expectNewLang: false,
		},
		{
			name: "Step 4: Update existing user (Carlos) by matching Phone with a new email/name (Should update via phone match, count stays 3)",
			payload: AlertUserDetails{
				Name:     "Carlos NewEmail",
				Email:    "carlos.new@example.com", // Changed email
				Phone:    "555-0200",               // Matches Carlos's phone
				Language: "Spanish",
			},
			expectedCount: 3,
			expectedLang:  "Spanish",
			expectedName:  "Carlos NewEmail",
			expectNewLang: false,
		},
		{
			name: "Step 5: Add user with an already existing language (Spanish) - Should NOT trigger new translation",
			payload: AlertUserDetails{
				Name:     "Mateo Ruiz",
				Email:    "mateo@example.com",
				Phone:    "555-0400",
				Language: "Spanish", // Already exists
			},
			expectedCount: 4,
			expectedLang:  "Spanish",
			expectedName:  "Mateo Ruiz",
			expectNewLang: false,
		},
	}

	for _, step := range testSteps {
		t.Run(step.name, func(t *testing.T) {
			jsonBody, _ := json.Marshal(step.payload)
			req, _ := http.NewRequest("PUT", "/api/yard/"+yardID+"/alert/user", bytes.NewBuffer(jsonBody))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
			}

			// Fetch updated state from DB
			var updated AlertDetails
			err := database.Collection("alerts").FindOne(t.Context(), bson.M{"yard_id": yardID}).Decode(&updated)
			if err != nil {
				t.Fatalf("Failed to query database: %v", err)
			}

			// Validate user count
			if len(updated.Users) != step.expectedCount {
				t.Errorf("Expected %d users, got %d", step.expectedCount, len(updated.Users))
			}

			// Validate that the user was properly inserted or updated in the slice
			foundTarget := false
			for _, u := range updated.Users {
				// Match either by phone or email depending on what was tested
				if u.Email == step.payload.Email || u.Phone == step.payload.Phone {
					foundTarget = true
					if u.Name != step.expectedName {
						t.Errorf("Expected user name '%s', got '%s'", step.expectedName, u.Name)
					}
					if u.Language != step.expectedLang {
						t.Errorf("Expected language '%s', got '%s'", step.expectedLang, u.Language)
					}
				}
			}
			if !foundTarget {
				t.Errorf("Target user payload was not found correctly in database array")
			}
		})
	}
}

func TestEditUserAlert_TriggersTranslationOnLanguageChange(t *testing.T) {
	// 1. Setup test environment
	_, userIDs, _, yardIDs, _, err := setupTest(t, 1, 1, 1, 0, time.Now().UTC())
	if err != nil {
		t.Fatalf("Failed to setup test data: %v", err)
	}

	yardID := yardIDs[0]

	// 2. Seed initial alert with ONE English user and ONLY an English message
	seededAlert := AlertDetails{
		YardID:   yardID,
		CoolDown: 5,
		Messages: []AlertMessages{
			{Language: "English", Message: "Emergency shutdown sequence initiated"},
		},
		Users: []AlertUserDetails{
			{
				Name:     "John Doe",
				Email:    "john@example.com",
				Phone:    "555-9000",
				Language: "English", // Starts as English
			},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	_, err = database.Collection("alerts").InsertOne(t.Context(), seededAlert)
	if err != nil {
		t.Fatalf("Failed to seed alert: %v", err)
	}

	router := setupRouter()
	token := generateTestToken(userIDs[0].Hex())

	// 3. Update John Doe's language to Portuguese (matching via email)
	updatePayload := AlertUserDetails{
		Name:     "John Doe",
		Email:    "john@example.com", // Matches existing user
		Phone:    "555-9000",
		Language: "Portuguese", // Changed to a new language
	}

	jsonBody, err := json.Marshal(updatePayload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	req, _ := http.NewRequest("PUT", "/api/yard/"+yardID+"/alert/user", bytes.NewBuffer(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200 on language update, got %d. Body: %s", w.Code, w.Body.String())
	}

	// 4. Verify database record updated the user's language AND successfully added the Portuguese translation
	var updatedAlert AlertDetails
	err = database.Collection("alerts").FindOne(t.Context(), bson.M{"yard_id": yardID}).Decode(&updatedAlert)
	if err != nil {
		t.Fatalf("Failed to query updated alert from database: %v", err)
	}

	// Check that user language changed
	if updatedAlert.Users[0].Language != "Portuguese" {
		t.Errorf("Expected user language to be updated to Portuguese, got %s", updatedAlert.Users[0].Language)
	}

	// Check that a Portuguese message translation was generated and added
	foundPortugueseTranslation := false
	for _, msg := range updatedAlert.Messages {
		if strings.EqualFold(msg.Language, "Portuguese") {
			foundPortugueseTranslation = true
			if msg.Message == "" {
				t.Errorf("Expected translated message content for Portuguese, got empty string")
			}
		}
	}

	if !foundPortugueseTranslation {
		t.Errorf("Expected changing user language to trigger and append a Portuguese translation to Messages")
	}
}

//func TestBroadcastAlert(t *testing.T) {
//	// 1. Setup test environment
//	_, userIDs, _, yardIDs, _, err := setupTest(t, 1, 1, 1, 0, time.Now().UTC())
//	if err != nil {
//		t.Fatalf("Failed to setup test data: %v", err)
//	}

//	yardID := yardIDs[0]

//	// 2. Seed initial alert details with users and messages (English and Spanish)
//	seededAlert := AlertDetails{
//		YardID:   yardID,
//		CoolDown: 5 * time.Minute,
//		Messages: []AlertMessages{
//			{Language: "English", Message: "This is a Central Valley Ag Plant Shutdown Alert. All equipment operators: Shut down all operations immediately. For additional instructions, contact Mike Barry or Wyatt Best."},
//			{Language: "Spanish", Message: "Este es un aviso de paralización de operaciones de Central Valley Ag. A todos los operadores de maquinaria: detengan todas las operaciones de inmediato. Para recibir instrucciones adicionales, comuníquense con Mike Barry o Wyatt Best."},
//		},
//		Users: []AlertUserDetails{
//			{Email: "test@gmail.com", Phone: "+10000000001", Language: "Spanish"},
//			{Email: "test+test@gmail.com", Phone: "+10000000002", Language: "English"},
//		},
//		CreatedAt: time.Now().UTC(),
//		UpdatedAt: time.Now().UTC(),
//	}
//	_, err = database.Collection("alerts").InsertOne(t.Context(), seededAlert)
//	if err != nil {
//		t.Fatalf("Failed to seed alert: %v", err)
//	}

//	// 3. Initialize router and generate test token
//	router := setupRouter()
//	token := generateTestToken(userIDs[0].Hex())

//	// ==========================================
//	// Test Case 1: Successfully broadcast alert
//	// ==========================================
//	req, _ := http.NewRequest("POST", "/api/yard/"+yardID+"/alert/broadcast", nil)
//	req.Header.Set("Authorization", "Bearer "+token)

//	w := httptest.NewRecorder()
//	router.ServeHTTP(w, req)

//	if w.Code != http.StatusOK {
//		t.Fatalf("Expected status 200 on broadcast alert, got %d. Body: %s", w.Code, w.Body.String())
//	}

//	// Verify database record has updated run history and last run timestamp
//	var updatedAlert AlertDetails
//	err = database.Collection("alerts").FindOne(t.Context(), bson.M{"yard_id": yardID}).Decode(&updatedAlert)
//	if err != nil {
//		t.Fatalf("Failed to query updated alert from database: %v", err)
//	}

//	if len(updatedAlert.RunHistory) == 0 {
//		t.Errorf("Expected at least 1 run history entry in database after broadcast, got %d", len(updatedAlert.RunHistory))
//	}

//	lastRun := updatedAlert.RunHistory[len(updatedAlert.RunHistory)-1]
//	if strings.Contains(lastRun.Message, "Error") {
//		t.Fatalf("Broadcast logged errors: %s", lastRun.Message)
//	}
//}

//func TestTestAlert(t *testing.T) {
//	// 1. Setup test environment
//	_, userIDs, _, yardIDs, _, err := setupTest(t, 1, 1, 1, 0, time.Now().UTC())
//	if err != nil {
//		t.Fatalf("Failed to setup test data: %v", err)
//	}

//	yardID := yardIDs[0]

//	// 2. Seed initial alert details with test email and test phone
//	seededAlert := AlertDetails{
//		YardID:    yardID,
//		TestEmail: "test@gmail.com",
//		TestPhone: "+1000000001",
//		Messages: []AlertMessages{
//			{Language: "English", Message: "Txhais cov lus no sai sai"},
//		},
//		CreatedAt: time.Now().UTC(),
//		UpdatedAt: time.Now().UTC(),
//	}
//	_, err = database.Collection("alerts").InsertOne(t.Context(), seededAlert)
//	if err != nil {
//		t.Fatalf("Failed to seed alert: %v", err)
//	}

//	// 3. Initialize router and generate test token
//	router := setupRouter()
//	token := generateTestToken(userIDs[0].Hex())

//	// ==========================================
//	// Test Case 1: Successfully execute alert test
//	// ==========================================
//	req, _ := http.NewRequest("POST", "/api/yard/"+yardID+"/alert/testOne", nil)
//	req.Header.Set("Authorization", "Bearer "+token)

//	w := httptest.NewRecorder()
//	router.ServeHTTP(w, req)

//	if w.Code != http.StatusOK {
//		t.Fatalf("Expected status 200 on test alert, got %d. Body: %s", w.Code, w.Body.String())
//	}

//	// Verify database record has updated run history with test log
//	var updatedAlert AlertDetails
//	err = database.Collection("alerts").FindOne(t.Context(), bson.M{"yard_id": yardID}).Decode(&updatedAlert)
//	if err != nil {
//		t.Fatalf("Failed to query updated alert from database: %v", err)
//	}

//	if len(updatedAlert.RunHistory) == 0 {
//		t.Errorf("Expected at least 1 run history entry in database after test run, got %d", len(updatedAlert.RunHistory))
//	}
//}

//func TestTestAlertAll(t *testing.T) {
//	// 1. Setup test environment
//	_, userIDs, _, yardIDs, _, err := setupTest(t, 1, 1, 1, 0, time.Now().UTC())
//	if err != nil {
//		t.Fatalf("Failed to setup test data: %v", err)
//	}

//	yardID := yardIDs[0]

//	// 2. Seed initial alert details with distinct localized messages and users
//	seededAlert := AlertDetails{
//		YardID:   yardID,
//		CoolDown: 5 * time.Minute,
//		Messages: []AlertMessages{
//			{Language: "English", Message: "English operational test message"},
//			{Language: "Spanish", Message: "Mensaje de prueba operativo en español"},
//		},
//		Users: []AlertUserDetails{
//			{Email: "test@gmail.com", Phone: "+10000000001", Language: "Spanish"},
//			{Email: "test+test@gmail.com", Phone: "+10000000002", Language: "English"},
//		},
//		CreatedAt: time.Now().UTC(),
//		UpdatedAt: time.Now().UTC(),
//	}
//	_, err = database.Collection("alerts").InsertOne(t.Context(), seededAlert)
//	if err != nil {
//		t.Fatalf("Failed to seed alert: %v", err)
//	}

//	// 3. Initialize router and generate test token
//	router := setupRouter()
//	token := generateTestToken(userIDs[0].Hex())

//	// ==========================================
//	// Test Case: Execute testAll endpoint
//	// ==========================================
//	req, _ := http.NewRequest("POST", "/api/yard/"+yardID+"/alert/testAll", nil)
//	req.Header.Set("Authorization", "Bearer "+token)

//	w := httptest.NewRecorder()
//	router.ServeHTTP(w, req)

//	if w.Code != http.StatusOK {
//		t.Fatalf("Expected status 200 on testAll, got %d. Body: %s", w.Code, w.Body.String())
//	}

//	// Verify database record has updated run history and last run timestamp
//	var updatedAlert AlertDetails
//	err = database.Collection("alerts").FindOne(t.Context(), bson.M{"yard_id": yardID}).Decode(&updatedAlert)
//	if err != nil {
//		t.Fatalf("Failed to query updated alert from database: %v", err)
//	}

//	if len(updatedAlert.RunHistory) == 0 {
//		t.Errorf("Expected at least 1 run history entry in database after testAll broadcast, got 0")
//	}

//	lastRun := updatedAlert.RunHistory[len(updatedAlert.RunHistory)-1]
//	if strings.Contains(lastRun.Message, "Error") {
//		t.Fatalf("TestAll broadcast logged errors: %s", lastRun.Message)
//	}
//}
