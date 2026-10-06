package domain

import "time"

type Role string

const (
	RoleAdmin      Role = "ADMIN"
	RoleDispatcher Role = "DISPATCHER"
	RoleCourier    Role = "COURIER"
	RoleCustomer   Role = "CUSTOMER"
)

type Status string

const (
	StatusCreated        Status = "CREATED"
	StatusPickedUp       Status = "PICKED_UP"
	StatusInTransit      Status = "IN_TRANSIT"
	StatusOutForDelivery Status = "OUT_FOR_DELIVERY"
	StatusDelivered      Status = "DELIVERED"
	StatusCancelled      Status = "CANCELLED"
)

func CanTransition(from, to Status) bool {
	switch from {
	case StatusCreated:
		return to == StatusPickedUp || to == StatusCancelled
	case StatusPickedUp:
		return to == StatusInTransit
	case StatusInTransit:
		return to == StatusOutForDelivery
	case StatusOutForDelivery:
		return to == StatusDelivered
	default:
		return false
	}
}

type User struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Name         string    `json:"name"`
	Role         Role      `json:"role"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
type Shipment struct {
	ID                int64      `json:"id"`
	TrackingNumber    string     `json:"tracking_number"`
	SenderName        string     `json:"sender_name"`
	SenderPhone       string     `json:"sender_phone"`
	RecipientName     string     `json:"recipient_name"`
	RecipientPhone    string     `json:"recipient_phone"`
	Origin            string     `json:"origin"`
	Destination       string     `json:"destination"`
	Status            Status     `json:"status"`
	CustomerID        int64      `json:"customer_id"`
	AssignedCourierID *int64     `json:"assigned_courier_id"`
	CourierName       string     `json:"courier_name,omitempty"`
	ExpectedDelivery  *time.Time `json:"expected_delivery"`
	DeliveredAt       *time.Time `json:"delivered_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}
type TrackingEvent struct {
	ID         int64     `json:"id"`
	ShipmentID int64     `json:"shipment_id"`
	Status     Status    `json:"status"`
	Location   string    `json:"location"`
	Notes      string    `json:"notes"`
	RecordedBy int64     `json:"recorded_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}
type PublicTracking struct {
	TrackingNumber   string          `json:"tracking_number"`
	Origin           string          `json:"origin"`
	Destination      string          `json:"destination"`
	Status           Status          `json:"status"`
	ExpectedDelivery *time.Time      `json:"expected_delivery"`
	DeliveredAt      *time.Time      `json:"delivered_at"`
	Events           []TrackingEvent `json:"events"`
}
type Dashboard struct {
	Total          int64 `json:"total"`
	Created        int64 `json:"created"`
	InTransit      int64 `json:"in_transit"`
	OutForDelivery int64 `json:"out_for_delivery"`
	Delivered      int64 `json:"delivered"`
	Cancelled      int64 `json:"cancelled"`
}
type ListParams struct {
	Page, Limit int
	Search      string
	Status      Status
	ActorID     int64
	ActorRole   Role
}
type Page struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

func NewPage(p ListParams, t int64) Page {
	n := 0
	if t > 0 {
		n = int((t + int64(p.Limit) - 1) / int64(p.Limit))
	}
	return Page{p.Page, p.Limit, t, n}
}
