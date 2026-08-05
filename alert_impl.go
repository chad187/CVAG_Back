package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func getAlertImpl(c *gin.Context, yardId string) (alert AlertDetails, err error) {

	filter := bson.M{"yard_id": yardId}

	err = database.Collection("alerts").FindOne(c, filter).Decode(&alert)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			alert = AlertDetails{
				YardID:     yardId,
				Messages:   []AlertMessages{},
				LastRun:    time.Now().UTC(),
				CoolDown:   300000000000,
				TestEmail:  "",
				TestPhone:  "",
				RunHistory: []AlertRunHistory{},
				Users:      []AlertUserDetails{},
				CreatedAt:  time.Now().UTC(),
				UpdatedAt:  time.Now().UTC(),
			}

			_, err = database.Collection("alerts").InsertOne(c, alert)
			return
		} else {
			return
		}
	}

	return
}

func postAlertImpl(c *gin.Context, yardId string) (err error) {

	var payload AlertPostPayload

	if err = c.ShouldBindJSON(&payload); err != nil {
		return
	}

	oldAlert, err := getAlertImpl(c, yardId)
	if err != nil {
		return
	}

	alert := AlertDetails{
		Messages: []AlertMessages{{
			Language: "English",
			Message:  payload.Message,
		}},
	}

	if len(oldAlert.Messages) > 0 && oldAlert.Messages[0].Message != payload.Message {
		messages, err := getMessageTranslations(oldAlert.Users, payload.Message)
		if err != nil {
			return err
		}

		alert.Messages = append(alert.Messages, messages...)
	}

	alert.LastRun = time.Unix(payload.LastRun, 0).UTC()
	alert.CoolDown = payload.CoolDown
	alert.TestEmail = payload.TestEmail
	alert.TestPhone = payload.TestPhone
	alert.UpdatedAt = time.Now().UTC()

	alert.Users = make([]AlertUserDetails, len(oldAlert.Users))
	copy(alert.Users, oldAlert.Users)

	found := false
	for i, u := range alert.Users {
		// Check by phone (or email) to see if this user already exists
		if u.Phone == payload.TestPhone {
			// Replace with the updated details
			alert.Users[i] = AlertUserDetails{
				Name:     "Test User", // Or keep the existing name if you have it stored
				Email:    payload.TestEmail,
				Phone:    payload.TestPhone,
				Language: u.Language, // Preserve existing language preference if set
			}
			found = true
			break
		}
	}

	// If not found in the existing list, append them as a new entry
	if !found {
		alert.Users = append(alert.Users, AlertUserDetails{
			Name:     "Test User",
			Email:    payload.TestEmail,
			Phone:    payload.TestPhone,
			Language: "English",
		})
	}

	update := bson.M{
		"$set": bson.M{
			"messages":   alert.Messages,
			"last_run":   alert.LastRun,
			"cool_down":  alert.CoolDown,
			"test_email": alert.TestEmail,
			"test_phone": alert.TestPhone,
			"updated_at": alert.UpdatedAt,
			"users":      alert.Users,
		},
		"$setOnInsert": bson.M{
			"created_at": time.Now().UTC(),
		},
	}

	filter := bson.M{"yard_id": yardId}

	_, err = database.Collection("alerts").UpdateOne(c, filter, update, options.UpdateOne().SetUpsert(true))

	return
}

func getMessageTranslations(users []AlertUserDetails, message string) ([]AlertMessages, error) {
	var messages []AlertMessages
	seenLanguages := make(map[string]bool)

	for _, user := range users {
		normalizedLang := strings.ToLower(strings.TrimSpace(user.Language))
		if normalizedLang != "english" && !seenLanguages[normalizedLang] {
			seenLanguages[normalizedLang] = true

			translatedMessage, err := translateMessage(message, user.Language)
			if err != nil {
				return nil, err
			}

			messages = append(messages, AlertMessages{
				Language: user.Language,
				Message:  translatedMessage,
			})
		}
	}
	return messages, nil
}

