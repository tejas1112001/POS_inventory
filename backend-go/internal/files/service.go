package files

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/plans"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Purposes (tenant files.purpose).
const (
	PurposeTenantLogo    = "tenant_logo"
	PurposeStoreLogo     = "store_logo"
	PurposeStoreBanner   = "store_banner"
	PurposeProductImage  = "product_image"
	PurposeCustomerImage = "customer_image"
	PurposeUserImage     = "user_image"
	PurposeCategoryImage = "category_image"
	PurposeBrandImage    = "brand_image"
	PurposeSupplierImage = "supplier_image"
)

// PublicPurposes are the only purposes /files/t/ can ever serve, and then
// only while the owning row allows it (API_CONTRACT §5.1).
var PublicPurposes = map[string]bool{
	PurposeTenantLogo: true, PurposeStoreLogo: true, PurposeStoreBanner: true, PurposeProductImage: true,
}

// ViewPermission is the permission needed for GET /api/files/{id}.
var ViewPermission = map[string]string{
	PurposeTenantLogo: "", PurposeStoreLogo: "stores:view", PurposeStoreBanner: "stores:view",
	PurposeProductImage: "products:view", PurposeCustomerImage: "customers:view", PurposeUserImage: "users:view",
	PurposeCategoryImage: "categories:view", PurposeBrandImage: "brands:view", PurposeSupplierImage: "suppliers:view",
}

// UploadPermissions lists, per purpose, the permissions that allow POST
// /api/files (any one of them is enough).
var UploadPermissions = map[string][]string{
	PurposeTenantLogo: {"settings:edit"}, PurposeStoreLogo: {"stores:create", "stores:edit"},
	PurposeStoreBanner: {"stores:create", "stores:edit"}, PurposeProductImage: {"products:create", "products:edit"},
	PurposeCustomerImage: {"customers:create", "customers:edit"}, PurposeUserImage: {"users:create", "users:edit"},
	PurposeCategoryImage: {"categories:create", "categories:edit"}, PurposeBrandImage: {"brands:create", "brands:edit"},
	PurposeSupplierImage: {"suppliers:create", "suppliers:edit"},
}

// URL returns the URL clients use for a file.
func URL(id uuid.UUID, purpose string) string {
	if PublicPurposes[purpose] {
		return "/files/t/" + id.String()
	}
	return "/api/files/" + id.String()
}

// URLPtr is URL for an optional file ID (JSON null when nil).
func URLPtr(id *uuid.UUID, purpose string) *string {
	if id == nil {
		return nil
	}
	s := URL(*id, purpose)
	return &s
}

// Service stores and serves tenant files.
type Service struct {
	store    Storage
	sessions *tenancy.Sessions
	plans    *plans.Service
	log      *slog.Logger
}

// NewService creates the service.
func NewService(st Storage, s *tenancy.Sessions, p *plans.Service, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{store: st, sessions: s, plans: p, log: log}
}

func objectKey(tenantID string, id uuid.UUID, ext string) string {
	return "tenants/" + tenantID + "/images/" + id.String() + "." + ext
}

// Stored is an object written to storage but not yet recorded in the DB.
type Stored struct {
	ID    uuid.UUID
	Key   string
	Image Image
}

// StoreImage validates quota and writes the object. Call outside the DB
// transaction; record it with InsertFile inside, and Discard it if the
// transaction fails.
func (s *Service) StoreImage(ctx context.Context, t tenancy.Tenant, img Image) (Stored, error) {
	if err := s.plans.CheckStorage(ctx, t, int64(len(img.Data))); err != nil {
		return Stored{}, err
	}
	id := uuid.New()
	st := Stored{ID: id, Key: objectKey(t.ID, id, img.Ext), Image: img}
	if err := s.store.Put(ctx, st.Key, img.ContentType, img.Data); err != nil {
		return Stored{}, err
	}
	return st, nil
}

// Discard deletes an object whose DB row was never committed.
func (s *Service) Discard(ctx context.Context, st Stored) {
	if err := s.store.Delete(context.WithoutCancel(ctx), st.Key); err != nil {
		s.log.WarnContext(ctx, "orphan file not deleted", slog.String("key", st.Key), slog.Any("err", err))
	}
}

const qInsertFile = `INSERT INTO files (id, kind, purpose, content_type, size_bytes, sha256, storage_key, width, height, created_by)
	VALUES ($1, 'image', $2, $3, $4, $5, $6, $7, $8, NULLIF($9, 0))`

// InsertFile records a stored object.
func InsertFile(ctx context.Context, tx tenancy.TenantTx, st Stored, purpose string, userID int64) error {
	_, err := tx.Exec(ctx, qInsertFile, st.ID, purpose, st.Image.ContentType, len(st.Image.Data), st.Image.SHA256,
		st.Key, st.Image.Width, st.Image.Height, userID)
	return err
}

// ----------------------------------------------------------- image fields

