package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func generateTestToken(userID string) string {
	// This will now match the secret set in TestMain
	secret := []byte(os.Getenv("AUTH_JWT_SECRET"))

	claims := AuthClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	t, _ := token.SignedString(secret)
	return t
}

func setupTest(t *testing.T, userCount, companyCount, yardCount, nodeCount int, baseTime time.Time) (sysAdminID primitive.ObjectID, userIDs []primitive.ObjectID, companyIDs []string, yardIDs []string, nodeIDs []string, err error) {
	wipeData(t)
	ctx := context.TODO()

	// 1. ALWAYS Create SysAdmin
	adminObjID := primitive.NewObjectID()
	sysAdminID = adminObjID // Now returning the actual ObjectID

	_, err = database.Collection("users").InsertOne(ctx, bson.M{
		"_id":         adminObjID,
		"sys_admin":   true,
		"email":       "admin@test.com",
		"company_ids": []string{},
		"provider":    "test",
		"provider_id": "admin-id",
	})
	if err != nil {
		return primitive.NilObjectID, nil, nil, nil, nil, fmt.Errorf("failed to create admin: %w", err)
	}

	// 2. Loop for Regular Users
	for i := 0; i < userCount; i++ {
		userObjID := primitive.NewObjectID()
		userIDs = append(userIDs, userObjID) // Storing the raw ObjectIDs
		userHexID := userObjID.Hex()

		var currentUsersCompanies []string

		// 3. Create Companies for this User FIRST
		for j := 0; j < companyCount; j++ {
			cID := fmt.Sprintf("U%d_COMP_%d", i, j)
			currentUsersCompanies = append(currentUsersCompanies, cID)
			companyIDs = append(companyIDs, cID)

			_, err := database.Collection("companies").InsertOne(ctx, bson.M{
				"_id":       cID,
				"name":      "Company " + cID,
				"owner_ids": []string{userHexID},
			})
			if err != nil {
				return primitive.NilObjectID, nil, nil, nil, nil, err
			}

			// 4. Create Yards for this Company
			for k := 0; k < yardCount; k++ {
				yID := fmt.Sprintf("%s:YARD_%d", cID, k)
				yardIDs = append(yardIDs, yID)
				_, err := database.Collection("yards").InsertOne(ctx, bson.M{
					"_id":       yID,
					"owner_ids": []string{userHexID},
				})
				if err != nil {
					return primitive.NilObjectID, nil, nil, nil, nil, err
				}

				// 5. Create Nodes for this Yard
				for l := 0; l < nodeCount; l++ {
					nID := fmt.Sprintf("%s:NODE_%d", yID, l)
					nodeIDs = append(nodeIDs, nID)
					_, err := database.Collection("nodes").InsertOne(ctx, bson.M{
						"_id":        nID,
						"status":     KEY_OK,
						"temp":       25,
						"battery":    90,
						"updated_At": baseTime,
					})
					if err != nil {
						return primitive.NilObjectID, nil, nil, nil, nil, err
					}
				}
			}
		}

		// 6. NOW Create the User with the pre-built list of Company IDs
		_, err = database.Collection("users").InsertOne(ctx, bson.M{
			"_id":         userObjID,
			"sys_admin":   false,
			"email":       fmt.Sprintf("user%d@test.com", i),
			"company_ids": currentUsersCompanies,
			"provider":    "test",
			"provider_id": fmt.Sprintf("reg-user-%d", i),
		})
		if err != nil {
			return primitive.NilObjectID, nil, nil, nil, nil, err
		}
	}

	return sysAdminID, userIDs, companyIDs, yardIDs, nodeIDs, nil
}

func wipeData(t *testing.T) {
	// If the database name isn't our test name, STOP IMMEDIATELY
	if !strings.Contains(database.Name(), "TEST") {
		msg := fmt.Sprintf("SAFETY PREVENTED DATA WIPE: Attempted to wipe non-test DB: %s", database.Name())
		if t != nil {
			t.Fatalf("%s", msg)
		} else {
			log.Fatal(msg)
		}
	}

	ctx := context.TODO()
	collections, _ := database.ListCollectionNames(ctx, bson.M{})

	for _, colName := range collections {
		// Clear documents
		database.Collection(colName).DeleteMany(ctx, bson.M{})
	}
}

func getAuthHeader(userID primitive.ObjectID) string {
	token := generateTestToken(userID.Hex())
	return "Bearer " + token
}
