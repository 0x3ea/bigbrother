package store

import (
	"database/sql"
	"io"
	"io/fs"
	"log"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-sql-driver/mysql"
)

// 测试时静音包内所有 log.*
// 暂时方案,后面考虑让migrate返回所有正常运行的sql([]string)
func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

func testDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("BIGBROTHER_TEST_DSN")
	if dsn == "" {
		t.Skip("BIGBROTHER_TEST_DSN not specified in environment variable, skip MySQL Integration testing")
	}

	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse BIGBROTHER_TEST_DSN: %v", err)
	}

	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("DROP DATABASE IF EXISTS " + mysqlTestDBName); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("CREATE DATABASE " + mysqlTestDBName); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin.Exec("DROP DATABASE " + mysqlTestDBName) // 尽力清理,Cleanup 里不报错
		admin.Close()
	})

	// 不信任环境变量里的库名
	cfg.DBName = mysqlTestDBName
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?", name,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func TestVersionOf(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		want     int
		wantErr  bool
	}{
		{"leading_zero", "001_init.sql", 1, false},
		{"without_leading_zero", "2_init.sql", 2, false},
		{"without_num", "_init.sql", 0, true},
		{"without_underscore", "01init.sql", 0, true},
		{"without_num_and_underscore", "init.sql", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := versionOf(tt.fileName)
			if !tt.wantErr && err != nil {
				t.Fatalf("VersionOf() unexpected error: %v", err)
			}
			if tt.wantErr && err == nil {
				t.Fatalf("VersionOf() expected error, got nil")
			}
			if !tt.wantErr && tt.want != n {
				t.Errorf("VersionOf() expected return: %d, got %d", tt.want, n)
			}
		})
	}
}

func TestMigrateFreshAndIdempotent(t *testing.T) {
	db := testDB(t)
	if err := migrate(db, migrationsFS); err != nil {
		t.Fatalf("first migration failed: %v", err)
	}
	if !tableExists(t, db, "probe_result") {
		t.Fatal("probe_result should exist,but not")
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	var want int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			want++
		}
	}

	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&got); err != nil {
		t.Fatal(err)
	}

	if got != want {
		t.Fatalf(".sql %d != sql tables %d", want, got)
	}

	if err := migrate(db, migrationsFS); err != nil {
		t.Fatalf("re-execute should idempotent: %v", err)
	}

	var got2 int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&got2); err != nil {
		t.Fatal(err)
	}
	if got2 != got {
		t.Fatalf("re-execute change: %d → %d(idempotent failed)", got, got2)
	}
}

func TestMigrateSkipsApplied(t *testing.T) {
	db := testDB(t)

	if _, err := db.Exec(sqlMigrationTable); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(
		"INSERT INTO schema_migrations (version, filename, applied_at) VALUES (1, 'faked.sql', NOW(3))",
	); err != nil {
		t.Fatal(err)
	}
	fsys := fstest.MapFS{
		"migrations/001_init.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE should_not_exist (id INT);"),
		}}

	if err := migrate(db, fsys); err != nil {
		t.Fatal(err)
	}

	if tableExists(t, db, "should_not_exist") {
		t.Fatal("migrated sql should not migrate")
	}

	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("migration should be 1, in fact %d", n)
	}
}

func TestMigrateAppliesInOrder(t *testing.T) {
	db := testDB(t)
	fsys := fstest.MapFS{
		"migrations/002_add_col.sql": &fstest.MapFile{
			Data: []byte("ALTER TABLE t ADD COLUMN c INT;"), // 依赖 001 的表
		},
		"migrations/001_init.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE t (id INT);"),
		},
	}

	if err := migrate(db, fsys); err != nil {
		t.Fatalf("order should be 001->002: %v", err)
	}

	if !tableExists(t, db, "t") {
		t.Fatal("001 should migrate")
	}
	if !columnExists(t, db, "t", "c") {
		t.Fatal("002 should migrate")
	}

	var order string
	if err := db.QueryRow(
		"SELECT GROUP_CONCAT(version ORDER BY version) FROM schema_migrations",
	).Scan(&order); err != nil {
		t.Fatal(err)
	}
	if order != "1,2" {
		t.Fatalf("progress should be 1,2,in fact %s", order)
	}
}

func TestMigrateStopsOnBadSQL(t *testing.T) {
	db := testDB(t)
	fsys := fstest.MapFS{
		"migrations/001_ok.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE ok_t (id INT);"),
		},
		"migrations/002_broken.sql": &fstest.MapFile{
			Data: []byte("THIS IS NOT SQL;"), // MySQL 1064 语法错误
		},
	}

	err := migrate(db, fsys)
	if err == nil {
		t.Fatal("bad migration should be failed")
	}
	if !strings.Contains(err.Error(), "002_broken.sql") {
		t.Fatalf("error should contain file name: %v", err)
	}

	if !tableExists(t, db, "ok_t") {
		t.Fatal("001 should migrated")
	}

	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("progress record should be 1 (001),in fact %d", n)
	}
}

func columnExists(t *testing.T, db *sql.DB, table, col string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM information_schema.columns "+
			"WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?",
		table, col,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}