// Change describes what an entity's image field asks for.
type Change struct {
	keep     bool
	clear    bool
	stored   *Stored    // new upload from a data URI
	attach   *uuid.UUID // existing unattached upload (POST /api/files)
	purpose  string
	svc      *Service
	tenantID string
	released int64 // bytes freed by replacing the old file
}

// Keep reports whether the field leaves the image unchanged.
func (c *Change) Keep() bool { return c == nil || c.keep }

// Prepare interprets an image field (C-17):
//   - absent (nil): keep
//   - "" : remove
//   - the current image's URL: keep (edit forms send the value back)
//   - URL of an unattached file uploaded with POST /api/files: attach it
//   - "data:image/...;base64,...": validate, re-encode, store
//   - anything else: 400
func (s *Service) Prepare(ctx context.Context, t tenancy.Tenant, field string, v *string, current *uuid.UUID, purpose string) (*Change, error) {
	c := &Change{purpose: purpose, svc: s, tenantID: t.ID}
	switch {
	case v == nil:
		c.keep = true
	case *v == "":
		c.clear, c.keep = current != nil, current == nil
	case current != nil && *v == URL(*current, purpose):
		c.keep = true
	case strings.HasPrefix(*v, "data:"):
		raw, err := ParseDataURI(*v)
		if err != nil {
			return nil, apperr.Validation(map[string]string{field: ErrBadImage.Error()})
		}
		img, err := ProcessImage(raw)
		if err != nil {
			return nil, apperr.Validation(map[string]string{field: ErrBadImage.Error()})
		}
		st, err := s.StoreImage(ctx, t, img)
		if err != nil {
			return nil, err
		}
		c.stored = &st
	default:
		id, ok := parseFileURL(*v, purpose)
		if !ok {
			return nil, apperr.Validation(map[string]string{field: "must be a PNG, JPEG or WebP data URI or an uploaded file URL"})
		}
		c.attach = &id
	}
	return c, nil
}

func parseFileURL(v, purpose string) (uuid.UUID, bool) {
	prefix := "/api/files/"
	if PublicPurposes[purpose] {
		prefix = "/files/t/"
	}
	rest, ok := strings.CutPrefix(v, prefix)
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(rest)
	return id, err == nil
}

// Apply runs inside the entity's transaction. It records a new upload,
// verifies an attached upload, soft-deletes a replaced file and returns the
// file ID to store (nil to clear). Only call it when !Keep().
func (c *Change) Apply(ctx context.Context, tx tenancy.TenantTx, current *uuid.UUID, userID int64) (*uuid.UUID, error) {
	var next *uuid.UUID
	switch {
	case c.stored != nil:
		if err := InsertFile(ctx, tx, *c.stored, c.purpose, userID); err != nil {
			return nil, err
		}
		next = &c.stored.ID
	case c.attach != nil:
		// The upload must exist, match the purpose, and be unused.
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM files WHERE id = $1 AND purpose = $2 AND deleted_at IS NULL)`,
			*c.attach, c.purpose).Scan(&ok); err != nil {
			return nil, err
		}
		if !ok {
			return nil, apperr.Validation(map[string]string{"image": "unknown file"})
		}
		if used, err := fileInUse(ctx, tx, *c.attach); err != nil {
			return nil, err
		} else if used {
			return nil, apperr.Validation(map[string]string{"image": "file is already in use"})
		}
		next = c.attach
	}
	if current != nil && (next == nil || *next != *current) {
		var size int64
		err := tx.QueryRow(ctx, `UPDATE files SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL RETURNING size_bytes`, *current).Scan(&size)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		c.released = size
	}
	return next, nil
}

func fileInUse(ctx context.Context, tx tenancy.TenantTx, id uuid.UUID) (bool, error) {
	var used bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM categories WHERE image_file_id = $1)
		OR EXISTS (SELECT 1 FROM brands WHERE image_file_id = $1)
		OR EXISTS (SELECT 1 FROM suppliers WHERE image_file_id = $1)
		OR EXISTS (SELECT 1 FROM customers WHERE image_file_id = $1)
		OR EXISTS (SELECT 1 FROM stores WHERE logo_file_id = $1 OR banner_file_id = $1)
		OR EXISTS (SELECT 1 FROM settings WHERE logo_file_id = $1)`, id).Scan(&used)
	return used, err
}

// Done finishes the change after the transaction: on success it records
// storage use; on failure it deletes the uploaded object.
func (c *Change) Done(ctx context.Context, txErr error) {
	if c == nil {
		return
	}
	if txErr != nil {
		if c.stored != nil {
			c.svc.Discard(ctx, *c.stored)
		}
		return
	}
	delta := -c.released
	if c.stored != nil {
		delta += int64(len(c.stored.Image.Data))
	}
	if delta != 0 {
		if err := c.svc.plans.AddStorage(context.WithoutCancel(ctx), c.tenantID, delta); err != nil {
			c.svc.log.WarnContext(ctx, "storage usage not updated", slog.Any("err", err))
		}
	}
}