func translateMessage(text string, langName string) (string, error) {
	targetLangCode, err := getLanguageCode(langName)
	if err != nil {
		return "", err
	}

	// MyMemory uses a simple GET request with query parameters
	apiURL := fmt.Sprintf(
		"https://api.mymemory.translated.net/get?q=%s&langpair=en|%s",
		url.QueryEscape(text),
		url.QueryEscape(targetLangCode),
	)

	resp, err := http.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("translation request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("api returned status code %d", resp.StatusCode)
	}

	var result MyMemoryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if result.ResponseStatus != 200 {
		return "", fmt.Errorf("translation service returned error status: %d", result.ResponseStatus)
	}

	return result.ResponseData.TranslatedText, nil
}

func getLanguageCode(langName string) (string, error) {
	mapping := map[string]string{
		"english":    "en",
		"spanish":    "es",
		"mandarin":   "zh",
		"tagalog":    "tl",
		"vietnamese": "vi",
		"arabic":     "ar",
		"french":     "fr",
		"korean":     "ko",
		"portuguese": "pt",
		"russian":    "ru",
	}

	normalized := strings.ToLower(strings.TrimSpace(langName))
	code, exists := mapping[normalized]
	if !exists {
		return "", fmt.Errorf("unsupported language name: %s", langName)
	}

	return code, nil
}

func addUserAlertImpl(c *gin.Context, yardId string) (err error) {

	var newUser AlertUserDetails
	if err = c.ShouldBindJSON(&newUser); err != nil {
		return fmt.Errorf("failed to bind JSON: %w", err)
	}

	// 1. Retrieve existing alert details (creates default record if none exists)
	alert, err := getAlertImpl(c, yardId)
	if err != nil {
		return fmt.Errorf("failed to retrieve alert details: %w", err)
	}

	// Safely copy existing users to a new slice so we don't mutate in-memory state prematurely
	updatedUsers := make([]AlertUserDetails, len(alert.Users))
	copy(updatedUsers, alert.Users)

	found := false
	for i, existing := range updatedUsers {
		// Check if either email or phone matches an existing user record
		matchByEmail := newUser.Email != "" && existing.Email == newUser.Email
		matchByPhone := newUser.Phone != "" && existing.Phone == newUser.Phone

		if matchByEmail || matchByPhone {
			// Preserve language preference if not provided in the new payload
			if newUser.Language == "" {
				newUser.Language = existing.Language
			}

			updatedUsers[i] = newUser
			found = true
			break
		}
	}

	// 2. If no match was found, append as a new user
	if !found {
		updatedUsers = append(updatedUsers, newUser)
	}

	// Assign back to our alert struct
	alert.Users = updatedUsers

	// 4. Check if the user's language needs a new translation added to messages
	normalizedLang := strings.ToLower(strings.TrimSpace(newUser.Language))
	if normalizedLang != "english" && len(alert.Messages) > 0 {
		langAlreadyTranslated := false
		for _, msg := range alert.Messages {
			if strings.EqualFold(msg.Language, newUser.Language) {
				langAlreadyTranslated = true
				break
			}
		}

		// If this language isn't translated yet, translate the primary (English) message
		if !langAlreadyTranslated {
			primaryMessage := alert.Messages[0].Message
			translatedText, err := translateMessage(primaryMessage, newUser.Language)
			if err != nil {
				return fmt.Errorf("translation failed: %w", err)
			}

			alert.Messages = append(alert.Messages, AlertMessages{
				Language: newUser.Language,
				Message:  translatedText,
			})
		}
	}

	// 5. Persist the updated users and messages back to MongoDB
	alert.UpdatedAt = time.Now().UTC()
	update := bson.M{
		"$set": bson.M{
			"users":      alert.Users,
			"messages":   alert.Messages,
			"updated_at": alert.UpdatedAt,
		},
	}

	filter := bson.M{"yard_id": yardId}
	_, err = database.Collection("alerts").UpdateOne(c, filter, update)
	if err != nil {
		return fmt.Errorf("failed to save new user to database: %w", err)
	}

	return
}

