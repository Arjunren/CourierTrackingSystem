package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Arjunren/courier-tracking-system/internal/domain"
	"github.com/Arjunren/courier-tracking-system/internal/service"
	"github.com/Arjunren/courier-tracking-system/internal/store"
	"github.com/go-chi/chi/v5"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type API struct {
	s   *service.Service
	log *slog.Logger
}

func New(s *service.Service, l *slog.Logger) *API { return &API{s, l} }

type key string

const claimsKey key = "claims"

func (a *API) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(a.recover)
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) { write(w, 200, map[string]string{"status": "ok"}) })
	r.Post("/api/auth/register", a.register)
	r.With(newLoginLimiter(10).middleware).Post("/api/auth/login", a.login)
	r.With(newLoginLimiter(60).middleware).Get("/api/track/{number}", a.track)
	r.Group(func(r chi.Router) {
		r.Use(a.auth)
		r.Post("/api/auth/logout", a.logout)
		r.Get("/api/dashboard", a.dashboard)
		r.With(a.admin).Post("/api/users", a.createUser)
		r.Get("/api/shipments", a.listShipments)
		r.Get("/api/shipments/{id}", a.getShipment)
		r.Get("/api/shipments/{id}/events", a.events)
		r.With(a.customer).Post("/api/shipments", a.createShipment)
		r.With(a.dispatch).Post("/api/shipments/{id}/assign", a.assign)
		r.Post("/api/shipments/{id}/status", a.updateStatus)
	})
	return r
}
func write(w http.ResponseWriter, n int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(n)
	_ = json.NewEncoder(w).Encode(v)
}
func apiError(w http.ResponseWriter, n int, c, m string, f map[string]string) {
	write(w, n, map[string]any{"error": map[string]any{"code": c, "message": m, "fields": f}})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	m, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || m != "application/json" {
		return errors.New("content type")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e = d.Decode(v); e != nil {
		return e
	}
	if e = d.Decode(&struct{}{}); !errors.Is(e, io.EOF) {
		return errors.New("multiple values")
	}
	return nil
}
func (a *API) fail(w http.ResponseWriter, e error) {
	var v service.ValidationError
	switch {
	case errors.As(e, &v):
		apiError(w, 422, "VALIDATION_ERROR", "Invalid request data", v.Fields)
	case errors.Is(e, service.ErrCredentials):
		apiError(w, 401, "INVALID_CREDENTIALS", "Email or password is incorrect", nil)
	case errors.Is(e, service.ErrUnauthorized):
		apiError(w, 401, "UNAUTHORIZED", "A valid bearer token is required", nil)
	case errors.Is(e, service.ErrForbidden):
		apiError(w, 403, "FORBIDDEN", "You cannot access or change this shipment", nil)
	case errors.Is(e, store.ErrInvalidTransition):
		apiError(w, 409, "INVALID_TRANSITION", "The shipment cannot make that transition", nil)
	case errors.Is(e, store.ErrConflict):
		apiError(w, 409, "CONFLICT", "The request conflicts with existing data", nil)
	case errors.Is(e, store.ErrNotFound):
		apiError(w, 404, "NOT_FOUND", "The requested resource was not found", nil)
	default:
		a.log.Error("request failed", "error", e)
		apiError(w, 500, "INTERNAL_ERROR", "An internal error occurred", nil)
	}
}
func (a *API) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() != nil {
				a.log.Error("panic", "path", r.URL.Path)
				apiError(w, 500, "INTERNAL_ERROR", "An internal error occurred", nil)
			}
		}()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
