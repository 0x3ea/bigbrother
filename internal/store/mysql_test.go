package store

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0x3ea/bigbrother/internal/prober"
	"github.com/go-sql-driver/mysql"
)

const mysqlTestDBName = "bigbrother_migrate_test"

func TestNewMySQLSuccess(t *testing.T) {
	testDB(t)

	base := os.Getenv("BIGBROTHER_TEST_DSN")
	cfg, err := mysql.ParseDSN(base)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = mysqlTestDBName

	ms, err := NewMySQL(cfg.FormatDSN())
	if err != nil {
		t.Fatalf("point to exist database should succeed: %v", err)
	}
	defer ms.Close()

	if !tableExists(t, ms.db, "probe_result") {
		t.Fatal("NewMySQL should have migrated")
	}
}

func TestNewMySQLErrors(t *testing.T) {
	_, err := NewMySQL("not-a-dsn")
	if err == nil || !strings.Contains(err.Error(), "open mysql") {
		t.Fatalf("bad DSN should report open mysql,got: %v", err)
	}
	_, err = NewMySQL("u:p@tcp(127.0.0.1:1)/x?parseTime=true")
	if err == nil || !strings.Contains(err.Error(), "ping mysql") {
		t.Fatalf("connect refused should report ping mysql,got: %v", err)
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"small", "hello", 512, "hello"},
		{"equal", strings.Repeat("x", 512), 512, strings.Repeat("x", 512)},
		{"over_by_one", strings.Repeat("x", 513), 512, strings.Repeat("x", 512)},
		{"much_bigger", strings.Repeat("ab", 400), 512, strings.Repeat("ab", 256)},
		{"empty", "", 512, ""},
		{"limit=zero", "hello", 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := truncate(c.in, c.n)
			if got != c.want {
				t.Errorf("truncate(len=%d, n=%d) got %d,want %d",
					len(c.in), c.n, len(got), len(c.want))
			}
		})
	}
}