func deleteUserAlertImpl(c *gin.Context, yardId string) (err error) {
	var payload struct {
		Email string `json:"email"`
	}
	if err = c.ShouldBindJSON(&payload); err != nil {
		return fmt.Errorf("failed to bind JSON: %w", err)
	}

	if payload.Email == "" {
		return fmt.Errorf("email is required to delete a user alert")
	}

	// 1. Retrieve existing alert details
	alert, err := getAlertImpl(c, yardId)
	if err != nil {
		return fmt.Errorf("failed to retrieve alert details: %w", err)
	}

	// 2. Filter out the user with the matching email
	found := false
	var updatedUsers []AlertUserDetails
	for _, user := range alert.Users {
		if user.Email == payload.Email {
			found = true
			continue
		}
		updatedUsers = append(updatedUsers, user)
	}

	if !found {
		return fmt.Errorf("user with email %s not found for this alert", payload.Email)
	}

	alert.Users = updatedUsers
	alert.UpdatedAt = time.Now().UTC()

	// 3. Persist the updated users list back to MongoDB
	update := bson.M{
		"$set": bson.M{
			"users":      alert.Users,
			"updated_at": alert.UpdatedAt,
		},
	}

	filter := bson.M{"yard_id": yardId}
	_, err = database.Collection("alerts").UpdateOne(c, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update users in database: %w", err)
	}

	return
}

func deleteAlertHistoryImpl(c *gin.Context, yardId string) (err error) {
	var payload struct {
		Date time.Time `json:"date"`
	}

	if err = c.ShouldBindJSON(&payload); err != nil {
		return fmt.Errorf("failed to bind JSON: %w", err)
	}

	alert, err := getAlertImpl(c, yardId)
	if err != nil {
		return fmt.Errorf("failed to retrieve alert details: %w", err)
	}

	// Filter out the run history entries that match the specified date
	var updatedHistory []AlertRunHistory
	for _, entry := range alert.RunHistory {
		if !entry.Date.Equal(payload.Date) {
			updatedHistory = append(updatedHistory, entry)
		}
	}
	alert.RunHistory = updatedHistory

	alert.UpdatedAt = time.Now().UTC()

	// Persist the updated run history back to MongoDB
	update := bson.M{
		"$set": bson.M{
			"run_history": alert.RunHistory,
			"updated_at":  alert.UpdatedAt,
		},
	}

	filter := bson.M{"yard_id": yardId}
	_, err = database.Collection("alerts").UpdateOne(c, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update alert history in database: %w", err)
	}

	return
}

