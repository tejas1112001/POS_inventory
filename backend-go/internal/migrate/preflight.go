package migrate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/db"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
)

// Preflight outcomes.
const (
	Pass   = "PASS"
	Fail   = "FAIL"
	Manual = "MANUAL"
	Skip   = "SKIP"
)

// Check is one preflight line (OPERATIONS §8).
type Check struct {
	ID     string
	Name   string
	Status string
	Detail string
}

// PreflightOptions tune the run.
type PreflightOptions struct {
	ManyRoles int // M5 loop size; the design asks for 2000
}

// Preflight checks a managed Postgres against M1–M14 in a fresh scratch
// database and cleans up after itself. adminDSN is the provider's admin
// login. It never prints DSNs or passwords.
func Preflight(ctx context.Context, adminDSN secrets.Secret, o PreflightOptions) ([]Check, error) {
	if o.ManyRoles <= 0 {
		o.ManyRoles = 2000
	}
	u, err := url.Parse(adminDSN.Reveal())
	if err != nil {
		return nil, fmt.Errorf("preflight: invalid DATABASE_ADMIN_URL")
	}
	suffix := randHex(4)
	scratchDB := "pos_preflight_" + suffix
	owner, migrator, app := "pf_owner_"+suffix, "pf_migrator_"+suffix, "pf_app_"+suffix
	tenantRole, tenantSchema := "t_pf"+suffix+"_rw", "t_pf"+suffix
	pw := randHex(16)

	admin, err := pgx.Connect(ctx, adminDSN.Reveal())
	if err != nil {
		return nil, fmt.Errorf("preflight: cannot connect with DATABASE_ADMIN_URL")
	}
	defer func() { _ = admin.Close(context.WithoutCancel(ctx)) }()

	var checks []Check
	add := func(id, name, status, detail string) {
		checks = append(checks, Check{ID: id, Name: name, Status: status, Detail: detail})
	}
	show := func(c *pgx.Conn, setting string) string {
		var v string
		if err := c.QueryRow(ctx, `SELECT coalesce(current_setting($1, true), '')`, setting).Scan(&v); err != nil {
			return ""
		}
		return v
	}

	// M1
	var ver int
	_ = admin.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).Scan(&ver)
	add("M1", "PostgreSQL >= 16", passIf(ver >= 160000), fmt.Sprintf("server_version_num=%d", ver))

	if err := db.CreateDatabase(ctx, admin, scratchDB); err != nil {
		add("M2", "create scratch database", Fail, "admin cannot CREATE DATABASE: "+short(err))
		return checks, nil
	}
	defer func() {
		cctx := context.WithoutCancel(ctx)
		_ = db.DropDatabase(cctx, admin, scratchDB)
		for _, r := range []string{tenantRole, app, migrator, owner} {
			_ = db.DropRole(cctx, admin, r)
		}
	}()

	scratchURL := *u
	scratchURL.Path = "/" + scratchDB
	sc, err := pgx.Connect(ctx, scratchURL.String())
	if err != nil {
		return checks, fmt.Errorf("preflight: cannot connect to the scratch database")
	}
	defer func() { _ = sc.Close(context.WithoutCancel(ctx)) }()

	// M2: roles, including one with CREATEROLE.
	verifier, _ := db.SCRAMVerifier(pw)
	err = pfCreateRoles(ctx, sc, owner, migrator, app, verifier, scratchDB)
	add("M2", "admin can CREATE ROLE ... CREATEROLE", passIfNil(err), short(err))

	// M4
	csg := show(sc, "createrole_self_grant")
	add("M4", "createrole_self_grant is empty", passIf(csg == ""), "value="+csg)

	// M7
	_, extErr := sc.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS extensions; CREATE EXTENSION IF NOT EXISTS citext WITH SCHEMA extensions; CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA extensions`)
	spl := show(sc, "shared_preload_libraries")
	m7 := extErr == nil && strings.Contains(spl, "pg_stat_statements")
	add("M7", "citext/pg_trgm installable; pg_stat_statements preloaded", passIf(m7), "shared_preload_libraries="+spl+" "+short(extErr))

	// M8
	locks := show(sc, "max_locks_per_transaction")
	var lockN int
	_, _ = fmt.Sscan(locks, &lockN)
	add("M8", "max_locks_per_transaction >= 1024", passIf(lockN >= 1024),
		"max_locks_per_transaction="+locks+", max_connections="+show(sc, "max_connections")+
			", log_min_duration_statement="+show(sc, "log_min_duration_statement"))

	// M14
	_, err = sc.Exec(ctx, `DO $$ BEGIN EXECUTE format('GRANT pg_read_all_stats TO %I', current_user); END $$`)
	add("M14", "pg_read_all_stats can be granted", passIfNil(err), short(err))

	// Connect as the migrator (startup params role/search_path = M6).
	migURL := scratchURL
	migURL.User = url.UserPassword(migrator, pw)
	mcfg, err := startupParamConfig(migURL.String(), owner, "extensions")
	if err != nil {
		return checks, err
	}
	mc, err := pgx.ConnectConfig(ctx, mcfg)
	if err != nil {
		add("M6", "role/search_path accepted as startup parameters", Fail, short(err))
		return checks, nil
	}
	defer func() { _ = mc.Close(context.WithoutCancel(ctx)) }()
	var curUser, curPath string
	_ = mc.QueryRow(ctx, `SELECT current_user::text, current_setting('search_path')`).Scan(&curUser, &curPath)
	m6 := curUser == owner && curPath == "extensions"
	errSchema := db.CreateSchema(ctx, mc, tenantSchema, owner)
	add("M6", "role/search_path startup parameters; CREATE SCHEMA as owner", passIf(m6 && errSchema == nil),
		fmt.Sprintf("current_user=%s search_path=%s %s", curUser, curPath, short(errSchema)))

	// M3: the migrator (as itself, which holds CREATEROLE) creates a tenant
	// role and grants it to the app.
	mc2, err := pgx.Connect(ctx, migURL.String())
	if err == nil {
		err = db.CreateRoleNoLogin(ctx, mc2, tenantRole)
		if err == nil {
			err = db.GrantRoleSet(ctx, mc2, tenantRole, app)
		}
		_ = mc2.Close(ctx)
	}
	m3detail := short(err)
	if err == nil {
		appURL := scratchURL
		appURL.User = url.UserPassword(app, pw)
		ac, cerr := pgx.Connect(ctx, appURL.String())
		if cerr != nil {
			err = cerr
		} else {
			// M9: the app role's default search_path is empty.
			var path string
			_ = ac.QueryRow(ctx, `SELECT current_setting('search_path')`).Scan(&path)
			add("M9", "ALTER ROLE ... SET search_path = '' takes effect", passIf(path == "" || path == `""`), "search_path="+path)
			err = pgx.BeginFunc(ctx, ac, func(tx pgx.Tx) error {
				if err := db.SetLocalRole(ctx, tx, tenantRole); err != nil {
					return err
				}
				var who string
				if err := tx.QueryRow(ctx, `SELECT current_user::text`).Scan(&who); err != nil {
					return err
				}
				if who != tenantRole {
					return fmt.Errorf("current_user=%s", who)
				}
				return nil
			})
			_ = ac.Close(ctx)
		}
		m3detail = short(err)
	}
	add("M3", "migrator grants tenant role to app WITH INHERIT FALSE, SET TRUE", passIfNil(err), m3detail)

	// M10: SECURITY DEFINER function owned by the owner role can write.
	_, err = sc.Exec(ctx, `CREATE TABLE extensions.pf_probe (x int);
		CREATE FUNCTION extensions.pf_probe_write() RETURNS void LANGUAGE sql SECURITY DEFINER AS 'INSERT INTO extensions.pf_probe VALUES (1)';
		SELECT extensions.pf_probe_write();
		DROP FUNCTION extensions.pf_probe_write(); DROP TABLE extensions.pf_probe`)
	add("M10", "SECURITY DEFINER functions can write", passIfNil(err), short(err))

	// M11: LISTEN on one connection, NOTIFY on another (this endpoint).
	add("M11", "LISTEN/NOTIFY on a direct endpoint", passIfNil(pfListenNotify(ctx, scratchURL.String())), "")

	// M12
	mode := u.Query().Get("sslmode")
	if mode == "verify-full" {
		add("M12", "TLS sslmode=verify-full", Pass, "")
	} else {
		add("M12", "TLS sslmode=verify-full", Skip, "DSN uses sslmode="+mode+" (required in production)")
	}

	// M5: many roles and schemas.
	start := time.Now()
	err = pfManyRoles(ctx, sc, o.ManyRoles)
	add("M5", fmt.Sprintf("%d roles + %d schemas", o.ManyRoles, o.ManyRoles), passIfNil(err),
		fmt.Sprintf("took %s %s", time.Since(start).Round(time.Millisecond), short(err)))

	add("M13", "PITR enabled (>= 7 days); pg_dump -n works", Manual, "check in the provider console")
	return checks, nil
}

func pfCreateRoles(ctx context.Context, c *pgx.Conn, owner, migrator, app, verifier, dbName string) error {
	return pgx.BeginFunc(ctx, c, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('pf.o', $1, true), set_config('pf.m', $2, true), set_config('pf.a', $3, true), set_config('pf.v', $4, true), set_config('pf.d', $5, true)`,
			owner, migrator, app, verifier, dbName); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DO $$