func (a *API) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
		if len(p) != 2 || !strings.EqualFold(p[0], "Bearer") {
			apiError(w, 401, "UNAUTHORIZED", "A valid bearer token is required", nil)
			return
		}
		c, e := a.s.Verify(r.Context(), p[1])
		if e != nil {
			a.fail(w, e)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey, c)))
	})
}
func (a *API) admin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if claims(r).Role != domain.RoleAdmin {
			apiError(w, 403, "FORBIDDEN", "Admin role required", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *API) dispatch(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := claims(r).Role
		if role != domain.RoleAdmin && role != domain.RoleDispatcher {
			apiError(w, 403, "FORBIDDEN", "Dispatcher or admin role required", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *API) customer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if claims(r).Role != domain.RoleCustomer {
			apiError(w, 403, "FORBIDDEN", "Customer role required", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func claims(r *http.Request) service.Claims {
	c, _ := r.Context().Value(claimsKey).(service.Claims)
	return c
}
func actor(r *http.Request) int64 { var id int64; _, _ = fmt.Sscan(claims(r).Subject, &id); return id }
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, e := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if e != nil || id < 1 {
		apiError(w, 400, "INVALID_ID", "ID must be positive", nil)
		return 0, false
	}
	return id, true
}

type userInput struct {
	Name     string      `json:"name"`
	Email    string      `json:"email"`
	Password string      `json:"password"`
	Role     domain.Role `json:"role"`
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var in userInput
	if decode(w, r, &in) != nil {
		apiError(w, 400, "INVALID_JSON", "Request body must be valid JSON", nil)
		return
	}
	u, e := a.s.CreateUser(r.Context(), in.Name, in.Email, in.Password, domain.RoleCustomer)
	if e != nil {
		a.fail(w, e)
		return
	}
	write(w, 201, map[string]any{"data": u})
}
func (a *API) createUser(w http.ResponseWriter, r *http.Request) {
	var in userInput
	if decode(w, r, &in) != nil {
		apiError(w, 400, "INVALID_JSON", "Request body must be valid JSON", nil)
		return
	}
	u, e := a.s.CreateUser(r.Context(), in.Name, in.Email, in.Password, in.Role)
	if e != nil {
		a.fail(w, e)
		return
	}
	write(w, 201, map[string]any{"data": u})
}
func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if decode(w, r, &in) != nil {
		apiError(w, 400, "INVALID_JSON", "Request body must be valid JSON", nil)
		return
	}
	t, u, e := a.s.Login(r.Context(), in.Email, in.Password)
	if e != nil {
		a.fail(w, e)
		return
	}
	write(w, 200, map[string]any{"data": map[string]any{"access_token": t, "token_type": "Bearer", "user": u}})
}
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if e := a.s.Logout(r.Context(), claims(r)); e != nil {
		a.fail(w, e)
		return
	}
	w.WriteHeader(204)
}

type shipmentInput struct {
	SenderName       string `json:"sender_name"`
	SenderPhone      string `json:"sender_phone"`
	RecipientName    string `json:"recipient_name"`
	RecipientPhone   string `json:"recipient_phone"`
	Origin           string `json:"origin"`
	Destination      string `json:"destination"`
	ExpectedDelivery string `json:"expected_delivery"`
}

func (a *API) createShipment(w http.ResponseWriter, r *http.Request) {
	var in shipmentInput
	if decode(w, r, &in) != nil {
		apiError(w, 400, "INVALID_JSON", "Request body must be valid JSON", nil)
		return
	}
	var expected *time.Time
	if in.ExpectedDelivery != "" {
		d, e := time.Parse("2006-01-02", in.ExpectedDelivery)
		if e != nil {
			apiError(w, 422, "VALIDATION_ERROR", "Invalid request data", map[string]string{"expected_delivery": "must use YYYY-MM-DD"})
			return
		}
		expected = &d
	}
	x, e := a.s.CreateShipment(r.Context(), domain.Shipment{SenderName: in.SenderName, SenderPhone: in.SenderPhone, RecipientName: in.RecipientName, RecipientPhone: in.RecipientPhone, Origin: in.Origin, Destination: in.Destination, ExpectedDelivery: expected, CustomerID: actor(r)})
	if e != nil {
		a.fail(w, e)
		return
	}
	write(w, 201, map[string]any{"data": x})
}
func (a *API) getShipment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	x, e := a.s.GetShipment(r.Context(), id, actor(r), claims(r).Role)
	if e != nil {
		a.fail(w, e)
		return
	}
	write(w, 200, map[string]any{"data": x})
}
func page(r *http.Request) (domain.ListParams, error) {
	p := domain.ListParams{Page: 1, Limit: 20, Search: strings.TrimSpace(r.URL.Query().Get("search")), Status: domain.Status(strings.ToUpper(r.URL.Query().Get("status"))), ActorID: actor(r), ActorRole: claims(r).Role}
	var e error
	if v := r.URL.Query().Get("page"); v != "" {
		p.Page, e = strconv.Atoi(v)
		if e != nil || p.Page < 1 {
			return p, errors.New("invalid page")
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		p.Limit, e = strconv.Atoi(v)
		if e != nil || p.Limit < 1 || p.Limit > 100 {
			return p, errors.New("invalid limit")
		}
	}
	return p, nil
}
func (a *API) listShipments(w http.ResponseWriter, r *http.Request) {
	p, e := page(r)
	if e != nil {
		apiError(w, 400, "INVALID_QUERY", "page and limit are invalid", nil)
		return
	}
	x, m, e := a.s.ListShipments(r.Context(), p)
	if e != nil {
		a.fail(w, e)
		return
	}
	write(w, 200, map[string]any{"data": x, "meta": m})
}
func (a *API) assign(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		CourierID int64 `json:"courier_id"`
	}
	if decode(w, r, &in) != nil {
		apiError(w, 400, "INVALID_JSON", "Request body must be valid JSON", nil)
		return
	}
	x, e := a.s.Assign(r.Context(), id, in.CourierID, actor(r))
	if e != nil {
		a.fail(w, e)
		return
	}
	write(w, 200, map[string]any{"data": x})
}
func (a *API) updateStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Status   domain.Status `json:"status"`
		Location string        `json:"location"`
		Notes    string        `json:"notes"`
	}
	if decode(w, r, &in) != nil {
		apiError(w, 400, "INVALID_JSON", "Request body must be valid JSON", nil)
		return
	}
	x, e := a.s.UpdateStatus(r.Context(), id, actor(r), claims(r).Role, in.Status, in.Location, in.Notes)
	if e != nil {
		a.fail(w, e)
		return
	}
	write(w, 200, map[string]any{"data": x})
}
func (a *API) events(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	x, e := a.s.Events(r.Context(), id, actor(r), claims(r).Role)
	if e != nil {
		a.fail(w, e)
		return
	}
	write(w, 200, map[string]any{"data": x})
}
func (a *API) track(w http.ResponseWriter, r *http.Request) {
	x, e := a.s.PublicTracking(r.Context(), chi.URLParam(r, "number"))
	if e != nil {
		a.fail(w, e)
		return
	}
	write(w, 200, map[string]any{"data": x})
}
func (a *API) dashboard(w http.ResponseWriter, r *http.Request) {
	x, e := a.s.Dashboard(r.Context(), actor(r), claims(r).Role)
	if e != nil {
		a.fail(w, e)
		return
	}
	write(w, 200, map[string]any{"data": x})
}
