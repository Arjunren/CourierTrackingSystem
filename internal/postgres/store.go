package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/Arjunren/courier-tracking-system/internal/domain"
	basestore "github.com/Arjunren/courier-tracking-system/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"time"
)

type Store struct{ Pool *pgxpool.Pool }

func Open(c context.Context, u string) (*Store, error) {
	p, e := pgxpool.New(c, u)
	if e != nil {
		return nil, e
	}
	if e = p.Ping(c); e != nil {
		p.Close()
		return nil, e
	}
	return &Store{p}, nil
}
func (s *Store) Close() { s.Pool.Close() }
func mapErr(e error) error {
	if errors.Is(e, pgx.ErrNoRows) {
		return basestore.ErrNotFound
	}
	var p *pgconn.PgError
	if errors.As(e, &p) && (p.Code == "23505" || p.Code == "23503" || p.Code == "23514") {
		return basestore.ErrConflict
	}
	return e
}

const userCols = `id,email,password_hash,name,role,is_active,created_at,updated_at`

func (s *Store) UserByEmail(c context.Context, email string) (domain.User, error) {
	var u domain.User
	e := s.Pool.QueryRow(c, `SELECT `+userCols+` FROM users WHERE LOWER(email)=LOWER($1)`, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	return u, mapErr(e)
}
func (s *Store) UserByID(c context.Context, id int64) (domain.User, error) {
	var u domain.User
	e := s.Pool.QueryRow(c, `SELECT `+userCols+` FROM users WHERE id=$1`, id).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	return u, mapErr(e)
}
func (s *Store) CreateUser(c context.Context, u domain.User) (domain.User, error) {
	e := s.Pool.QueryRow(c, `INSERT INTO users(email,password_hash,name,role)VALUES(LOWER($1),$2,$3,$4)RETURNING `+userCols, u.Email, u.PasswordHash, u.Name, u.Role).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	return u, mapErr(e)
}
func (s *Store) CreateSessionToken(c context.Context, id string, uid int64, exp time.Time) error {
	_, e := s.Pool.Exec(c, `INSERT INTO sessions(id,user_id,expires_at)VALUES($1,$2,$3)`, id, uid, exp)
	return e
}
func (s *Store) SessionActive(c context.Context, id string, uid int64) (bool, error) {
	var ok bool
	e := s.Pool.QueryRow(c, `SELECT EXISTS(SELECT 1 FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.id=$1 AND s.user_id=$2 AND s.revoked_at IS NULL AND s.expires_at>NOW() AND u.is_active)`, id, uid).Scan(&ok)
	return ok, e
}
func (s *Store) RevokeSession(c context.Context, id string, uid int64) error {
	_, e := s.Pool.Exec(c, `UPDATE sessions SET revoked_at=NOW() WHERE id=$1 AND user_id=$2`, id, uid)
	return e
}

const shipmentSelect = `SELECT s.id,s.tracking_number,s.sender_name,s.sender_phone,s.recipient_name,s.recipient_phone,s.origin,s.destination,s.status,s.customer_id,s.assigned_courier_id,COALESCE(u.name,''),s.expected_delivery,s.delivered_at,s.created_at,s.updated_at FROM shipments s LEFT JOIN users u ON u.id=s.assigned_courier_id`

func scanShipment(r interface{ Scan(...any) error }) (domain.Shipment, error) {
	var x domain.Shipment
	e := r.Scan(&x.ID, &x.TrackingNumber, &x.SenderName, &x.SenderPhone, &x.RecipientName, &x.RecipientPhone, &x.Origin, &x.Destination, &x.Status, &x.CustomerID, &x.AssignedCourierID, &x.CourierName, &x.ExpectedDelivery, &x.DeliveredAt, &x.CreatedAt, &x.UpdatedAt)
	return x, e
}
func (s *Store) ShipmentByID(c context.Context, id int64) (domain.Shipment, error) {
	x, e := scanShipment(s.Pool.QueryRow(c, shipmentSelect+` WHERE s.id=$1`, id))
	return x, mapErr(e)
}
func (s *Store) CreateShipment(c context.Context, x domain.Shipment) (domain.Shipment, error) {
	tx, e := s.Pool.Begin(c)
	if e != nil {
		return x, e
	}
	defer func() { _ = tx.Rollback(c) }()
	if e = tx.QueryRow(c, `INSERT INTO shipments(tracking_number,sender_name,sender_phone,recipient_name,recipient_phone,origin,destination,customer_id,expected_delivery)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)RETURNING id`, x.TrackingNumber, x.SenderName, x.SenderPhone, x.RecipientName, x.RecipientPhone, x.Origin, x.Destination, x.CustomerID, x.ExpectedDelivery).Scan(&x.ID); e != nil {
		return x, mapErr(e)
	}
	if _, e = tx.Exec(c, `INSERT INTO tracking_events(shipment_id,status,location,notes,recorded_by)VALUES($1,'CREATED',$2,'Shipment created',$3)`, x.ID, x.Origin, x.CustomerID); e != nil {
		return x, mapErr(e)
	}
	if e = tx.Commit(c); e != nil {
		return x, e
	}
	return s.ShipmentByID(c, x.ID)
}
func (s *Store) ListShipments(c context.Context, p domain.ListParams) ([]domain.Shipment, int64, error) {
	clauses := []string{`(LOWER(s.tracking_number) LIKE $1 OR LOWER(s.origin) LIKE $1 OR LOWER(s.destination) LIKE $1)`}
	args := []any{"%" + strings.ToLower(p.Search) + "%"}
	add := func(q string, v any) { args = append(args, v); clauses = append(clauses, fmt.Sprintf(q, len(args))) }
	if p.Status != "" {
		add(`s.status=$%d`, p.Status)
	}
	if p.ActorRole == domain.RoleCustomer {
		add(`s.customer_id=$%d`, p.ActorID)
	} else if p.ActorRole == domain.RoleCourier {
		add(`s.assigned_courier_id=$%d`, p.ActorID)
	}
	where := " WHERE " + strings.Join(clauses, " AND ")
	var n int64
	if e := s.Pool.QueryRow(c, `SELECT COUNT(*) FROM shipments s`+where, args...).Scan(&n); e != nil {
		return nil, 0, e
	}
	args = append(args, p.Limit, (p.Page-1)*p.Limit)
	q := fmt.Sprintf(`%s%s ORDER BY s.updated_at DESC,s.id DESC LIMIT $%d OFFSET $%d`, shipmentSelect, where, len(args)-1, len(args))
	rows, e := s.Pool.Query(c, q, args...)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	out := []domain.Shipment{}
	for rows.Next() {
		x, e := scanShipment(rows)
		if e != nil {
			return nil, 0, e
		}
		out = append(out, x)
	}
	return out, n, rows.Err()
}
func (s *Store) AssignCourier(c context.Context, id, courier, actor int64) (domain.Shipment, error) {
	tx, e := s.Pool.Begin(c)
	if e != nil {
		return domain.Shipment{}, e
	}
	defer func() { _ = tx.Rollback(c) }()
	var status domain.Status
	var assigned *int64
	if e = tx.QueryRow(c, `SELECT status,assigned_courier_id FROM shipments WHERE id=$1 FOR UPDATE`, id).Scan(&status, &assigned); e != nil {
		return domain.Shipment{}, mapErr(e)
	}
	if status != domain.StatusCreated || assigned != nil {
		return domain.Shipment{}, basestore.ErrInvalidTransition
	}
	if _, e = tx.Exec(c, `UPDATE shipments SET assigned_courier_id=$2,updated_at=NOW() WHERE id=$1`, id, courier); e != nil {
		return domain.Shipment{}, mapErr(e)
	}
	if _, e = tx.Exec(c, `INSERT INTO tracking_events(shipment_id,status,notes,recorded_by)VALUES($1,$2,'Courier assigned',$3)`, id, status, actor); e != nil {
		return domain.Shipment{}, mapErr(e)
	}
	if e = tx.Commit(c); e != nil {
		return domain.Shipment{}, e
	}
	return s.ShipmentByID(c, id)
}
func (s *Store) UpdateStatus(c context.Context, id int64, to domain.Status, location, notes string, actor int64) (domain.Shipment, error) {
	tx, e := s.Pool.Begin(c)
	if e != nil {
		return domain.Shipment{}, e
	}
	defer func() { _ = tx.Rollback(c) }()
	var from domain.Status
	var courier *int64
	if e = tx.QueryRow(c, `SELECT status,assigned_courier_id FROM shipments WHERE id=$1 FOR UPDATE`, id).Scan(&from, &courier); e != nil {
		return domain.Shipment{}, mapErr(e)
	}
	if !domain.CanTransition(from, to) || (to == domain.StatusPickedUp && courier == nil) {
		return domain.Shipment{}, basestore.ErrInvalidTransition
	}
	if _, e = tx.Exec(c, `UPDATE shipments SET status=$2,delivered_at=CASE WHEN $2='DELIVERED' THEN NOW() ELSE delivered_at END,updated_at=NOW() WHERE id=$1`, id, to); e != nil {
		return domain.Shipment{}, mapErr(e)
	}
	if _, e = tx.Exec(c, `INSERT INTO tracking_events(shipment_id,status,location,notes,recorded_by)VALUES($1,$2,$3,$4,$5)`, id, to, location, notes, actor); e != nil {
		return domain.Shipment{}, mapErr(e)
	}
	if e = tx.Commit(c); e != nil {
		return domain.Shipment{}, e
	}
	return s.ShipmentByID(c, id)
}
func (s *Store) ListEvents(c context.Context, id int64) ([]domain.TrackingEvent, error) {
	rows, e := s.Pool.Query(c, `SELECT id,shipment_id,status,location,notes,recorded_by,created_at FROM tracking_events WHERE shipment_id=$1 ORDER BY created_at,id`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []domain.TrackingEvent{}
	for rows.Next() {
		var x domain.TrackingEvent
		if e := rows.Scan(&x.ID, &x.ShipmentID, &x.Status, &x.Location, &x.Notes, &x.RecordedBy, &x.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) PublicTracking(c context.Context, number string) (domain.PublicTracking, error) {
	var x domain.PublicTracking
	var id int64
	e := s.Pool.QueryRow(c, `SELECT id,tracking_number,origin,destination,status,expected_delivery,delivered_at FROM shipments WHERE tracking_number=UPPER($1)`, number).Scan(&id, &x.TrackingNumber, &x.Origin, &x.Destination, &x.Status, &x.ExpectedDelivery, &x.DeliveredAt)
	if e != nil {
		return x, mapErr(e)
	}
	rows, e := s.Pool.Query(c, `SELECT id,shipment_id,status,location,notes,created_at FROM tracking_events WHERE shipment_id=$1 ORDER BY created_at,id`, id)
	if e != nil {
		return x, e
	}
	defer rows.Close()
	x.Events = []domain.TrackingEvent{}
	for rows.Next() {
		var ev domain.TrackingEvent
		if e := rows.Scan(&ev.ID, &ev.ShipmentID, &ev.Status, &ev.Location, &ev.Notes, &ev.CreatedAt); e != nil {
			return x, e
		}
		x.Events = append(x.Events, ev)
	}
	return x, rows.Err()
}
func (s *Store) Dashboard(c context.Context, id int64, role domain.Role) (domain.Dashboard, error) {
	where := ""
	args := []any{}
	if role == domain.RoleCustomer {
		where = " WHERE customer_id=$1"
		args = append(args, id)
	} else if role == domain.RoleCourier {
		where = " WHERE assigned_courier_id=$1"
		args = append(args, id)
	}
	var d domain.Dashboard
	q := `SELECT COUNT(*),COUNT(*)FILTER(WHERE status='CREATED'),COUNT(*)FILTER(WHERE status IN('PICKED_UP','IN_TRANSIT')),COUNT(*)FILTER(WHERE status='OUT_FOR_DELIVERY'),COUNT(*)FILTER(WHERE status='DELIVERED'),COUNT(*)FILTER(WHERE status='CANCELLED') FROM shipments` + where
	e := s.Pool.QueryRow(c, q, args...).Scan(&d.Total, &d.Created, &d.InTransit, &d.OutForDelivery, &d.Delivered, &d.Cancelled)
	return d, e
}