func TestMySQLStoreRoundtrip(t *testing.T) {
	db := testDB(t)

	if err := migrate(db, migrationsFS); err != nil {
		t.Fatal(err)
	}
	s := &MySQLStore{db: db}

	base := time.Now()
	rows := []prober.Result{
		{Success: false, Error: errors.New("timeout"), Timestamp: base.Add(-2 * time.Second)},
		{Success: true, StatusCode: 200, Latency: 1200 * time.Microsecond, Timestamp: base.Add(-1 * time.Second)},
		{Success: true, StatusCode: 200, Latency: 1300 * time.Microsecond, Timestamp: base},
	}
	for _, r := range rows {
		if err := s.Save("blog", r); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	rs, err := s.History("blog", 10)
	if err != nil || len(rs) != 3 {
		t.Fatalf("history: err=%v got=%d want=3", err, len(rs))
	}
	if !rs[0].Timestamp.Before(rs[2].Timestamp) {
		t.Fatal("History should order by Timestamp")
	}
	if rs[0].Error == nil || rs[0].Error.Error() != "timeout" {
		t.Fatal("failed result err should keep equal")
	}
	if rs[1].Latency != 1200*time.Microsecond {
		t.Fatalf("µs accuracy reread loss: %v, want: 1200", rs[1].Latency)
	}

	r, ok, err := s.Latest("blog")
	if err != nil || !ok || r.Error != nil {
		t.Fatalf("latest: ok=%v err=%v r=%+v", ok, err, r)
	}

	if _, ok, err := s.Latest("nope"); ok || err != nil {
		t.Fatalf("no result should be (false, nil),got ok=%v err=%v", ok, err)
	}
}

func TestMySQLStoreLimitAndIsolation(t *testing.T) {
	db := testDB(t)
	if err := migrate(db, migrationsFS); err != nil {
		t.Fatal(err)
	}
	s := &MySQLStore{db: db}

	base := time.Now()
	for i, us := range []int64{100, 200, 300} {
		r := prober.Result{
			Success:   true,
			Latency:   time.Duration(us) * time.Microsecond,
			Timestamp: base.Add(time.Duration(i-2) * time.Second),
		}
		if err := s.Save("blog", r); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Save("shop", prober.Result{
		Success:   false,
		Error:     errors.New("shop-marker"),
		Timestamp: base,
	}); err != nil {
		t.Fatal(err)
	}

	rs, err := s.History("blog", 2)
	if err != nil || len(rs) != 2 {
		t.Fatalf("limit=2 should return 2: err=%v len=%d", err, len(rs))
	}
	if rs[0].Latency != 200*time.Microsecond || rs[1].Latency != 300*time.Microsecond {
		t.Fatalf("should be latest 2 results [200,300],got [%v,%v]", rs[0].Latency, rs[1].Latency)
	}

	rs, err = s.History("blog", 10)
	if err != nil || len(rs) != 3 {
		t.Fatalf("isolation failed: err=%v len=%d", err, len(rs))
	}
	for _, r := range rs {
		if r.Error != nil && r.Error.Error() == "shop-marker" {
			t.Fatal("shop's result should not appear in blog")
		}
	}

	rs, err = s.History("shop", 10)
	if err != nil || len(rs) != 1 || rs[0].Error == nil || rs[0].Error.Error() != "shop-marker" {
		t.Fatalf("shop should have 1 result with err: %v got=%+v", err, rs)
	}
}

func TestMySQLStoreNulls(t *testing.T) {
	db := testDB(t)
	if err := migrate(db, migrationsFS); err != nil {
		t.Fatal(err)
	}
	s := &MySQLStore{db: db}

	base := time.Now()
	cert := base.Add(90 * 24 * time.Hour).Truncate(time.Millisecond)
	if err := s.Save("tls-on", prober.Result{
		Success:         true,
		StatusCode:      200,
		TLSCertNotAfter: cert,
		Timestamp:       base,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("tls-off", prober.Result{
		Success:   false,
		Error:     errors.New("no tls"),
		Timestamp: base,
	}); err != nil {
		t.Fatal(err)
	}

	rs, err := s.History("tls-on", 1)
	if err != nil || len(rs) != 1 {
		t.Fatalf("tls-on: err=%v n=%d", err, len(rs))
	}
	if !rs[0].TLSCertNotAfter.Equal(cert) {
		t.Fatalf("TLS reread loss: got=%v want=%v", rs[0].TLSCertNotAfter, cert)
	}
	if rs[0].Error != nil {
		t.Fatalf("err should be nil(NULL),got %q", rs[0].Error.Error())
	}

	rs, err = s.History("tls-off", 1)
	if err != nil || len(rs) != 1 {
		t.Fatalf("tls-off: err=%v n=%d", err, len(rs))
	}
	if !rs[0].TLSCertNotAfter.IsZero() {
		t.Fatalf("NULL reread should be base,got %v", rs[0].TLSCertNotAfter)
	}
	if rs[0].Error == nil || rs[0].Error.Error() != "no tls" {
		t.Fatal("err_msg should be keep equal")
	}
}

func TestMySQLStoreErrors(t *testing.T) {
	db := testDB(t)
	if err := migrate(db, migrationsFS); err != nil {
		t.Fatal(err)
	}
	s := &MySQLStore{db: db}

	if _, err := db.Exec("DROP TABLE probe_result"); err != nil {
		t.Fatal(err)
	}

	if err := s.Save("blog", prober.Result{Success: true, Timestamp: time.Now()}); err == nil {
		t.Fatal("Save() when Store failed should return error")
	}

	if rs, err := s.History("blog", 10); err == nil {
		t.Fatalf("History() when Store failed should return error,got %d results", len(rs))
	}

	if _, ok, err := s.Latest("blog"); err == nil {
		t.Fatal("Latest() when Store failed should return error")
	} else if ok {
		t.Fatal("Latest() when Store failed should return ok = false,without any data")
	}

}
