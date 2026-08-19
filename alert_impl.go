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
	"strconv"
	"strings"
	"time"
	"unicode"

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
				YardID:       yardId,
				Messages:     []AlertMessages{},
				TestMessages: []AlertMessages{},
				LastRun:      time.Now().UTC(),
				CoolDown:     300000000000,
				TestEmail:    "",
				TestPhone:    "",
				RunHistory:   []AlertRunHistory{},
				Users:        []AlertUserDetails{},
				CreatedAt:    time.Now().UTC(),
				UpdatedAt:    time.Now().UTC(),
			}

			_, err = database.Collection("alerts").InsertOne(c, alert)
			return
		} else {
			return
		}
	}

	return
}

func resolveAlertMessages(oldMessages []AlertMessages, newText string, users []AlertUserDetails) ([]AlertMessages, error) {
	// If text hasn't changed and we already have messages, keep them (preserving translations)
	if len(oldMessages) > 0 && oldMessages[0].Message == newText {
		return oldMessages, nil
	}

	// Otherwise, reset with English and re-translate if there are users
	messages := []AlertMessages{{
		Language: "English",
		Message:  newText,
	}}

	if len(users) > 0 {
		translated, err := getMessageTranslations(users, newText)
		if err != nil {
			return nil, err
		}
		messages = append(messages, translated...)
	}

	return messages, nil
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

	// Resolve main messages and test messages using the helper
	messages, err := resolveAlertMessages(oldAlert.Messages, payload.Message, oldAlert.Users)
	if err != nil {
		return err
	}

	// Assuming oldAlert has a TestMessages field of type []AlertMessages
	testMessages, err := resolveAlertMessages(oldAlert.TestMessages, payload.TestMessage, oldAlert.Users)
	if err != nil {
		return err
	}

	alert := AlertDetails{
		Messages:     messages,
		TestMessages: testMessages,
		LastRun:      time.Unix(payload.LastRun, 0).UTC(),
		CoolDown:     payload.CoolDown,
		TestEmail:    payload.TestEmail,
		TestPhone:    payload.TestPhone,
		UpdatedAt:    time.Now().UTC(),
	}

	alert.Users = make([]AlertUserDetails, len(oldAlert.Users))
	copy(alert.Users, oldAlert.Users)

	found := false
	for i, u := range alert.Users {
		if normalizeAlertPhone(u.Phone) == normalizeAlertPhone(payload.TestPhone) || normalizeAlertEmail(u.Email) == normalizeAlertEmail(payload.TestEmail) {
			name := u.Name
			if name == "" {
				name = "Test User"
			}
			candidate := AlertUserDetails{
				Name:     name,
				Email:    payload.TestEmail,
				Phone:    payload.TestPhone,
				Language: u.Language,
			}
			if err := ensureUniqueAlertUser(alert.Users, candidate, i); err != nil {
				return err
			}
			alert.Users[i] = candidate
			found = true
			break
		}
	}

	if !found {
		candidate := AlertUserDetails{
			Name:     "Test User",
			Email:    payload.TestEmail,
			Phone:    payload.TestPhone,
			Language: "English",
		}
		if err := ensureUniqueAlertUser(alert.Users, candidate, -1); err != nil {
			return err
		}
		alert.Users = append(alert.Users, candidate)
	}

	update := bson.M{
		"$set": bson.M{
			"messages":      alert.Messages,
			"test_messages": alert.TestMessages,
			"last_run":      alert.LastRun,
			"cool_down":     alert.CoolDown,
			"test_email":    alert.TestEmail,
			"test_phone":    alert.TestPhone,
			"updated_at":    alert.UpdatedAt,
			"users":         alert.Users,
		},
		"$setOnInsert": bson.M{
			"created_at": time.Now().UTC(),
		},
	}

	filter := bson.M{"yard_id": yardId}
	_, err = database.Collection("alerts").UpdateOne(c, filter, update, options.UpdateOne().SetUpsert(true))

	return
}

var ErrDuplicateContact = errors.New("duplicate contact value")

func normalizeAlertEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeAlertPhone(value string) string {
	cleaned := strings.TrimSpace(value)
	var digits []rune
	for _, r := range cleaned {
		if unicode.IsDigit(r) || r == '+' {
			digits = append(digits, unicode.ToLower(r))
		}
	}
	return string(digits)
}

func isEmptyAlertContact(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == "" || strings.EqualFold(trimmed, "noEmail") || strings.EqualFold(trimmed, "noPhone")
}

func ensureUniqueAlertUser(users []AlertUserDetails, candidate AlertUserDetails, ignoreIndex int) error {
	candidateEmail := normalizeAlertEmail(candidate.Email)
	candidatePhone := normalizeAlertPhone(candidate.Phone)

	for i, existing := range users {
		if i == ignoreIndex {
			continue
		}
		if !isEmptyAlertContact(existing.Email) && candidateEmail != "" && normalizeAlertEmail(existing.Email) == candidateEmail {
			return fmt.Errorf("%w: email %s is already in use", ErrDuplicateContact, candidate.Email)
		}
		if !isEmptyAlertContact(existing.Phone) && candidatePhone != "" && normalizeAlertPhone(existing.Phone) == candidatePhone {
			return fmt.Errorf("%w: phone %s is already in use", ErrDuplicateContact, candidate.Phone)
		}
	}

	return nil
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
		"portuguese": "pt",
	}

	normalized := strings.ToLower(strings.TrimSpace(langName))
	code, exists := mapping[normalized]
	if !exists {
		return "", fmt.Errorf("unsupported language name: %s", langName)
	}

	return code, nil
}

