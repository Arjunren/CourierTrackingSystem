package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/Arjunren/courier-tracking-system/internal/domain"
	"github.com/Arjunren/courier-tracking-system/internal/store"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"net/mail"
	"strings"
	"time"
)

var (
	ErrCredentials  = errors.New("invalid credentials")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
)

type ValidationError struct{ Fields map[string]string }

func (e ValidationError) Error() string { return "validation failed" }
func invalid(k, v string) error         { return ValidationError{map[string]string{k: v}} }

type Claims struct {
	Role domain.Role `json:"role"`
	jwt.RegisteredClaims
}
type Service struct {
	db     store.Store
	secret []byte
	expiry time.Duration
	dummy  []byte
	now    func() time.Time
}

func New(db store.Store, secret string, expiry time.Duration) *Service {
	d, _ := bcrypt.GenerateFromPassword([]byte("dummy"), bcrypt.DefaultCost)
	return &Service{db, []byte(secret), expiry, d, time.Now}
}
func (s *Service) CreateUser(c context.Context, name, email, password string, role domain.Role) (domain.User, error) {
	name = strings.TrimSpace(name)
	email = strings.ToLower(strings.TrimSpace(email))
	a, e := mail.ParseAddress(email)
	if len(name) < 2 || len(name) > 120 {
		return domain.User{}, invalid("name", "must contain 2 to 120 characters")
	}
	if e != nil || a.Address != email {
		return domain.User{}, invalid("email", "must be valid")
	}
	if len(password) < 12 || len(password) > 72 {
		return domain.User{}, invalid("password", "must contain 12 to 72 characters")
	}
	if role != domain.RoleAdmin && role != domain.RoleDispatcher && role != domain.RoleCourier && role != domain.RoleCustomer {
		return domain.User{}, invalid("role", "is invalid")
	}
	h, e := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if e != nil {
		return domain.User{}, e
	}
	return s.db.CreateUser(c, domain.User{Name: name, Email: email, PasswordHash: string(h), Role: role})
}
func (s *Service) Login(c context.Context, email, password string) (string, domain.User, error) {
	u, e := s.db.UserByEmail(c, email)
	if e != nil {
		_ = bcrypt.CompareHashAndPassword(s.dummy, []byte(password))
		return "", domain.User{}, ErrCredentials
	}
	if !u.IsActive || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return "", domain.User{}, ErrCredentials
	}
	now := s.now().UTC()
	id := uuid.NewString()
	cl := Claims{u.Role, jwt.RegisteredClaims{Subject: fmt.Sprint(u.ID), ID: id, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(s.expiry))}}
	token, e := jwt.NewWithClaims(jwt.SigningMethodHS256, cl).SignedString(s.secret)
	if e != nil {
		return "", u, e
	}
	if e = s.db.CreateSessionToken(c, id, u.ID, now.Add(s.expiry)); e != nil {
		return "", u, e
	}
	return token, u, nil
}
func (s *Service) Verify(c context.Context, raw string) (Claims, error) {
	var cl Claims
	t, e := jwt.ParseWithClaims(raw, &cl, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, ErrUnauthorized
		}
		return s.secret, nil
	}, jwt.WithExpirationRequired(), jwt.WithValidMethods([]string{"HS256"}))
	if e != nil || !t.Valid {
		return cl, ErrUnauthorized
	}
	var uid int64
	if _, e = fmt.Sscan(cl.Subject, &uid); e != nil {
		return cl, ErrUnauthorized
	}
	ok, e := s.db.SessionActive(c, cl.ID, uid)
	if e != nil || !ok {
		return cl, ErrUnauthorized
	}
	return cl, nil
}
func (s *Service) Logout(c context.Context, cl Claims) error {
	var id int64
	_, _ = fmt.Sscan(cl.Subject, &id)
	return s.db.RevokeSession(c, cl.ID, id)
}
func (s *Service) CreateShipment(c context.Context, x domain.Shipment) (domain.Shipment, error) {
	x.SenderName = strings.TrimSpace(x.SenderName)
	x.SenderPhone = strings.TrimSpace(x.SenderPhone)
	x.RecipientName = strings.TrimSpace(x.RecipientName)
	x.RecipientPhone = strings.TrimSpace(x.RecipientPhone)
	x.Origin = strings.TrimSpace(x.Origin)
	x.Destination = strings.TrimSpace(x.Destination)
	if len(x.SenderName) < 2 || len(x.SenderName) > 120 {
		return x, invalid("sender_name", "must contain 2 to 120 characters")
	}
	if len(x.RecipientName) < 2 || len(x.RecipientName) > 120 {
		return x, invalid("recipient_name", "must contain 2 to 120 characters")
	}
	if len(x.SenderPhone) > 40 || len(x.RecipientPhone) > 40 {
		return x, invalid("phone", "must not exceed 40 characters")
	}
	if len(x.Origin) < 3 || len(x.Origin) > 200 {
		return x, invalid("origin", "must contain 3 to 200 characters")
	}
	if len(x.Destination) < 3 || len(x.Destination) > 200 {
		return x, invalid("destination", "must contain 3 to 200 characters")
	}
	x.TrackingNumber = "CT-" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))[:12]
	return s.db.CreateShipment(c, x)
}
func (s *Service) GetShipment(c context.Context, id, actor int64, role domain.Role) (domain.Shipment, error) {
	x, e := s.db.ShipmentByID(c, id)
	if e != nil {
		return x, e
	}
	if role == domain.RoleCustomer && x.CustomerID != actor {
		return domain.Shipment{}, ErrForbidden
	}
	if role == domain.RoleCourier && (x.AssignedCourierID == nil || *x.AssignedCourierID != actor) {
		return domain.Shipment{}, ErrForbidden
	}
	return x, nil
}
func (s *Service) ListShipments(c context.Context, p domain.ListParams) ([]domain.Shipment, domain.Page, error) {
	x, n, e := s.db.ListShipments(c, p)
	return x, domain.NewPage(p, n), e
}
func (s *Service) Assign(c context.Context, id, courier, actor int64) (domain.Shipment, error) {
	u, e := s.db.UserByID(c, courier)
	if e != nil {
		return domain.Shipment{}, e
	}
	if !u.IsActive || u.Role != domain.RoleCourier {
		return domain.Shipment{}, invalid("courier_id", "must reference an active courier")
	}
	return s.db.AssignCourier(c, id, courier, actor)
}
func (s *Service) UpdateStatus(c context.Context, id, actor int64, role domain.Role, to domain.Status, location, notes string) (domain.Shipment, error) {
	x, e := s.GetShipment(c, id, actor, role)
	if e != nil {
		return x, e
	}
	location = strings.TrimSpace(location)
	notes = strings.TrimSpace(notes)
	if len(location) > 200 {
		return x, invalid("location", "must not exceed 200 characters")
	}
	if len(notes) > 1000 {
		return x, invalid("notes", "must not exceed 1000 characters")
	}
	if role == domain.RoleCustomer {
		if to != domain.StatusCancelled || x.Status != domain.StatusCreated {
			return x, ErrForbidden
		}
	} else if role == domain.RoleCourier {
		if to == domain.StatusCancelled {
			return x, ErrForbidden
		}
	} else if role != domain.RoleAdmin && role != domain.RoleDispatcher {
		return x, ErrForbidden
	}
	return s.db.UpdateStatus(c, id, to, location, notes, actor)
}
func (s *Service) Events(c context.Context, id, actor int64, role domain.Role) ([]domain.TrackingEvent, error) {
	if _, e := s.GetShipment(c, id, actor, role); e != nil {
		return nil, e
	}
	return s.db.ListEvents(c, id)
}
func (s *Service) PublicTracking(c context.Context, n string) (domain.PublicTracking, error) {
	n = strings.ToUpper(strings.TrimSpace(n))
	if len(n) < 5 || len(n) > 24 {
		return domain.PublicTracking{}, invalid("tracking_number", "is invalid")
	}
	return s.db.PublicTracking(c, n)
}
func (s *Service) Dashboard(c context.Context, id int64, role domain.Role) (domain.Dashboard, error) {
	return s.db.Dashboard(c, id, role)
}
