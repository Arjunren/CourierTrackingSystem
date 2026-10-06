package main

import (
	"context"
	"errors"
	"github.com/Arjunren/courier-tracking-system/internal/domain"
	"github.com/Arjunren/courier-tracking-system/internal/postgres"
	"github.com/Arjunren/courier-tracking-system/internal/service"
	"github.com/Arjunren/courier-tracking-system/internal/store"
	"log"
	"os"
)

func main() {
	c := context.Background()
	db, e := postgres.Open(c, need("DATABASE_URL"))
	if e != nil {
		log.Fatal(e)
	}
	defer db.Close()
	s := service.New(db, "seed-only-secret-not-used-for-tokens", 0)
	seed(c, s, db, need("SEED_ADMIN_EMAIL"), need("SEED_ADMIN_PASSWORD"), "Development Admin", domain.RoleAdmin)
	seed(c, s, db, need("SEED_DISPATCHER_EMAIL"), need("SEED_DISPATCHER_PASSWORD"), "Development Dispatcher", domain.RoleDispatcher)
	seed(c, s, db, need("SEED_COURIER_EMAIL"), need("SEED_COURIER_PASSWORD"), "Development Courier", domain.RoleCourier)
	seed(c, s, db, need("SEED_CUSTOMER_EMAIL"), need("SEED_CUSTOMER_PASSWORD"), "Development Customer", domain.RoleCustomer)
}
func seed(c context.Context, s *service.Service, db store.Store, email, pw, name string, role domain.Role) {
	if _, e := db.UserByEmail(c, email); e == nil {
		return
	} else if !errors.Is(e, store.ErrNotFound) {
		log.Fatal(e)
	}
	if _, e := s.CreateUser(c, name, email, pw, role); e != nil {
		log.Fatal(e)
	}
}
func need(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("%s is required", k)
	}
	return v
}