DECLARE o text := current_setting('pf.o'); m text := current_setting('pf.m'); a text := current_setting('pf.a');
        v text := current_setting('pf.v'); d text := current_setting('pf.d');
BEGIN
  EXECUTE format('CREATE ROLE %I NOLOGIN', o);
  EXECUTE format('CREATE ROLE %I LOGIN CREATEROLE PASSWORD %L', m, v);
  EXECUTE format('CREATE ROLE %I LOGIN PASSWORD %L', a, v);
  EXECUTE format('GRANT %I TO %I WITH INHERIT FALSE, SET TRUE', o, m);
  EXECUTE format('GRANT CREATE ON DATABASE %I TO %I', d, o);
  EXECUTE format('ALTER ROLE %I SET search_path = ''''', a);
  EXECUTE format('ALTER ROLE %I SET createrole_self_grant = ''''', m);
END $$`)
		return err
	})
}

func pfListenNotify(ctx context.Context, dsn string) error {
	l, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer func() { _ = l.Close(context.WithoutCancel(ctx)) }()
	if _, err := l.Exec(ctx, `LISTEN pf_probe`); err != nil {
		return err
	}
	n, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer func() { _ = n.Close(context.WithoutCancel(ctx)) }()
	if _, err := n.Exec(ctx, `SELECT pg_notify('pf_probe', 'x')`); err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err = l.WaitForNotification(wctx)
	return err
}

func pfManyRoles(ctx context.Context, c *pgx.Conn, n int) error {
	// The DO block reads its parameters from transaction-local settings, so
	// the statement text stays constant.
	return pgx.BeginFunc(ctx, c, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('pf.n', $1, true), set_config('pf.p', $2, true)`,
			fmt.Sprint(n), "pf_"+randHex(3)+"_"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DO $$
DECLARE i int; n int := current_setting('pf.n')::int; p text := current_setting('pf.p');
BEGIN
  FOR i IN 1..n LOOP
    EXECUTE format('CREATE ROLE %I NOLOGIN', p || 'r' || i);
    EXECUTE format('CREATE SCHEMA %I', p || 's' || i);
  END LOOP;
  FOR i IN 1..n LOOP
    EXECUTE format('DROP SCHEMA %I', p || 's' || i);
    EXECUTE format('DROP ROLE %I', p || 'r' || i);
  END LOOP;
END $$`)
		return err
	})
}

func passIf(ok bool) string {
	if ok {
		return Pass
	}
	return Fail
}

func passIfNil(err error) string { return passIf(err == nil) }

func short(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
