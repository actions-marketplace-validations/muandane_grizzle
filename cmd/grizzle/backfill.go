package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/muandane/grizzle"
	"github.com/muandane/grizzle/internal/exec"
)

// backfillFlag is a custom flag.Value for --backfill with an optional argument.
// Bare --backfill (via IsBoolFlag) defaults to the "copy" strategy; --backfill=copy
// and (after normalizeBackfillArgs) --backfill copy are equivalent.
type backfillFlag struct {
	set      bool
	strategy string
}

func (f *backfillFlag) String() string {
	if !f.set {
		return ""
	}
	return f.strategy
}

func (f *backfillFlag) Set(v string) error {
	f.set = true
	switch v {
	case "", "true": // bare --backfill via IsBoolFlag
		f.strategy = "copy"
	default:
		f.strategy = v
	}
	return nil
}

// IsBoolFlag lets --backfill appear with no value (defaults to copy).
func (f *backfillFlag) IsBoolFlag() bool { return true }

// normalizeBackfillArgs rewrites `--backfill <strategy>` into `--backfill=<strategy>`
// so the space-separated form works alongside IsBoolFlag bare `--backfill`.
func normalizeBackfillArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--backfill" {
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				out = append(out, "--backfill="+args[i+1])
				i++
			} else {
				out = append(out, "--backfill=copy")
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

func dialectFromDSN(dsn string) grizzle.Dialect {
	switch {
	case strings.HasPrefix(dsn, "sqlite://"),
		strings.HasPrefix(dsn, "sqlite:"),
		strings.HasSuffix(dsn, ".db"),
		strings.HasSuffix(dsn, ".sqlite"),
		dsn == ":memory:":
		return grizzle.DialectSQLite
	default:
		return grizzle.DialectPostgres
	}
}

// resolveBackfillHook builds a BackfillFunc from CLI flags, or nil when disabled.
func resolveBackfillHook(expandContract bool, bf backfillFlag, file string, batch int, dsn string) (grizzle.BackfillFunc, error) {
	if !bf.set && file == "" {
		return nil, nil
	}
	if !expandContract {
		return nil, fmt.Errorf("--backfill/--backfill-file require --expand-contract")
	}
	if bf.set && file != "" {
		return nil, fmt.Errorf("cannot combine --backfill and --backfill-file; choose one")
	}
	if batch <= 0 {
		return nil, fmt.Errorf("--backfill-batch must be positive, got %d", batch)
	}
	if file != "" {
		content, err := os.ReadFile(filepath.Clean(file)) //nolint:gosec // G304: CLI accepts user-provided SQL file path
		if err != nil {
			return nil, fmt.Errorf("reading --backfill-file: %w", err)
		}
		return newFileBackfillHook(string(content), batch), nil
	}
	if bf.strategy != "copy" {
		return nil, fmt.Errorf("unsupported --backfill strategy %q (supported: copy)", bf.strategy)
	}
	return newCopyBackfillHook(dialectFromDSN(dsn), batch), nil
}

// buildCopyBackfillSQL returns a keyset-batched UPDATE for the copy strategy.
// Placeholders are (lastPK, batchSize): $1/$2 for Postgres, ?/? for SQLite.
// Callers pass a zero-value lastPK on the first batch (e.g. 0 for integer PKs).
func buildCopyBackfillSQL(dialect grizzle.Dialect, tableRef, pkCol, oldCol, newCol string) string {
	switch dialect {
	case grizzle.DialectPostgres:
		return fmt.Sprintf(
			`UPDATE %s SET %q = %q WHERE %q IN (SELECT %q FROM %s WHERE %q IS NULL AND %q > $1 ORDER BY %q LIMIT $2)`,
			tableRef, newCol, oldCol, pkCol, pkCol, tableRef, newCol, pkCol, pkCol,
		)
	default:
		return fmt.Sprintf(
			`UPDATE %s SET %q = %q WHERE %q IN (SELECT %q FROM %s WHERE %q IS NULL AND %q > ? ORDER BY %q LIMIT ?)`,
			tableRef, newCol, oldCol, pkCol, pkCol, tableRef, newCol, pkCol, pkCol,
		)
	}
}

// buildCopyBackfillSQLFirstBatch is the first-iteration SQL (no cursor lower bound).
func buildCopyBackfillSQLFirstBatch(dialect grizzle.Dialect, tableRef, pkCol, oldCol, newCol string) string {
	switch dialect {
	case grizzle.DialectPostgres:
		return fmt.Sprintf(
			`UPDATE %s SET %q = %q WHERE %q IN (SELECT %q FROM %s WHERE %q IS NULL ORDER BY %q LIMIT $1)`,
			tableRef, newCol, oldCol, pkCol, pkCol, tableRef, newCol, pkCol,
		)
	default:
		return fmt.Sprintf(
			`UPDATE %s SET %q = %q WHERE %q IN (SELECT %q FROM %s WHERE %q IS NULL ORDER BY %q LIMIT ?)`,
			tableRef, newCol, oldCol, pkCol, pkCol, tableRef, newCol, pkCol,
		)
	}
}

func newCopyBackfillHook(d grizzle.Dialect, batch int) grizzle.BackfillFunc {
	execDialect := exec.DialectPostgres
	if d == grizzle.DialectSQLite {
		execDialect = exec.DialectSQLite
	}
	return grizzle.BackfillFunc(exec.NewCopyBackfillHook(execDialect, batch))
}

func newFileBackfillHook(templateSQL string, batch int) grizzle.BackfillFunc {
	return func(ctx context.Context, tx *sql.Tx, table, oldCol, newCol string) error {
		stmt := applyBackfillTemplate(templateSQL, table, oldCol, newCol, batch)
		//nolint:gosec // G701: user-supplied --backfill-file SQL; identifiers substituted via %q
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("file backfill exec: %w", err)
		}
		return nil
	}
}

