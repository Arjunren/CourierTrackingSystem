package store

import (
	"context"
	"errors"
	"github.com/Arjunren/courier-tracking-system/internal/domain"
	"time"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("conflict")
	ErrInvalidTransition = errors.New("invalid transition")
)

type Store interface {
	UserByEmail(context.Context, string) (domain.User, error)
	UserByID(context.Context, int64) (domain.User, error)
	CreateUser(context.Context, domain.User) (domain.User, error)
	CreateSessionToken(context.Context, string, int64, time.Time) error
	SessionActive(context.Context, string, int64) (bool, error)
	RevokeSession(context.Context, string, int64) error
	CreateShipment(context.Context, domain.Shipment) (domain.Shipment, error)
	ShipmentByID(context.Context, int64) (domain.Shipment, error)
	ListShipments(context.Context, domain.ListParams) ([]domain.Shipment, int64, error)
	AssignCourier(context.Context, int64, int64, int64) (domain.Shipment, error)
	UpdateStatus(context.Context, int64, domain.Status, string, string, int64) (domain.Shipment, error)
	ListEvents(context.Context, int64) ([]domain.TrackingEvent, error)
	PublicTracking(context.Context, string) (domain.PublicTracking, error)
	Dashboard(context.Context, int64, domain.Role) (domain.Dashboard, error)
}
