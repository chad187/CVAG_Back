package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

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
		Message:   "new",
		CoolDown:  10,
		TestEmail: "alerts@example.com",
		TestPhone: "555-0199",
		LastRun:   time.Now().UTC().Unix(),
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
