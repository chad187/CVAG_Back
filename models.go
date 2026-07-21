package main

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Node struct {
	ID           string      `json:"id" bson:"_id"`
	Name         string      `json:"name" bson:"name"`
	Temp         float64     `json:"temp" bson:"temp"`
	Status       string      `json:"status" bson:"status"` // OK, OFFLINE, OVERHEATING, LOW_BATTERY
	Version      string      `json:"version" bson:"version"`
	Battery      float64     `json:"battery" bson:"battery"`
	Warning_temp int         `json:"warning_temp" bson:"warning_temp"`
	Location     Coordinates `json:"location" bson:"location"`
	CreatedAt    time.Time   `json:"created_at" bson:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at" bson:"updated_at"`
}

type Coordinates struct {
	Lat float64 `json:"lat" bson:"lat"`
	Lng float64 `json:"lng" bson:"lng"`
}

type NodeSummary struct {
	Node    Node            `json:"node"`
	History []HistoryRecord `json:"history"`
	Update  FirmwareUpdate  `json:"firmware_update,omitempty"`
}

type Company struct {
	ID        string    `json:"id" bson:"_id"`
	CreatedAt time.Time `json:"created_at" bson:"created_at"`
	UpdatedAt time.Time `json:"updated_at" bson:"updated_at"`
}

type Yard struct {
	ID        string    `json:"id" bson:"_id"`
	CreatedAt time.Time `json:"created_at" bson:"created_at"`
	UpdatedAt time.Time `json:"updated_at" bson:"updated_at"`
}

type HistoryRecord struct {
	NodeID    string    `json:"node_id" bson:"node_id"`
	Temp      float64   `json:"temp" bson:"temp"`
	Timestamp time.Time `json:"timestamp" bson:"timestamp"`
}

type HeartbeatPayload struct {
	Current Node   `json:"current"`
	History []Node `json:"history"`
}

type SystemMetrics struct {
	YardID       string    `json:"yard_id" bson:"_id"` // Using compound ID as PK
	TempC        float64   `json:"temp_c" bson:"temp_c"`
	FanRPM       int       `json:"fan_rpm" bson:"fan_rpm"`
	LoadAvg      float64   `json:"load_avg" bson:"load_avg"`
	RamUsedPct   float64   `json:"ram_used_pct" bson:"ram_used_pct"`
	DiskUsedPct  float64   `json:"disk_used_pct" bson:"disk_used_pct"`
	Goroutines   int       `json:"goroutines" bson:"goroutines"`
	UpdatedAt    time.Time `json:"updated_at" bson:"updated_at"`
	SystemUptime float64   `json:"system_uptime" bson:"system_uptime"`
}

type FirmwareUpdate struct {
	NodeID       string    `json:"node_id" bson:"_id"`
	UpdateStatus string    `json:"update_status" bson:"update_status"`
	Version      string    `json:"version" bson:"version" form:"version"`          // Added form tag
	SSID         string    `json:"ssid" bson:"ssid" form:"ssid"`                   // Added form tag
	Password     string    `json:"password" bson:"password" form:"password"`       // Added form tag
	Firmware     []byte    `json:"firmware" bson:"firmware"`                       // Extracted manually via io.ReadAll
	UpdateURL    string    `json:"update_url" bson:"update_url" form:"update_url"` // Added form tag
	UpdateAt     time.Time `json:"update_at" bson:"update_at"`
	CreatedAt    time.Time `json:"created_at" bson:"created_at"`
}

type User struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	SysAdmin   bool               `bson:"sys_admin" json:"sys_admin"`
	CompanyIDs []string           `bson:"company_ids" json:"company_ids"` // <-- Look for a typo here!
	Email      string             `bson:"email" json:"email"`
	Name       string             `bson:"name" json:"name"`
	Provider   string             `bson:"provider" json:"provider"`
	ProviderID string             `bson:"provider_id" json:"provider_id"`
	Picture    string             `bson:"picture,omitempty" json:"picture,omitempty"`
	CreatedAt  time.Time          `bson:"created_at" json:"created_at"`
	LastLogin  time.Time          `bson:"last_login" json:"last_login"`
}

type CompositeID struct {
	CompanyID string
	YardID    string
	NodeID    string
}

type YardSummary struct {
	ID             string `json:"id"`
	NodeCount      int    `json:"node_count"`
	UnhealthyCount int    `json:"unhealthy_count"`
}

type CompanySummary struct {
	ID           string `json:"id"`
	YardCount    int    `json:"yard_count"`
	NodeCount    int    `json:"node_count"`
	HasBadStatus bool   `json:"has_bad_status"`
}

type socialProfile struct {
	Email             string `json:"email"`
	Name              string `json:"name"`
	Sub               string `json:"sub"`
	Picture           string `json:"picture"`
	PreferredUsername string `json:"preferred_username"`
}

type AuthClaims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// CancelUpdateResult represents the result of attempting to cancel an update
type CancelUpdateResult struct {
	Cancelled     bool
	NotFound      bool
	AlreadyQueued bool
}
