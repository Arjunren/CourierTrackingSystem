//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/Arjunren/courier-tracking-system/internal/domain"
	"github.com/Arjunren/courier-tracking-system/internal/service"
	basestore "github.com/Arjunren/courier-tracking-system/internal/store"
	"github.com/Arjunren/courier-tracking-system/migrations"
)

func TestConcurrentCourierAssignmentAllowsOneWinner(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Apply(ctx, db.Pool); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `TRUNCATE tracking_events,shipments,sessions,users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	svc := service.New(db, "0123456789abcdef0123456789abcdef", 0)
	dispatcher, _ := svc.CreateUser(ctx, "Dispatcher", "dispatch@test.local", "TestPassword123!", domain.RoleDispatcher)
	first, _ := svc.CreateUser(ctx, "First", "first@test.local", "TestPassword123!", domain.RoleCourier)
	second, _ := svc.CreateUser(ctx, "Second", "second@test.local", "TestPassword123!", domain.RoleCourier)
	customer, _ := svc.CreateUser(ctx, "Customer", "customer@test.local", "TestPassword123!", domain.RoleCustomer)
	shipment, err := db.CreateShipment(ctx, domain.Shipment{TrackingNumber: "CT-TEST000001", SenderName: "Sender", RecipientName: "Recipient", Origin: "Taipei", Destination: "Kaohsiung", CustomerID: customer.ID})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, courier := range []int64{first.ID, second.ID} {
		wg.Add(1)
		go func(courier int64) {
			defer wg.Done()
			_, err := db.AssignCourier(ctx, shipment.ID, courier, dispatcher.ID)
			errs <- err
		}(courier)
	}
	wg.Wait()
	close(errs)
	success, rejected := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, basestore.ErrInvalidTransition) {
			rejected++
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("success=%d rejected=%d", success, rejected)
	}
}