func broadcastAlertImpl(c *gin.Context, yardId string, testing bool) (remaining float64, err error) {
	// 1. Retrieve existing alert details
	alert, err := getAlertImpl(c, yardId)
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve alert details: %w", err)
	}

	// 2. Rate Limit Check (cool-down)
	now := time.Now().UTC()
	if !alert.LastRun.IsZero() {
		timeDiffMinutes := now.Sub(alert.LastRun).Minutes()
		coolDownMinutes := alert.CoolDown.Minutes()
		if timeDiffMinutes < coolDownMinutes {
			remaining := coolDownMinutes - timeDiffMinutes
			return remaining, fmt.Errorf("rate limit exceeded: %.1f minutes remaining", remaining)
		}
	}

	// 3. Extract Roster from Alert Users
	type langGroup struct {
		emails  []string
		phones  []string
		message string
		lang    string
	}

	groups := make(map[string]*langGroup)

	// Pre-load available translations into a lookup map
	messageMap := make(map[string]string)
	defaultMessage := "This is an alert but your specified message is not loading. Please follow proper alert procedures and contact your supervisor for more information."
	if len(alert.Messages) > 0 {
		defaultMessage = alert.Messages[0].Message
		for _, m := range alert.Messages {
			messageMap[strings.ToLower(strings.TrimSpace(m.Language))] = m.Message
		}
	}

	for _, user := range alert.Users {
		email := strings.TrimSpace(user.Email)
		phone := strings.TrimSpace(user.Phone)

		var langKey, msgText string

		if testing {
			// Force English and default message for all users during testing
			langKey = "english"
			msgText = "This is a TEST alert to confirm that the system is working. Please continue working as usual. If you have any questions, please contact your supervisor."
		} else {
			// Use user's actual specified language and localized message
			lang := strings.TrimSpace(user.Language)
			if lang == "" {
				lang = "English"
			}
			langKey = strings.ToLower(lang)
			var found bool
			msgText, found = messageMap[langKey]
			if !found {
				fmt.Printf("[DEBUG] No translation found for langKey=%q, falling back to English default\n", langKey)
				msgText = defaultMessage
			}
		}

		group, exists := groups[langKey]
		if !exists {
			group = &langGroup{message: msgText, lang: langKey}
			groups[langKey] = group
		}

		if email != "" {
			group.emails = append(group.emails, email)
		}
		if phone != "" {
			if !strings.HasPrefix(phone, "+") {
				phone = "+" + phone
			}
			group.phones = append(group.phones, phone)
		}
	}

	var emailSuccess, twilioSuccess bool
	var statusMessages []string

	// Iterate through language groups and dispatch localized batches
	for _, group := range groups {
		// Isolate current group's message and language to prevent cross-contamination
		currentMsg := group.message
		currentLang := group.lang

		if len(group.emails) > 0 {
			if emailErr := sendCloudEmailBatch(group.emails, currentMsg); emailErr == nil {
				emailSuccess = true
			} else {
				statusMessages = append(statusMessages, "Resend Error: "+emailErr.Error())
			}
		}

		if len(group.phones) > 0 {
			// Fetch explicit code for this specific loop iteration
			langCode, langErr := getLanguageCode(currentLang)
			if langErr != nil {
				statusMessages = append(statusMessages, "Language mapping warning: "+langErr.Error())
				langCode = "en"
			}

			// Dispatch with explicit per-group parameters
			if twilioErr := triggerTwilioCloudFunction(group.phones, currentMsg, langCode); twilioErr == nil {
				twilioSuccess = true
			} else {
				statusMessages = append(statusMessages, "Twilio Error: "+twilioErr.Error())
			}
		}
	}

	if emailSuccess || twilioSuccess {
		alert.LastRun = now
		statusMessages = append(statusMessages, "At least 1 email/text/call was successful")
	} else {
		statusMessages = append(statusMessages, "no error but nothing was sent")
	}

	// Append run history log
	historyEntry := AlertRunHistory{
		Date:    now,
		Message: strings.Join(statusMessages, " | "),
	}
	alert.RunHistory = append(alert.RunHistory, historyEntry)
	alert.UpdatedAt = now

	// Persist changes back to MongoDB
	update := bson.M{
		"$set": bson.M{
			"last_run":    alert.LastRun,
			"run_history": alert.RunHistory,
			"updated_at":  alert.UpdatedAt,
		},
	}

	filter := bson.M{"yard_id": yardId}
	_, err = database.Collection("alerts").UpdateOne(c, filter, update)
	if err != nil {
		return 0, fmt.Errorf("failed to update alert run state in database: %w", err)
	}

	hasTargets := false
	for _, group := range groups {
		if len(group.emails) > 0 || len(group.phones) > 0 {
			hasTargets = true
			break
		}
	}

	if !emailSuccess && !twilioSuccess && hasTargets {
		return 0, fmt.Errorf("broadcast failed: %s", strings.Join(statusMessages, " | "))
	}

	return 0, nil
}