func applyBackfillTemplate(tmpl, table, oldCol, newCol string, batch int) string {
	r := strings.NewReplacer(
		"{table}", fmt.Sprintf("%q", table),
		"{old}", fmt.Sprintf("%q", oldCol),
		"{new}", fmt.Sprintf("%q", newCol),
		"{batch}", strconv.Itoa(batch),
	)
	return r.Replace(tmpl)
}

// lookupSingleColumnPK returns the sole primary-key column name, or an error
// when the table has no PK or a composite PK (copy strategy cannot keyset).
func lookupSingleColumnPK(ctx context.Context, tx *sql.Tx, dialect grizzle.Dialect, table string) (string, error) {
	switch dialect {
	case grizzle.DialectPostgres:
		return lookupPostgresSingleColumnPK(ctx, tx, table)
	default:
		return lookupSQLiteSingleColumnPK(ctx, tx, table)
	}
}

func lookupPostgresSingleColumnPK(ctx context.Context, tx *sql.Tx, table string) (string, error) {
	const q = `
SELECT kcu.column_name
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_schema = kcu.constraint_schema
 AND tc.constraint_name = kcu.constraint_name
 AND tc.table_schema = kcu.table_schema
 AND tc.table_name = kcu.table_name
WHERE tc.constraint_type = 'PRIMARY KEY'
  AND tc.table_name = $1
  AND tc.table_schema = current_schema()
ORDER BY kcu.ordinal_position`

	rows, err := tx.QueryContext(ctx, q, table)
	if err != nil {
		return "", fmt.Errorf("looking up primary key for %q: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	var cols []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return "", fmt.Errorf("scanning primary key for %q: %w", table, err)
		}
		cols = append(cols, col)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(cols) != 1 {
		return "", fmt.Errorf("copy backfill requires a single-column primary key on %q (found %d columns)", table, len(cols))
	}
	return cols[0], nil
}

func lookupSQLiteSingleColumnPK(ctx context.Context, tx *sql.Tx, table string) (string, error) {
	//nolint:gosec // G201: table name is from Grizzle rename map; quoted with %q
	q := fmt.Sprintf(`PRAGMA table_info(%q)`, table)
	rows, err := tx.QueryContext(ctx, q)
	if err != nil {
		return "", fmt.Errorf("looking up primary key for %q: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	type pkCol struct {
		name string
		pk   int
	}
	var pks []pkCol
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return "", fmt.Errorf("scanning pragma table_info for %q: %w", table, err)
		}
		if pk > 0 {
			pks = append(pks, pkCol{name: name, pk: pk})
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(pks) != 1 {
		return "", fmt.Errorf("copy backfill requires a single-column primary key on %q (found %d columns)", table, len(pks))
	}
	return pks[0].name, nil
}
