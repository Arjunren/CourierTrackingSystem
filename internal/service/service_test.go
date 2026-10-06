package service

import (
	"context"
	"errors"
	"github.com/Arjunren/courier-tracking-system/internal/domain"
	"github.com/Arjunren/courier-tracking-system/internal/store"
	"testing"
	"time"
)

type fake struct {
	store.Store
	shipment domain.Shipment
	user     domain.User
}

func (f *fake) ShipmentByID(context.Context, int64) (domain.Shipment, error) { return f.shipment, nil }
func (f *fake) UserByID(context.Context, int64) (domain.User, error)         { return f.user, nil }
func TestCustomerOwnership(t *testing.T) {
	s := New(&fake{shipment: domain.Shipment{ID: 1, CustomerID: 2}}, "0123456789abcdef0123456789abcdef", time.Minute)
	_, e := s.GetShipment(context.Background(), 1, 3, domain.RoleCustomer)
	if !errors.Is(e, ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", e)
	}
}
func TestCustomerCanOnlyCancelCreatedShipment(t *testing.T) {
	s := New(&fake{shipment: domain.Shipment{ID: 1, CustomerID: 2, Status: domain.StatusInTransit}}, "0123456789abcdef0123456789abcdef", time.Minute)
	_, e := s.UpdateStatus(context.Background(), 1, 2, domain.RoleCustomer, domain.StatusCancelled, "", "")
	if !errors.Is(e, ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", e)
	}
}
func TestStatusMachine(t *testing.T) {
	if domain.CanTransition(domain.StatusCreated, domain.StatusDelivered) {
		t.Fatal("created must not transition directly to delivered")
	}
	if !domain.CanTransition(domain.StatusInTransit, domain.StatusOutForDelivery) {
		t.Fatal("expected valid transition")
	}
}