// ------------------------------------------------------------- serving

type fileRow struct {
	Purpose     string
	ContentType string
	Key         string
	SHA         []byte
}

const qFileByID = `SELECT purpose, content_type, storage_key, sha256 FROM files WHERE id = $1 AND deleted_at IS NULL AND kind = 'image'`

// publiclyVisible checks the owning row for a public purpose. Anything not
// positively allowed is hidden.
func publiclyVisible(ctx context.Context, tx tenancy.TenantTx, id uuid.UUID, purpose string) (bool, error) {
	var ok bool
	var err error
	switch purpose {
	case PurposeTenantLogo:
		err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM settings WHERE logo_file_id = $1)`, id).Scan(&ok)
	case PurposeStoreLogo:
		err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM stores WHERE logo_file_id = $1 AND deleted_at IS NULL)`, id).Scan(&ok)
	case PurposeStoreBanner:
		err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM stores WHERE banner_file_id = $1 AND deleted_at IS NULL)`, id).Scan(&ok)
	}
	return ok, err
}

// ServePublic handles GET /files/t/{id} on a tenant host.
func (s *Service) ServePublic(w http.ResponseWriter, r *http.Request, rawID string) {
	t, ok := tenancy.TenantFrom(r.Context())
	id, err := uuid.Parse(rawID)
	if !ok || err != nil {
		writeNotFound(w, r)
		return
	}
	var row fileRow
	err = s.sessions.InTenantTx(r.Context(), t, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
		if err := tx.QueryRow(r.Context(), qFileByID, id).Scan(&row.Purpose, &row.ContentType, &row.Key, &row.SHA); err != nil {
			return err
		}
		if !PublicPurposes[row.Purpose] {
			return pgx.ErrNoRows
		}
		visible, err := publiclyVisible(r.Context(), tx, id, row.Purpose)
		if err != nil {
			return err
		}
		if !visible {
			return pgx.ErrNoRows
		}
		return nil
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.log.ErrorContext(r.Context(), "public file lookup failed", slog.Any("err", err))
		}
		writeNotFound(w, r)
		return
	}
	s.stream(w, r, row, "public, max-age=3600")
}

// ServeAuthed handles GET /api/files/{id}; perms are the caller's.
func (s *Service) ServeAuthed(w http.ResponseWriter, r *http.Request, rawID string, has func(string) bool) {
	t, ok := tenancy.TenantFrom(r.Context())
	id, err := uuid.Parse(rawID)
	if !ok || err != nil {
		writeNotFound(w, r)
		return
	}
	var row fileRow
	err = s.sessions.InTenantTx(r.Context(), t, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.TenantTx) error {
		return tx.QueryRow(r.Context(), qFileByID, id).Scan(&row.Purpose, &row.ContentType, &row.Key, &row.SHA)
	})
	perm, known := ViewPermission[row.Purpose]
	if err != nil || !known || (perm != "" && !has(perm)) {
		writeNotFound(w, r)
		return
	}
	s.stream(w, r, row, "private, max-age=300")
}

func (s *Service) stream(w http.ResponseWriter, r *http.Request, row fileRow, cache string) {
	etag := `"` + hex.EncodeToString(row.SHA[:12]) + `"`
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", cache)
	// Images only, never rendered as a document.
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	rc, size, err := s.store.Get(r.Context(), row.Key)
	if err != nil {
		s.log.ErrorContext(r.Context(), "file object missing", slog.String("key", row.Key), slog.Any("err", err))
		writeNotFound(w, r)
		return
	}
	defer func() { _ = rc.Close() }()
	h.Set("Content-Type", row.ContentType)
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, rc)
	}
}

func writeNotFound(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"error":"Not found","code":"NOT_FOUND"}` + "\n"))
}

// ---------------------------------------------------------------- upload

// Upload handles a single image upload for POST /api/files. The file stays
// unattached until an entity references its URL.
func (s *Service) Upload(ctx context.Context, t tenancy.Tenant, purpose string, raw []byte, userID int64) (uuid.UUID, error) {
	if _, ok := UploadPermissions[purpose]; !ok {
		return uuid.Nil, apperr.Validation(map[string]string{"purpose": "unknown purpose"})
	}
	img, err := ProcessImage(raw)
	if err != nil {
		return uuid.Nil, apperr.Validation(map[string]string{"file": ErrBadImage.Error()})
	}
	st, err := s.StoreImage(ctx, t, img)
	if err != nil {
		return uuid.Nil, err
	}
	err = s.sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
		return InsertFile(ctx, tx, st, purpose, userID)
	})
	if err != nil {
		s.Discard(ctx, st)
		return uuid.Nil, err
	}
	if err := s.plans.AddStorage(context.WithoutCancel(ctx), t.ID, int64(len(img.Data))); err != nil {
		s.log.WarnContext(ctx, "storage usage not updated", slog.Any("err", err))
	}
	return st.ID, nil
}
