package store

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/0x3ea/bigbrother/internal/prober"

	_ "github.com/go-sql-driver/mysql"
)

const insertSQL = `INSERT INTO probe_result
    (name, success, latency_us, status_code, err_msg, tls_not_after, checked_at)
    VALUES (?, ?, ?, ?, ?, ?, ?)`

const selectSQL = `SELECT success, latency_us, status_code, err_msg, tls_not_after, checked_at
    FROM probe_result
    WHERE name = ?
    ORDER BY checked_at DESC
    LIMIT ?`

var _ Store = (*MySQLStore)(nil)

type MySQLStore struct {
	db *sql.DB
}

// NewMySQL 创建mysql存储并自动执行Ping(fail-fast)和迁移,使用完毕后需要调用Close
func NewMySQL(dsn string) (*MySQLStore, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	if err := migrate(db, migrationsFS); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &MySQLStore{db: db}, nil
}

// Close 关闭数据库
func (ms *MySQLStore) Close() error {
	return ms.db.Close()
}

// Save 实现 Store.Save;零值时间以落库时刻兜底,err_msg 超长截断为 512 字节
func (ms *MySQLStore) Save(name string, r prober.Result) error {
	ts := r.Timestamp

	if ts.IsZero() {
		ts = time.Now()
	}

	var errMsg, tlsNA any
	if r.Error != nil {
		errMsg = truncate(r.Error.Error(), 512)
	}
	if !r.TLSCertNotAfter.IsZero() {
		tlsNA = r.TLSCertNotAfter
	}
	if _, err := ms.db.Exec(insertSQL,
		name, r.Success, r.Latency.Microseconds(), r.StatusCode, errMsg, tlsNA, ts,
	); err != nil {
		return fmt.Errorf("insert %s: %w", name, err)
	}
	return nil
}

// Latest 实现 Store.Latest
func (ms *MySQLStore) Latest(name string) (prober.Result, bool, error) {
	rs, err := ms.History(name, 1)
	if err != nil {
		return prober.Result{}, false, err
	}

	if len(rs) == 0 {
		return prober.Result{}, false, nil
	}
	return rs[0], true, nil
}

// History 实现 Store.History
func (ms *MySQLStore) History(name string, n int) ([]prober.Result, error) {
	if n <= 0 {
		return nil, nil
	}

	rows, err := ms.db.Query(selectSQL, name, n)
	if err != nil {
		return nil, fmt.Errorf("query history %s: %w", name, err)
	}
	defer rows.Close()

	var out []prober.Result
	for rows.Next() {
		r, err := scanResult(rows)
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", name, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", name, err)
	}

	slices.Reverse(out)
	return out, nil
}

func scanResult(rows *sql.Rows) (prober.Result, error) {
	var r prober.Result
	var latencyUS int64
	var errMsg sql.NullString // 可空列必须用 NullXxx 承接
	var tlsNA sql.NullTime

	if err := rows.Scan(&r.Success, &latencyUS, &r.StatusCode, &errMsg, &tlsNA, &r.Timestamp); err != nil {
		return r, err
	}
	r.Latency = time.Duration(latencyUS) * time.Microsecond
	if errMsg.Valid {
		r.Error = errors.New(errMsg.String)
	}
	if tlsNA.Valid {
		r.TLSCertNotAfter = tlsNA.Time
	}
	return r, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