func testAlertOneImpl(c *gin.Context, yardId string) (err error) {
	// 1. Retrieve existing alert details
	alert, err := getAlertImpl(c, yardId)
	if err != nil {
		return fmt.Errorf("failed to retrieve alert details: %w", err)
	}

	var statusMessages []string
	var emailSuccess, twilioSuccess bool

	// Determine message text (defaults to the first message configured, or fallback)
	messageText := "This is a test alert."
	if len(alert.Messages) > 0 {
		messageText = alert.Messages[0].Message
	}

	// 2. Test Email Dispatch
	if alert.TestEmail != "" {
		emailList := []string{strings.TrimSpace(alert.TestEmail)}
		if emailErr := sendCloudEmailBatch(emailList, messageText); emailErr == nil {
			emailSuccess = true
		} else {
			statusMessages = append(statusMessages, "Test Email Error: "+emailErr.Error())
		}
	} else {
		statusMessages = append(statusMessages, "Test Warning: TestEmail is empty")
	}

	// 3. Test Phone Dispatch
	if alert.TestPhone != "" {
		phone := strings.TrimSpace(alert.TestPhone)
		if !strings.HasPrefix(phone, "+") {
			phone = "+" + phone
		}
		phoneList := []string{phone}
		if twilioErr := triggerTwilioCloudFunction(phoneList, messageText, "English"); twilioErr == nil {
			twilioSuccess = true
		} else {
			statusMessages = append(statusMessages, "Test Twilio Error: "+twilioErr.Error())
		}
	} else {
		statusMessages = append(statusMessages, "Test Warning: TestPhone is empty")
	}

	now := time.Now().UTC()
	var logMessage string
	if len(statusMessages) > 0 {
		logMessage = "TEST One RUN | " + strings.Join(statusMessages, " | ")
	} else {
		logMessage = "TEST One RUN | Success"
	}

	alert.RunHistory = append(alert.RunHistory, AlertRunHistory{
		Date:    now,
		Message: logMessage,
	})
	alert.UpdatedAt = now

	update := bson.M{
		"$set": bson.M{
			"run_history": alert.RunHistory,
			"updated_at":  alert.UpdatedAt,
		},
	}

	filter := bson.M{"yard_id": yardId}
	_, err = database.Collection("alerts").UpdateOne(c, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update test alert history in database: %w", err)
	}

	if !emailSuccess && !twilioSuccess {
		return fmt.Errorf("test broadcast failed: %s", logMessage)
	}

	return nil
}

func sendCloudEmailBatch(emailsArray []string, messageText string) error {
	urlStr := "https://api.resend.com/emails"

	payloadData := map[string]interface{}{
		"from":    "CVAG Lookout <CVAGLookout@" + os.Getenv("EMAIL_DOMAIN") + ">",
		"to":      emailsArray,
		"subject": "Company Wide Alert",
		"text":    messageText,
	}

	jsonBody, err := json.Marshal(payloadData)
	if err != nil {
		return fmt.Errorf("failed to marshal email payload: %w", err)
	}

	req, err := http.NewRequest("POST", urlStr, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create email request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+os.Getenv("RESEND_API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send email request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	fmt.Printf("[DEBUG] Resend Response Status: %d | Body: %s\n", resp.StatusCode, string(bodyBytes))

	if resp.StatusCode >= 300 {
		return fmt.Errorf("resend API returned status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

func triggerTwilioCloudFunction(phones []string, messageText string, language string) error {
	twilioURL := os.Getenv("TWILIO_FUNC_URL")
	twilioPass := os.Getenv("API_PASSWORD")
	twilioFrom := os.Getenv("TWILIO_NUMBER")

	payload := map[string]interface{}{
		"password": twilioPass,
		"numbers":  phones,
		"from":     twilioFrom,
		"message":  messageText,
		"language": language,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal twilio payload: %w", err)
	}

	req, err := http.NewRequest("POST", twilioURL, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return fmt.Errorf("failed to create twilio request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send twilio request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	fmt.Printf("[DEBUG] Twilio Response Status: %d | Body: %s\n", resp.StatusCode, string(bodyBytes))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("twilio function returned status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}