func editUserAlertImpl(c *gin.Context, yardId string) (err error) {
	var newUser AlertUserDetails
	if err = c.ShouldBindJSON(&newUser); err != nil {
		return fmt.Errorf("failed to bind JSON: %w", err)
	}

	// 1. Retrieve existing alert details
	alert, err := getAlertImpl(c, yardId)
	if err != nil {
		return fmt.Errorf("failed to retrieve alert details: %w", err)
	}

	// Safely copy existing users to a new slice
	updatedUsers := make([]AlertUserDetails, len(alert.Users))
	copy(updatedUsers, alert.Users)

	found := false
	for i, existing := range updatedUsers {
		matchByEmail := !isEmptyAlertContact(newUser.Email) && normalizeAlertEmail(existing.Email) == normalizeAlertEmail(newUser.Email)
		matchByPhone := !isEmptyAlertContact(newUser.Phone) && normalizeAlertPhone(existing.Phone) == normalizeAlertPhone(newUser.Phone)

		if matchByEmail || matchByPhone {
			if err := ensureUniqueAlertUser(updatedUsers, newUser, i); err != nil {
				return err
			}
			updatedUsers[i] = newUser
			found = true
			break
		}
	}

	// 2. If no match was found, append as a new user
	if !found {
		if err := ensureUniqueAlertUser(updatedUsers, newUser, -1); err != nil {
			return err
		}
		updatedUsers = append(updatedUsers, newUser)
	}

	// Assign back to our alert struct
	alert.Users = updatedUsers

	// 3. Ensure ALL unique languages used by ANY user have a corresponding translation message
	if len(alert.Messages) == 0 {
		return fmt.Errorf("cannot translate: baseline English message does not exist in alert.Messages")
	}
	primaryMessage := alert.Messages[0].Message

	// Collect unique languages from all current users
	activeLanguages := make(map[string]bool)
	for _, u := range alert.Users {
		lang := strings.TrimSpace(u.Language)
		if lang != "" {
			activeLanguages[lang] = true
		}
	}

	// Check each active language against existing message translations
	for lang := range activeLanguages {
		if strings.EqualFold(lang, "english") {
			continue // English baseline assumed present
		}

		langAlreadyTranslated := false
		for _, msg := range alert.Messages {
			if strings.EqualFold(msg.Language, lang) {
				langAlreadyTranslated = true
				break
			}
		}

		// If a user requires this language but it has no translation yet, translate it now
		if !langAlreadyTranslated {
			translatedText, err := translateMessage(primaryMessage, lang)
			if err != nil {
				return fmt.Errorf("translation failed for language %s: %w", lang, err)
			}

			alert.Messages = append(alert.Messages, AlertMessages{
				Language: lang,
				Message:  translatedText,
			})
		}
	}

	// 4. Persist the updated users and messages back to MongoDB
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

	return nil
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
	dateStr := c.Param("date")

	// Parse the incoming millisecond timestamp string into a time.Time object
	millis, err := strconv.ParseInt(dateStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid history date format: %w", err)
	}
	targetDate := time.Unix(0, millis*int64(time.Millisecond)).UTC()

	alert, err := getAlertImpl(c, yardId)
	if err != nil {
		return fmt.Errorf("failed to retrieve alert details: %w", err)
	}

	// Filter out the run history entries that match the specified date
	var updatedHistory []AlertRunHistory
	for _, entry := range alert.RunHistory {
		// Using Unix milli comparison is safer than .Equal() to avoid tiny timezone/sub-millisecond mismatches
		if entry.Date.UnixMilli() != targetDate.UnixMilli() {
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

	return nil
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

	// Choose which message collection to use based on the testing flag
	targetMessages := alert.Messages
	defaultMessage := "This is an alert but your specified message is not loading. Please follow proper alert procedures and contact your supervisor for more information."

	if testing {
		targetMessages = alert.TestMessages
		defaultMessage = "This is a TEST alert to confirm that the system is working. Please continue working as usual. If you have any questions, please contact your supervisor."
	}

	// Pre-load available translations into a lookup map
	messageMap := make(map[string]string)
	if len(targetMessages) > 0 {
		defaultMessage = targetMessages[0].Message
		for _, m := range targetMessages {
			messageMap[strings.ToLower(strings.TrimSpace(m.Language))] = m.Message
		}
	}

	for _, user := range alert.Users {
		email := strings.TrimSpace(user.Email)
		phone := strings.TrimSpace(user.Phone)

		// Use user's actual specified language and look up the appropriate message (Test or Real)
		lang := strings.TrimSpace(user.Language)
		if lang == "" {
			lang = "English"
		}
		langKey := strings.ToLower(lang)

		msgText, found := messageMap[langKey]
		if !found {
			fmt.Printf("[DEBUG] No translation found for langKey=%q, falling back to default\n", langKey)
			msgText = defaultMessage
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
