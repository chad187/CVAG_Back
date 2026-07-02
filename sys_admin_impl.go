package main

import (
	"context"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// GetNonAdminUsers returns all users where sys_admin is not true
func GetNonAdminUsers(ctx context.Context) ([]User, error) {
	var users []User
	filter := bson.M{"sys_admin": bson.M{"$ne": true}}

	cursor, err := database.Collection("users").Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	if err = cursor.All(ctx, &users); err != nil {
		return nil, err
	}

	return users, nil
}

// GetUserByID fetches a user by their ObjectID
func GetUserByID(ctx context.Context, userID primitive.ObjectID) (User, error) {
	var user User
	err := database.Collection("users").FindOne(ctx, bson.M{"_id": userID}).Decode(&user)
	return user, err
}

// UpdateUserCompanies updates a user's company_ids list
func UpdateUserCompanies(ctx context.Context, userID primitive.ObjectID, companyID string, remove bool) error {
	filter := bson.M{"_id": userID}
	update := bson.M{"$addToSet": bson.M{"company_ids": companyID}}
	if remove {
		update = bson.M{"$pull": bson.M{"company_ids": companyID}}
	}

	result, err := database.Collection("users").UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}

	return nil
}

// DeleteUserByID deletes a user by their ObjectID
func DeleteUserByID(ctx context.Context, userID primitive.ObjectID) error {
	result, err := database.Collection("users").DeleteOne(ctx, bson.M{"_id": userID})
	if err != nil {
		return err
	}

	if result.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}

	return nil
}
